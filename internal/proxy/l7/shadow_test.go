package l7

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/balancer"
)

func TestNewShadowManager(t *testing.T) {
	tests := []struct {
		name   string
		config ShadowConfig
		want   bool
	}{
		{
			name: "enabled shadow manager",
			config: ShadowConfig{
				Enabled:     true,
				Percentage:  50.0,
				CopyHeaders: true,
				CopyBody:    true,
				Timeout:     5 * time.Second,
			},
			want: true,
		},
		{
			name: "disabled shadow manager",
			config: ShadowConfig{
				Enabled:     false,
				Percentage:  0.0,
				CopyHeaders: false,
				CopyBody:    false,
				Timeout:     0,
			},
			want: false,
		},
		{
			name: "zero timeout uses default",
			config: ShadowConfig{
				Enabled:     true,
				Percentage:  100.0,
				CopyHeaders: true,
				CopyBody:    true,
				Timeout:     0,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewShadowManager(tt.config)
			if sm == nil {
				t.Fatal("NewShadowManager() returned nil")
			}
			if sm.enabled != tt.want {
				t.Errorf("enabled = %v, want %v", sm.enabled, tt.want)
			}
			if sm.config.Percentage != tt.config.Percentage {
				t.Errorf("config.Percentage = %v, want %v", sm.config.Percentage, tt.config.Percentage)
			}
			if sm.targets == nil {
				t.Error("targets should be initialized")
			}
		})
	}
}

func TestShadowManager_AddTarget(t *testing.T) {
	t.Run("add target to enabled manager", func(t *testing.T) {
		config := ShadowConfig{
			Enabled:     true,
			Percentage:  50.0,
			CopyHeaders: true,
			CopyBody:    true,
			Timeout:     5 * time.Second,
		}
		sm := NewShadowManager(config)

		be := backend.NewBackend("test", "127.0.0.1:8080")
		be.SetState(backend.StateUp)
		backends := []*backend.Backend{be}
		b := balancer.NewRoundRobin()

		sm.AddTarget(b, backends, 50.0)

		if len(sm.targets) != 1 {
			t.Errorf("expected 1 target, got %d", len(sm.targets))
		}
	})

	t.Run("add target to disabled manager is no-op", func(t *testing.T) {
		config := ShadowConfig{
			Enabled: false,
		}
		sm := NewShadowManager(config)

		be := backend.NewBackend("test", "127.0.0.1:8080")
		be.SetState(backend.StateUp)
		backends := []*backend.Backend{be}
		b := balancer.NewRoundRobin()

		sm.AddTarget(b, backends, 50.0)

		if len(sm.targets) != 0 {
			t.Errorf("expected 0 targets for disabled manager, got %d", len(sm.targets))
		}
	})

	t.Run("add multiple targets", func(t *testing.T) {
		config := ShadowConfig{
			Enabled:     true,
			Percentage:  50.0,
			CopyHeaders: true,
			CopyBody:    true,
			Timeout:     5 * time.Second,
		}
		sm := NewShadowManager(config)

		// Add first target
		be1 := backend.NewBackend("test1", "127.0.0.1:8080")
		be1.SetState(backend.StateUp)
		sm.AddTarget(balancer.NewRoundRobin(), []*backend.Backend{be1}, 50.0)

		// Add second target
		be2 := backend.NewBackend("test2", "127.0.0.1:8081")
		be2.SetState(backend.StateUp)
		sm.AddTarget(balancer.NewRoundRobin(), []*backend.Backend{be2}, 100.0)

		if len(sm.targets) != 2 {
			t.Errorf("expected 2 targets, got %d", len(sm.targets))
		}
	})
}

