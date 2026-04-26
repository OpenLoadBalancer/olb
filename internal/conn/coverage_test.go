package conn

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestCov_EvictIdle_TickerPath exercises the actual evictIdle goroutine's
// ticker path (pool.go lines 112-131) by waiting for the eviction interval
// to elapse. With idleTimeout=20s the interval is 10s (minimum clamp).
// Connections are pre-aged so they are expired when the ticker fires.
func TestCov_EvictIdle_TickerPath(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow eviction ticker test in short mode")
	}

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

	// idleTimeout = 20s -> eviction interval = idleTimeout/2 = 10s (minimum clamp)
	pool := NewPool(&PoolConfig{
		Address:     srv.Addr().String(),
		MaxSize:     10,
		MaxLifetime: 1 * time.Hour,
		IdleTimeout: 20 * time.Second,
		DialTimeout: 5 * time.Second,
	})
	defer pool.Close()

	ctx := context.Background()

	// Get and return two connections so they sit idle.
	conn1, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get 1 failed: %v", err)
	}
	conn2, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get 2 failed: %v", err)
	}
	pool.Put(conn1)
	pool.Put(conn2)

	preStats := pool.Stats()
	if preStats.Idle != 2 {
		t.Fatalf("expected 2 idle before aging, got %d", preStats.Idle)
	}

	// Artificially age the idle connections so they are expired when ticker fires.
	pool.mu.Lock()
	for _, pc := range pool.idle {
		pc.lastUsed = time.Now().Add(-30 * time.Second)
		pc.createdAt = time.Now().Add(-30 * time.Second)
	}
	pool.mu.Unlock()

	// Wait for the eviction goroutine ticker to fire (interval is 10s).
	// Give it up to 15s to make sure the tick fires.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		stats := pool.Stats()
		if stats.Idle == 0 && stats.Evictions == 2 {
			return // success: goroutine evicted expired connections
		}
		time.Sleep(200 * time.Millisecond)
	}

	stats := pool.Stats()
	t.Errorf("expected idle=0 evictions=2 after ticker, got idle=%d evictions=%d", stats.Idle, stats.Evictions)
}

// TestCov_PooledConn_DoubleClose covers the second-call-fast-return path
// in PooledConn.Close (pool.go line 312-313), where closeOnce has already
// been swapped to true.
func TestCov_PooledConn_DoubleClose(t *testing.T) {
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
		MaxSize:     5,
		MaxLifetime: 1 * time.Hour,
		IdleTimeout: 30 * time.Minute,
		DialTimeout: 5 * time.Second,
	})
	defer pool.Close()

	ctx := context.Background()
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// First Close returns the connection to the pool.
	err = conn.Close()
	if err != nil {
		t.Errorf("first Close() error = %v", err)
	}

	// Second Close should hit the CompareAndSwap fast-return path (returns nil).
	err = conn.Close()
	if err != nil {
		t.Errorf("second Close() error = %v, want nil", err)
	}

	// Pool should still have exactly 1 idle (the first Close put it back).
	stats := pool.Stats()
	if stats.Idle != 1 {
		t.Errorf("idle = %d, want 1", stats.Idle)
	}
}

