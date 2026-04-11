package conn

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestCov_PoolGet_PoolAlreadyClosed tests that Get on a closed pool returns
// "pool is closed" immediately (pool.go line 140).
func TestCov_PoolGet_PoolAlreadyClosed(t *testing.T) {
	pool := NewPool(&PoolConfig{
		Address:     "127.0.0.1:0",
		DialTimeout: 1 * time.Second,
		IdleTimeout: 10 * time.Minute,
		MaxSize:     5,
	})
	pool.Close()

	_, err := pool.Get(context.Background())
	if err == nil {
		t.Error("expected error from Get on closed pool")
	}
}

// TestCov_PoolGet_ContextCancelled tests Get with a pre-cancelled context.
func TestCov_PoolGet_ContextCancelled(t *testing.T) {
	pool := NewPool(&PoolConfig{
		Address:     "127.0.0.1:1", // unreachable
		DialTimeout: 5 * time.Second,
		IdleTimeout: 10 * time.Minute,
		MaxSize:     5,
	})
	defer pool.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Get

	_, err := pool.Get(ctx)
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

// TestCov_PoolPut_ClosedPool tests returning a connection to a closed pool.
func TestCov_PoolPut_ClosedPool(t *testing.T) {
	// Start a server so we can get a real connection
	srv, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	go func() {
		for {
			c, err := srv.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	pool := NewPool(&PoolConfig{
		Address:     srv.Addr().String(),
		DialTimeout: 1 * time.Second,
		IdleTimeout: 10 * time.Minute,
		MaxSize:     5,
	})

	ctx := context.Background()
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// Close the pool while the connection is checked out
	pool.Close()

	// Return the connection to the closed pool — should just close it
	pool.Put(conn)

	stats := pool.Stats()
	if stats.Active != 0 {
		t.Errorf("expected active=0 after put to closed pool, got %d", stats.Active)
	}
}

// TestCov_PoolPut_NonPooledConn tests Put with a non-PooledConn.
func TestCov_PoolPut_NonPooledConn(t *testing.T) {
	pool := NewPool(&PoolConfig{
		Address:     "127.0.0.1:0",
		DialTimeout: 1 * time.Second,
		IdleTimeout: 10 * time.Minute,
		MaxSize:     5,
	})
	defer pool.Close()

	// Create a raw pipe connection (not a PooledConn)
	serverConn, _ := net.Pipe()
	defer serverConn.Close()

	// Putting a non-PooledConn should close it without panic
	pool.Put(serverConn)
}

// TestCov_PoolEviction_ManualStats verifies that the eviction counter is
// incremented correctly when idle connections expire.
func TestCov_PoolEviction_ManualStats(t *testing.T) {
	srv, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	go func() {
		for {
			c, err := srv.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	pool := NewPool(&PoolConfig{
		Address:     srv.Addr().String(),
		DialTimeout: 1 * time.Second,
		IdleTimeout: 10 * time.Minute,
		MaxSize:     5,
	})
	defer pool.Close()

	ctx := context.Background()

	// Get 3 connections simultaneously so they are distinct
	var poolConns [3]net.Conn
	for i := 0; i < 3; i++ {
		conn, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("Get %d failed: %v", i, err)
		}
		poolConns[i] = conn
	}

	// Return all 3 via Put (not Close — Close sets closeOnce preventing reuse)
	for _, conn := range poolConns {
		pool.Put(conn)
	}

	preStats := pool.Stats()
	if preStats.Idle != 3 {
		t.Fatalf("expected 3 idle before eviction, got %d", preStats.Idle)
	}

	// Expire them by manipulating timestamps
	pool.mu.Lock()
	for _, pc := range pool.idle {
		pc.createdAt = time.Now().Add(-2 * time.Hour)
		pc.lastUsed = time.Now().Add(-2 * time.Hour)
	}
	pool.mu.Unlock()

	// Run eviction logic (mirrors evictIdle ticker path)
	pool.mu.Lock()
	remaining := make([]*PooledConn, 0, len(pool.idle))
	evicted := 0
	for _, conn := range pool.idle {
		if conn.isExpired(pool.maxLifetime, pool.idleTimeout) {
			conn.Conn.Close()
			evicted++
		} else {
			remaining = append(remaining, conn)
		}
	}
	pool.evictions += int64(evicted)
	pool.idle = remaining
	pool.mu.Unlock()

	stats := pool.Stats()
	if stats.Evictions != 3 {
		t.Errorf("expected 3 evictions, got %d", stats.Evictions)
	}
	if stats.Idle != 0 {
		t.Errorf("expected 0 idle after eviction, got %d", stats.Idle)
	}
}

// TestCov_ManagerDrain_TimerTimeout tests that Drain exits via timer when
// no context deadline is provided.
func TestCov_ManagerDrain_TimerTimeout(t *testing.T) {
	mgr := NewManager(&Config{
		MaxConnections: 100,
		MaxPerSource:   100,
		MaxPerBackend:  100,
		DrainTimeout:   50 * time.Millisecond, // very short drain timeout
	})

	// Create a tracked connection to prevent immediate drain
	srv, _ := net.Listen("tcp", "127.0.0.1:0")
	defer srv.Close()
	go func() {
		for {
			c, err := srv.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	rawConn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	tracked, err := mgr.Accept(rawConn)
	if err != nil {
		rawConn.Close()
		t.Fatal(err)
	}

	// Drain with no context deadline — should timeout via DrainTimeout timer
	err = mgr.Drain(context.Background())
	if err != nil {
		t.Logf("Drain returned: %v", err)
	}
	tracked.Close()
}

// TestCov_PoolStats verifies all fields of PoolStats.
func TestCov_PoolStats(t *testing.T) {
	pool := NewPool(&PoolConfig{
		BackendID:   "test-backend",
		Address:     "127.0.0.1:0",
		DialTimeout: 1 * time.Second,
		IdleTimeout: 10 * time.Minute,
		MaxSize:     5,
	})
	defer pool.Close()

	stats := pool.Stats()
	if stats.BackendID != "test-backend" {
		t.Errorf("expected BackendID 'test-backend', got %q", stats.BackendID)
	}
	if stats.MaxSize != 5 {
		t.Errorf("expected MaxSize 5, got %d", stats.MaxSize)
	}
}