func TestShadowManager_ShouldShadow(t *testing.T) {
	t.Run("disabled returns false", func(t *testing.T) {
		config := ShadowConfig{Enabled: false, Percentage: 50.0}
		sm := NewShadowManager(config)
		if sm.ShouldShadow() {
			t.Error("disabled ShouldShadow() should return false")
		}
	})

	t.Run("no targets returns false", func(t *testing.T) {
		config := ShadowConfig{Enabled: true, Percentage: 50.0}
		sm := NewShadowManager(config)
		if sm.ShouldShadow() {
			t.Error("ShouldShadow() with no targets should return false")
		}
	})

	t.Run("100 percent always shadows", func(t *testing.T) {
		config := ShadowConfig{Enabled: true, Percentage: 100.0}
		sm := NewShadowManager(config)
		sm.AddTarget(nil, []*backend.Backend{backend.NewBackend("test", "localhost:0")}, 100)
		for i := 0; i < 10; i++ {
			if !sm.ShouldShadow() {
				t.Error("100%% ShouldShadow() should always return true")
			}
		}
	})

	t.Run("50 percent shadows approximately half", func(t *testing.T) {
		config := ShadowConfig{Enabled: true, Percentage: 50.0}
		sm := NewShadowManager(config)
		sm.AddTarget(nil, []*backend.Backend{backend.NewBackend("test", "localhost:0")}, 50)
		shadowed := 0
		total := 100
		for i := 0; i < total; i++ {
			if sm.ShouldShadow() {
				shadowed++
			}
		}
		if shadowed == 0 || shadowed == total {
			t.Errorf("50%% ShouldShadow() got %d/%d shadowed, expected ~50", shadowed, total)
		}
	})

	t.Run("0 percent never shadows", func(t *testing.T) {
		config := ShadowConfig{Enabled: true, Percentage: 0}
		sm := NewShadowManager(config)
		sm.AddTarget(nil, []*backend.Backend{backend.NewBackend("test", "localhost:0")}, 0)
		for i := 0; i < 10; i++ {
			if sm.ShouldShadow() {
				t.Error("0%% ShouldShadow() should never return true")
			}
		}
	})

	t.Run("nil manager returns false", func(t *testing.T) {
		var sm *ShadowManager
		if sm.ShouldShadow() {
			t.Error("ShouldShadow() on nil manager should return false")
		}
	})
}