// TestCov_PooledConn_CloseNilPool covers the fallback path in
// PooledConn.Close (pool.go line 319) where pool is nil, so it closes
// the underlying net.Conn directly.
func TestCov_PooledConn_CloseNilPool(t *testing.T) {
	srv, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	go func() {
		c, err := srv.Accept()
		if err != nil {
			return
		}
		c.Close()
	}()

	rawConn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	pc := &PooledConn{
		Conn:      rawConn,
		pool:      nil, // nil pool triggers direct Conn.Close
		createdAt: time.Now(),
		lastUsed:  time.Now(),
	}

	err = pc.Close()
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

// TestCov_Get_SkipsExpiredIdle covers the path in Get (pool.go lines
// 146-155) where multiple expired idle connections are iterated and
// skipped, forcing a fresh dial after exhausting the idle list.
func TestCov_Get_SkipsExpiredIdle(t *testing.T) {
	srv, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	var accepted atomic.Int32
	go func() {
		for {
			c, err := srv.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			go func(cn net.Conn) {
				defer cn.Close()
				buf := make([]byte, 1024)
				for {
					n, _ := cn.Read(buf)
					if n <= 0 {
						return
					}
				}
			}(c)
		}
	}()

	pool := NewPool(&PoolConfig{
		Address:     srv.Addr().String(),
		MaxSize:     10,
		MaxLifetime: 1 * time.Hour,
		IdleTimeout: 30 * time.Minute,
		DialTimeout: 5 * time.Second,
	})
	defer pool.Close()

	ctx := context.Background()

	// Get 3 connections and return them to the idle list.
	var conns []net.Conn
	for i := 0; i < 3; i++ {
		c, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("Get %d failed: %v", i, err)
		}
		conns = append(conns, c)
	}
	for _, c := range conns {
		pool.Put(c)
	}

	stats := pool.Stats()
	if stats.Idle != 3 {
		t.Fatalf("expected 3 idle, got %d", stats.Idle)
	}

	// Age all 3 idle connections so they are expired.
	pool.mu.Lock()
	for _, pc := range pool.idle {
		pc.lastUsed = time.Now().Add(-2 * time.Hour)
		pc.createdAt = time.Now().Add(-2 * time.Hour)
	}
	pool.mu.Unlock()

	// Now Get should skip all 3 expired idle connections and dial a new one.
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get after expired idle failed: %v", err)
	}
	conn.Close() // return to pool

	// Should have had 4 misses total (3 initial + 1 new), and the expired
	// idle connections should have been closed.
	stats = pool.Stats()
	if stats.Misses != 4 {
		t.Errorf("misses = %d, want 4", stats.Misses)
	}
	if stats.Hits != 0 {
		t.Errorf("hits = %d, want 0 (all idle were expired)", stats.Hits)
	}
}

// TestCov_PoolConcurrentGetPutWithEviction tests concurrent Get/Put
// operations under real conditions to exercise lock contention and the
// evictIdle goroutine simultaneously.
func TestCov_PoolConcurrentGetPutWithEviction(t *testing.T) {
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
			go func(cn net.Conn) {
				defer cn.Close()
				buf := make([]byte, 1024)
				for {
					n, _ := cn.Read(buf)
					if n <= 0 {
						return
					}
				}
			}(c)
		}
	}()

	pool := NewPool(&PoolConfig{
		Address:     srv.Addr().String(),
		MaxSize:     20,
		MaxLifetime: 1 * time.Hour,
		IdleTimeout: 30 * time.Minute,
		DialTimeout: 5 * time.Second,
	})
	defer pool.Close()

	ctx := context.Background()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := pool.Get(ctx)
			if err != nil {
				return
			}
			// Briefly hold the connection.
			time.Sleep(time.Millisecond)
			pool.Put(conn)
		}()
	}
	wg.Wait()

	stats := pool.Stats()
	total := stats.Idle + stats.Active
	if total > 20 {
		t.Errorf("total connections %d exceeds max size 20", total)
	}
}

// TestCov_EvictIdle_GoroutineExitsOnClosedFlag covers the path in evictIdle
// (pool.go lines 113-116) where the goroutine detects p.closed is true
// after acquiring the lock on a ticker tick and returns immediately.
// This tests the actual goroutine behavior, not a manual simulation.
func TestCov_EvictIdle_GoroutineExitsOnClosedFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow eviction test in short mode")
	}

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

	// idleTimeout = 20s -> interval = 10s
	pool := NewPool(&PoolConfig{
		Address:     srv.Addr().String(),
		MaxSize:     10,
		MaxLifetime: 1 * time.Hour,
		IdleTimeout: 20 * time.Second,
		DialTimeout: 5 * time.Second,
	})

	ctx := context.Background()
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pool.Put(conn)

	// Wait for the eviction ticker to fire at least once (10s).
	// On the first tick, the pool is open so it processes normally.
	// Then we close the pool. On the next tick, the goroutine detects
	// p.closed and exits.
	time.Sleep(12 * time.Second)

	// Close pool between ticker fires
	pool.Close()

	// The evictIdle goroutine should exit on the next ticker or via stopCh.
	// Give it a moment to finish.
	time.Sleep(1 * time.Second)

	// Verify the pool is properly closed.
	stats := pool.Stats()
	if stats.Idle != 0 {
		t.Errorf("expected idle=0 after close, got %d", stats.Idle)
	}
}

// TestCov_EvictIdle_ClosedFlagBeforeTicker covers the path in evictIdle
// (pool.go lines 114-116) where the goroutine detects p.closed is true
// inside the ticker case. We set p.closed=true without closing stopCh,
// so the goroutine must discover the closed flag via the ticker path.
func TestCov_EvictIdle_ClosedFlagBeforeTicker(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow eviction test in short mode")
	}

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

	// idleTimeout = 20s -> interval = 10s
	pool := NewPool(&PoolConfig{
		Address:     srv.Addr().String(),
		MaxSize:     10,
		MaxLifetime: 1 * time.Hour,
		IdleTimeout: 20 * time.Second,
		DialTimeout: 5 * time.Second,
	})

	ctx := context.Background()
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pool.Put(conn)

	// Set p.closed=true directly WITHOUT closing stopCh.
	// This forces the evictIdle goroutine to discover the closed state
	// via the ticker path (lines 114-116).
	pool.mu.Lock()
	pool.closed = true
	pool.mu.Unlock()

	// Wait for the eviction ticker to fire (up to 10s).
	// The goroutine will lock, see p.closed=true, unlock and return.
	time.Sleep(12 * time.Second)

	// Now properly close the pool (close stopCh so goroutine can finish
	// if it hasn't already, and clean up idle connections).
	pool.mu.Lock()
	select {
	case <-pool.stopCh:
	default:
		close(pool.stopCh)
	}
	for _, c := range pool.idle {
		c.Conn.Close()
	}
	pool.idle = pool.idle[:0]
	pool.mu.Unlock()

	// Wait for the wg to finish
	pool.wg.Wait()
}

// TestCov_PooledConn_CloseAfterPoolClose covers the path where a
// PooledConn is closed after its pool has been closed. The Put method
// should detect the closed pool and close the underlying connection.
func TestCov_PooledConn_CloseAfterPoolClose(t *testing.T) {
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
			go func(cn net.Conn) {
				defer cn.Close()
				buf := make([]byte, 1024)
				for {
					n, _ := cn.Read(buf)
					if n <= 0 {
						return
					}
				}
			}(c)
		}
	}()

	pool := NewPool(&PoolConfig{
		Address:     srv.Addr().String(),
		MaxSize:     5,
		MaxLifetime: 1 * time.Hour,
		IdleTimeout: 30 * time.Minute,
		DialTimeout: 5 * time.Second,
	})

	ctx := context.Background()
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Close the pool while connection is checked out.
	pool.Close()

	// Now close the connection - Put will detect pool is closed and
	// close the underlying connection.
	err = conn.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	stats := pool.Stats()
	if stats.Active != 0 {
		t.Errorf("active = %d after close, want 0", stats.Active)
	}
}

// TestCov_Pool_GetWithPreCancelledContext covers the path in Get where
// the context is already cancelled before dial starts (pool.go line 167),
// causing dial to fail immediately and returning the error.
func TestCov_Pool_GetWithPreCancelledContext(t *testing.T) {
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
		MaxSize:     5,
		MaxLifetime: 1 * time.Hour,
		IdleTimeout: 30 * time.Minute,
		DialTimeout: 5 * time.Second,
	})
	defer pool.Close()

	// Cancel context before calling Get.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = pool.Get(ctx)
	if err == nil {
		t.Error("expected error with pre-cancelled context")
	}

	stats := pool.Stats()
	if stats.Misses != 1 {
		t.Errorf("misses = %d, want 1", stats.Misses)
	}
}