func TestShadowManager_ShadowRequest(t *testing.T) {
	t.Run("shadow request with no targets is no-op", func(t *testing.T) {
		config := ShadowConfig{
			Enabled:     true,
			Percentage:  50.0,
			CopyHeaders: true,
			CopyBody:    true,
			Timeout:     100 * time.Millisecond,
		}
		sm := NewShadowManager(config)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Host = "example.com"

		// Should not panic or block
		sm.ShadowRequest(req)
	})

	t.Run("disabled manager does nothing", func(t *testing.T) {
		config := ShadowConfig{
			Enabled:     false,
			Percentage:  50.0,
			CopyHeaders: true,
			CopyBody:    true,
			Timeout:     100 * time.Millisecond,
		}
		sm := NewShadowManager(config)

		be := backend.NewBackend("test", "127.0.0.1:8080")
		be.SetState(backend.StateUp)
		sm.AddTarget(balancer.NewRoundRobin(), []*backend.Backend{be}, 100.0)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Host = "example.com"

		// Should not panic or block
		sm.ShadowRequest(req)
	})

	t.Run("nil manager does not panic", func(t *testing.T) {
		var sm *ShadowManager
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		// Should not panic
		sm.ShadowRequest(req)
	})

	t.Run("shadow request with nil balancer skips target", func(t *testing.T) {
		config := ShadowConfig{
			Enabled:     true,
			Percentage:  50.0,
			CopyHeaders: true,
			CopyBody:    true,
			Timeout:     100 * time.Millisecond,
		}
		sm := NewShadowManager(config)

		// Add target with nil balancer
		be := backend.NewBackend("test", "127.0.0.1:8080")
		be.SetState(backend.StateUp)
		sm.targets = append(sm.targets, ShadowTarget{
			Balancer:   nil,
			Backends:   []*backend.Backend{be},
			Percentage: 100.0,
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Host = "example.com"

		// Should not panic
		sm.ShadowRequest(req)
	})

	t.Run("shadow request with no available backend", func(t *testing.T) {
		config := ShadowConfig{
			Enabled:     true,
			Percentage:  50.0,
			CopyHeaders: true,
			CopyBody:    true,
			Timeout:     100 * time.Millisecond,
		}
		sm := NewShadowManager(config)

		// Add target with empty backends
		sm.AddTarget(balancer.NewRoundRobin(), []*backend.Backend{}, 100.0)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Host = "example.com"

		// Should not panic
		sm.ShadowRequest(req)
	})
}

func TestShadowManager_sendShadow(t *testing.T) {
	t.Run("send shadow to unavailable backend", func(t *testing.T) {
		config := ShadowConfig{
			Enabled:     true,
			Percentage:  50.0,
			CopyHeaders: true,
			CopyBody:    true,
			Timeout:     100 * time.Millisecond,
		}
		sm := NewShadowManager(config)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Host = "example.com"

		// Send to a closed port
		target := ShadowTarget{
			Balancer:   balancer.NewRoundRobin(),
			Backends:   []*backend.Backend{backend.NewBackend("test", "127.0.0.1:1")},
			Percentage: 100.0,
		}

		// Should not panic or block, just return
		sm.sendShadow(req, "127.0.0.1:1", target)
	})

	t.Run("send shadow with body copying", func(t *testing.T) {
		config := ShadowConfig{
			Enabled:     true,
			Percentage:  50.0,
			CopyHeaders: true,
			CopyBody:    true,
			Timeout:     100 * time.Millisecond,
		}
		sm := NewShadowManager(config)

		bodyContent := []byte("test body content")
		_ = bodyContent // Used for documentation purposes

		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		req.Body = nil // Body is nil, should not panic
		req.Host = "example.com"

		target := ShadowTarget{
			Balancer:   balancer.NewRoundRobin(),
			Backends:   []*backend.Backend{backend.NewBackend("test", "127.0.0.1:1")},
			Percentage: 100.0,
		}

		// Should not panic with nil body
		sm.sendShadow(req, "127.0.0.1:1", target)
	})

	t.Run("send shadow without headers", func(t *testing.T) {
		config := ShadowConfig{
			Enabled:     true,
			Percentage:  50.0,
			CopyHeaders: false,
			CopyBody:    false,
			Timeout:     100 * time.Millisecond,
		}
		sm := NewShadowManager(config)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Host = "example.com"
		req.Header.Set("X-Custom", "should-not-be-copied")

		target := ShadowTarget{
			Balancer:   balancer.NewRoundRobin(),
			Backends:   []*backend.Backend{backend.NewBackend("test", "127.0.0.1:1")},
			Percentage: 100.0,
		}

		// Should not panic
		sm.sendShadow(req, "127.0.0.1:1", target)
	})
}

func TestShadowManager_Stats(t *testing.T) {
	tests := []struct {
		name string
		sm   *ShadowManager
		want ShadowStats
	}{
		{
			name: "enabled manager returns empty stats",
			sm: NewShadowManager(ShadowConfig{
				Enabled: true,
			}),
			want: ShadowStats{},
		},
		{
			name: "disabled manager returns empty stats",
			sm: NewShadowManager(ShadowConfig{
				Enabled: false,
			}),
			want: ShadowStats{},
		},
		{
			name: "nil manager returns empty stats",
			sm:   nil,
			want: ShadowStats{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.sm.Stats()
			if got.TotalRequests != tt.want.TotalRequests {
				t.Errorf("Stats().TotalRequests = %v, want %v", got.TotalRequests, tt.want.TotalRequests)
			}
			if got.SuccessRequests != tt.want.SuccessRequests {
				t.Errorf("Stats().SuccessRequests = %v, want %v", got.SuccessRequests, tt.want.SuccessRequests)
			}
			if got.FailedRequests != tt.want.FailedRequests {
				t.Errorf("Stats().FailedRequests = %v, want %v", got.FailedRequests, tt.want.FailedRequests)
			}
		})
	}
}

func TestShadowTarget(t *testing.T) {
	t.Run("shadow target with balancer and backends", func(t *testing.T) {
		be1 := backend.NewBackend("b1", "127.0.0.1:8080")
		be1.SetState(backend.StateUp)
		be2 := backend.NewBackend("b2", "127.0.0.1:8081")
		be2.SetState(backend.StateUp)

		target := ShadowTarget{
			Balancer:   balancer.NewRoundRobin(),
			Backends:   []*backend.Backend{be1, be2},
			Percentage: 50.0,
		}

		if target.Balancer == nil {
			t.Error("expected balancer to be set")
		}
		if len(target.Backends) != 2 {
			t.Errorf("expected 2 backends, got %d", len(target.Backends))
		}
		if target.Percentage != 50.0 {
			t.Errorf("expected percentage 50.0, got %f", target.Percentage)
		}
	})
}

func TestShadowConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      ShadowConfig
		wantEnabled bool
		wantPct     float64
	}{
		{
			name: "full config",
			config: ShadowConfig{
				Enabled:     true,
				Percentage:  75.0,
				CopyHeaders: true,
				CopyBody:    true,
				Timeout:     5 * time.Second,
			},
			wantEnabled: true,
			wantPct:     75.0,
		},
		{
			name: "minimal config",
			config: ShadowConfig{
				Enabled: true,
			},
			wantEnabled: true,
			wantPct:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewShadowManager(tt.config)
			if sm.enabled != tt.wantEnabled {
				t.Errorf("enabled = %v, want %v", sm.enabled, tt.wantEnabled)
			}
			if sm.config.Percentage != tt.wantPct {
				t.Errorf("percentage = %v, want %v", sm.config.Percentage, tt.wantPct)
			}
		})
	}
}