// TestCov_PoolStats_AddressField verifies that PoolStats.Address is
// populated correctly from the pool config.
func TestCov_PoolStats_AddressField(t *testing.T) {
	pool := NewPool(&PoolConfig{
		BackendID:   "backend-42",
		Address:     "192.168.1.1:8080",
		MaxSize:     5,
		MaxLifetime: 1 * time.Hour,
		IdleTimeout: 30 * time.Minute,
		DialTimeout: 5 * time.Second,
	})
	defer pool.Close()

	stats := pool.Stats()
	if stats.Address != "192.168.1.1:8080" {
		t.Errorf("Address = %q, want %q", stats.Address, "192.168.1.1:8080")
	}
	if stats.BackendID != "backend-42" {
		t.Errorf("BackendID = %q, want %q", stats.BackendID, "backend-42")
	}
}

// TestCov_Pool_PutExpiredConn covers the Put path (pool.go line 210)
// where an expired connection is returned - it should be closed rather
// than added back to the idle list.
func TestCov_Pool_PutExpiredConn(t *testing.T) {
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
			go func(cn net.Conn) {
				defer cn.Close()
				buf := make([]byte, 1024)
				for {
					n, _ := cn.Read(buf)
					if n <= 0 {
						return
					}
				}
			}(c)
		}
	}()

	pool := NewPool(&PoolConfig{
		Address:     srv.Addr().String(),
		MaxSize:     10,
		MaxLifetime: 1 * time.Millisecond, // very short max lifetime
		IdleTimeout: 30 * time.Minute,
		DialTimeout: 5 * time.Second,
	})
	defer pool.Close()

	ctx := context.Background()
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for maxLifetime to expire.
	time.Sleep(5 * time.Millisecond)

	// Put should detect the expired connection and close it.
	pool.Put(conn)

	stats := pool.Stats()
	if stats.Idle != 0 {
		t.Errorf("idle = %d after putting expired connection, want 0", stats.Idle)
	}
	if stats.Active != 0 {
		t.Errorf("active = %d after putting expired connection, want 0", stats.Active)
	}
}

// TestCov_Pool_PutToFullPool covers the Put path (pool.go line 210)
// where the pool is already at max capacity, so the connection is closed.
func TestCov_Pool_PutToFullPool(t *testing.T) {
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
			go func(cn net.Conn) {
				defer cn.Close()
				buf := make([]byte, 1024)
				for {
					n, _ := cn.Read(buf)
					if n <= 0 {
						return
					}
				}
			}(c)
		}
	}()

	pool := NewPool(&PoolConfig{
		Address:     srv.Addr().String(),
		MaxSize:     1, // only 1 idle slot
		MaxLifetime: 1 * time.Hour,
		IdleTimeout: 30 * time.Minute,
		DialTimeout: 5 * time.Second,
	})
	defer pool.Close()

	ctx := context.Background()

	// Fill the idle list with 1 connection.
	conn1, err := pool.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pool.Put(conn1)

	// Get a second connection.
	conn2, err := pool.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// conn1 was reused from idle (hit), so we now have conn2 active.
	// Get another connection to fill idle again.
	conn3, err := pool.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pool.Put(conn3)

	// Now idle list has 1 connection. Put conn2 - pool is full.
	pool.Put(conn2)

	stats := pool.Stats()
	if stats.Idle > 1 {
		t.Errorf("idle = %d, want <= 1 (pool max size)", stats.Idle)
	}
	if stats.Active != 0 {
		t.Errorf("active = %d, want 0", stats.Active)
	}
}