// ============================================================================
// sendShadow - full coverage with live backend
// ============================================================================

func TestShadowManager_sendShadow_WithLiveBackend(t *testing.T) {
	// Create a real HTTP server to receive the shadow request
	var receivedHeaders http.Header
	var receivedBody []byte
	var receivedMethod string
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		receivedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer backendServer.Close()

	config := ShadowConfig{
		Enabled:     true,
		Percentage:  100.0,
		CopyHeaders: true,
		CopyBody:    true,
		Timeout:     2 * time.Second,
	}
	sm := NewShadowManager(config)

	req := httptest.NewRequest(http.MethodPost, "/test?query=1", bytes.NewReader([]byte("original body content")))
	req.Host = "example.com"
	req.Header.Set("X-Custom-Header", "custom-value")
	req.Header.Set("Authorization", "Bearer token123")

	// Get the backend address from the test server
	backendAddr := strings.TrimPrefix(backendServer.URL, "http://")

	target := ShadowTarget{
		Balancer:   balancer.NewRoundRobin(),
		Backends:   []*backend.Backend{backend.NewBackend("shadow-backend", backendAddr)},
		Percentage: 100.0,
	}

	sm.sendShadow(req, backendAddr, target)

	// Wait a bit for the async request to complete
	time.Sleep(200 * time.Millisecond)

	// Verify the shadow request was sent
	if receivedMethod != http.MethodPost {
		t.Errorf("shadow method = %q, want %q", receivedMethod, http.MethodPost)
	}

	// Verify shadow marker headers
	if receivedHeaders.Get("X-OLB-Shadow") != "true" {
		t.Error("expected X-OLB-Shadow header to be 'true'")
	}
	if receivedHeaders.Get("X-OLB-Shadow-Source") != "example.com" {
		t.Error("expected X-OLB-Shadow-Source header to be 'example.com'")
	}

	// Verify custom headers were copied
	if receivedHeaders.Get("X-Custom-Header") != "custom-value" {
		t.Error("expected X-Custom-Header to be copied")
	}

	// Verify body was copied
	if string(receivedBody) != "original body content" {
		t.Errorf("shadow body = %q, want 'original body content'", string(receivedBody))
	}
}

func TestShadowManager_sendShadow_WithCopyBodyDisabled(t *testing.T) {
	var receivedBody []byte
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer backendServer.Close()

	config := ShadowConfig{
		Enabled:     true,
		Percentage:  100.0,
		CopyHeaders: true,
		CopyBody:    false, // Body copy disabled
		Timeout:     2 * time.Second,
	}
	sm := NewShadowManager(config)

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader([]byte("body should not be copied")))
	req.Host = "example.com"

	backendAddr := strings.TrimPrefix(backendServer.URL, "http://")
	target := ShadowTarget{
		Balancer:   balancer.NewRoundRobin(),
		Backends:   []*backend.Backend{backend.NewBackend("shadow-backend", backendAddr)},
		Percentage: 100.0,
	}

	sm.sendShadow(req, backendAddr, target)
	time.Sleep(200 * time.Millisecond)

	// Body should be empty or nil since CopyBody is false
	if string(receivedBody) == "body should not be copied" {
		t.Error("body should NOT have been copied when CopyBody is false")
	}
}

func TestShadowManager_sendShadow_WithCopyHeadersDisabled(t *testing.T) {
	var receivedHeaders http.Header
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer backendServer.Close()

	config := ShadowConfig{
		Enabled:     true,
		Percentage:  100.0,
		CopyHeaders: false, // Headers copy disabled
		CopyBody:    false,
		Timeout:     2 * time.Second,
	}
	sm := NewShadowManager(config)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("X-Custom-Header", "should-not-appear")

	backendAddr := strings.TrimPrefix(backendServer.URL, "http://")
	target := ShadowTarget{
		Balancer:   balancer.NewRoundRobin(),
		Backends:   []*backend.Backend{backend.NewBackend("shadow-backend", backendAddr)},
		Percentage: 100.0,
	}

	sm.sendShadow(req, backendAddr, target)
	time.Sleep(200 * time.Millisecond)

	// Custom header should NOT be present since CopyHeaders is false
	if receivedHeaders.Get("X-Custom-Header") == "should-not-appear" {
		t.Error("X-Custom-Header should NOT have been copied when CopyHeaders is false")
	}

	// Shadow marker headers should still be present
	if receivedHeaders.Get("X-OLB-Shadow") != "true" {
		t.Error("X-OLB-Shadow should still be set")
	}
}

func TestShadowManager_sendShadow_SkipsHopByHopHeaders(t *testing.T) {
	var receivedHeaders http.Header
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer backendServer.Close()

	config := ShadowConfig{
		Enabled:     true,
		Percentage:  100.0,
		CopyHeaders: true,
		CopyBody:    false,
		Timeout:     2 * time.Second,
	}
	sm := NewShadowManager(config)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("X-Custom", "should-appear")

	backendAddr := strings.TrimPrefix(backendServer.URL, "http://")
	target := ShadowTarget{
		Balancer:   balancer.NewRoundRobin(),
		Backends:   []*backend.Backend{backend.NewBackend("shadow-backend", backendAddr)},
		Percentage: 100.0,
	}

	sm.sendShadow(req, backendAddr, target)
	time.Sleep(200 * time.Millisecond)

	// Keep-Alive and Upgrade are hop-by-hop headers and should NOT be present
	// Note: Connection header may be added by Go's HTTP client, so we don't test for it
	if receivedHeaders.Get("Keep-Alive") != "" {
		t.Error("Keep-Alive header should have been skipped")
	}
	if receivedHeaders.Get("Upgrade") != "" {
		t.Error("Upgrade header should have been skipped")
	}

	// Custom header should be present
	if receivedHeaders.Get("X-Custom") != "should-appear" {
		t.Error("X-Custom header should have been copied")
	}

	// Shadow marker headers should always be present
	if receivedHeaders.Get("X-OLB-Shadow") != "true" {
		t.Error("X-OLB-Shadow header should be set")
	}
}

func TestShadowManager_ShadowRequest_WithLiveBackend(t *testing.T) {
	var shadowReceived atomic.Int32
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shadowReceived.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer backendServer.Close()

	config := ShadowConfig{
		Enabled:     true,
		Percentage:  100.0,
		CopyHeaders: true,
		CopyBody:    true,
		Timeout:     2 * time.Second,
	}
	sm := NewShadowManager(config)

	backendAddr := strings.TrimPrefix(backendServer.URL, "http://")
	be := backend.NewBackend("shadow-live", backendAddr)
	be.SetState(backend.StateUp)
	sm.AddTarget(balancer.NewRoundRobin(), []*backend.Backend{be}, 100.0)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Host = "example.com"

	sm.ShadowRequest(req)

	// Wait for async shadow request
	time.Sleep(200 * time.Millisecond)

	if shadowReceived.Load() == 0 {
		t.Error("expected shadow request to reach backend")
	}
}

func TestShadowManager_sendShadow_EmptyURLScheme(t *testing.T) {
	var receivedRequest bool
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRequest = true
		w.WriteHeader(http.StatusOK)
	}))
	defer backendServer.Close()

	config := ShadowConfig{
		Enabled:     true,
		Percentage:  100.0,
		CopyHeaders: false,
		CopyBody:    false,
		Timeout:     2 * time.Second,
	}
	sm := NewShadowManager(config)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Host = "example.com"
	// URL.Scheme is empty by default in httptest.NewRequest

	backendAddr := strings.TrimPrefix(backendServer.URL, "http://")
	target := ShadowTarget{
		Balancer:   balancer.NewRoundRobin(),
		Backends:   []*backend.Backend{backend.NewBackend("shadow-backend", backendAddr)},
		Percentage: 100.0,
	}

	sm.sendShadow(req, backendAddr, target)
	time.Sleep(200 * time.Millisecond)

	if !receivedRequest {
		t.Error("expected shadow request to be sent even with empty URL scheme")
	}
}
