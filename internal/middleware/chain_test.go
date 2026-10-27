package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// testMiddleware is a test implementation of Middleware
type testMiddleware struct {
	name     string
	priority int
	marker   *string
	markVal  string
}

func (m *testMiddleware) Name() string {
	return m.name
}

func (m *testMiddleware) Priority() int {
	return m.priority
}

func (m *testMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*m.marker += m.markVal + "-before-"
		next.ServeHTTP(w, r)
		*m.marker += m.markVal + "-after-"
	})
}

func TestNewChain(t *testing.T) {
	chain := NewChain()
	if chain == nil {
		t.Fatal("NewChain returned nil")
	}
	if chain.Len() != 0 {
		t.Errorf("expected empty chain, got %d middleware", chain.Len())
	}
}

func TestChainUse(t *testing.T) {
	chain := NewChain()
	mw := &testMiddleware{name: "test", priority: 100}

	result := chain.Use(mw)
	if result != chain {
		t.Error("Use should return chain for chaining")
	}
	if chain.Len() != 1 {
		t.Errorf("expected 1 middleware, got %d", chain.Len())
	}
}

func TestChainUseNil(t *testing.T) {
	chain := NewChain()
	chain.Use(nil)
	if chain.Len() != 0 {
		t.Errorf("expected 0 middleware, got %d", chain.Len())
	}
}

func TestChainPriorityOrdering(t *testing.T) {
	var order string
	chain := NewChain()

	// Add middleware in random order
	chain.Use(&testMiddleware{name: "high", priority: 300, marker: &order, markVal: "high"})
	chain.Use(&testMiddleware{name: "low", priority: 100, marker: &order, markVal: "low"})
	chain.Use(&testMiddleware{name: "mid", priority: 200, marker: &order, markVal: "mid"})

	handler := chain.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order += "handler-"
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Expected order: low (100) -> mid (200) -> high (300) -> handler
	expected := "low-before-mid-before-high-before-handler-high-after-mid-after-low-after-"
	if order != expected {
		t.Errorf("execution order wrong:\n  got:      %s\n  expected: %s", order, expected)
	}
}

func TestChainThenNil(t *testing.T) {
	chain := NewChain()
	var order string
	chain.Use(&testMiddleware{name: "test", priority: 100, marker: &order, markVal: "test"})

	// nil handler should default to DefaultServeMux
	handler := chain.Then(nil)
	if handler == nil {
		t.Error("Then(nil) should not return nil")
	}
}

func TestChainThenFunc(t *testing.T) {
	var order string
	chain := NewChain()
	chain.Use(&testMiddleware{name: "test", priority: 100, marker: &order, markVal: "test"})

	handler := chain.ThenFunc(func(w http.ResponseWriter, r *http.Request) {
		order += "handler-"
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	expected := "test-before-handler-test-after-"
	if order != expected {
		t.Errorf("execution order wrong:\n  got:      %s\n  expected: %s", order, expected)
	}
}

func TestChainClone(t *testing.T) {
	original := NewChain()
	original.Use(&testMiddleware{name: "original", priority: 100})

	cloned := original.Clone()
	if cloned == nil {
		t.Fatal("Clone returned nil")
	}
	if cloned.Len() != original.Len() {
		t.Error("cloned chain has different length")
	}

	// Modify cloned chain
	cloned.Use(&testMiddleware{name: "added", priority: 200})
	if cloned.Len() != 2 {
		t.Error("cloned chain should have 2 middleware")
	}
	if original.Len() != 1 {
		t.Error("original chain should still have 1 middleware")
	}
}

func TestChainRemove(t *testing.T) {
	chain := NewChain()
	chain.Use(&testMiddleware{name: "keep", priority: 100})
	chain.Use(&testMiddleware{name: "remove", priority: 200})
	chain.Use(&testMiddleware{name: "also-keep", priority: 300})

	result := chain.Remove("remove")
	if result != chain {
		t.Error("Remove should return chain for chaining")
	}
	if chain.Len() != 2 {
		t.Errorf("expected 2 middleware, got %d", chain.Len())
	}

	// Removing non-existent should be no-op
	chain.Remove("non-existent")
	if chain.Len() != 2 {
		t.Errorf("expected 2 middleware, got %d", chain.Len())
	}
}

func TestChainGet(t *testing.T) {
	chain := NewChain()
	mw := &testMiddleware{name: "find-me", priority: 100}
	chain.Use(mw)
	chain.Use(&testMiddleware{name: "other", priority: 200})

	found := chain.Get("find-me")
	if found == nil {
		t.Error("Get returned nil for existing middleware")
	}
	if found.Name() != "find-me" {
		t.Error("Get returned wrong middleware")
	}

	notFound := chain.Get("non-existent")
	if notFound != nil {
		t.Error("Get should return nil for non-existent middleware")
	}
}

func TestChainClear(t *testing.T) {
	chain := NewChain()
	chain.Use(&testMiddleware{name: "one", priority: 100})
	chain.Use(&testMiddleware{name: "two", priority: 200})

	result := chain.Clear()
	if result != chain {
		t.Error("Clear should return chain for chaining")
	}
	if chain.Len() != 0 {
		t.Errorf("expected 0 middleware, got %d", chain.Len())
	}
}

func TestChainMiddlewares(t *testing.T) {
	chain := NewChain()
	chain.Use(&testMiddleware{name: "one", priority: 100})
	chain.Use(&testMiddleware{name: "two", priority: 200})

	middlewares := chain.Middlewares()
	if len(middlewares) != 2 {
		t.Errorf("expected 2 middleware, got %d", len(middlewares))
	}

	// Modifying returned slice should not affect chain
	_ = append(middlewares, &testMiddleware{name: "three", priority: 300})
	if chain.Len() != 2 {
		t.Errorf("chain should still have 2 middleware, got %d", chain.Len())
	}
}

func TestPriorityConstants(t *testing.T) {
	// Ensure priorities are in correct order (lower = earlier)
	if PrioritySecurity >= PriorityAuth {
		t.Error("Security should have lower priority than Auth")
	}
	if PriorityAuth >= PriorityRealIP {
		t.Error("Auth should have lower priority than RealIP")
	}
	if PriorityRealIP >= PriorityRequestID {
		t.Error("RealIP should have lower priority than RequestID")
	}
	if PriorityRequestID >= PriorityRateLimit {
		t.Error("RequestID should have lower priority than RateLimit")
	}
	if PriorityRateLimit >= PriorityCORS {
		t.Error("RateLimit should have lower priority than CORS")
	}
	if PriorityCORS >= PriorityHeaders {
		t.Error("CORS should have lower priority than Headers")
	}
	if PriorityHeaders >= PriorityCompress {
		t.Error("Headers should have lower priority than Compress")
	}
	if PriorityCompress >= PriorityMetrics {
		t.Error("Compress should have lower priority than Metrics")
	}
	if PriorityMetrics >= PriorityAccessLog {
		t.Error("Metrics should have lower priority than AccessLog")
	}
}

func TestChainStableSort(t *testing.T) {
	var order string
	chain := NewChain()

	// Add middleware with same priority - should preserve insertion order
	chain.Use(&testMiddleware{name: "first", priority: 100, marker: &order, markVal: "first"})
	chain.Use(&testMiddleware{name: "second", priority: 100, marker: &order, markVal: "second"})
	chain.Use(&testMiddleware{name: "third", priority: 100, marker: &order, markVal: "third"})

	handler := chain.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order += "handler-"
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Expected order: first -> second -> third -> handler (preserved insertion order)
	expected := "first-before-second-before-third-before-handler-third-after-second-after-first-after-"
	if order != expected {
		t.Errorf("stable sort failed:\n  got:      %s\n  expected: %s", order, expected)
	}
}

func TestChainEmpty(t *testing.T) {
	chain := NewChain()
	called := false

	handler := chain.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler should be called even with empty chain")
	}
}

// testSecretMiddleware implements both Middleware and SecretZeroer
type testSecretMiddleware struct {
	name     string
	priority int
	zeroed   bool
}

func (m *testSecretMiddleware) Name() string  { return m.name }
func (m *testSecretMiddleware) Priority() int { return m.priority }
func (m *testSecretMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}
func (m *testSecretMiddleware) ZeroSecrets() { m.zeroed = true }

func TestChain_ZeroSecrets(t *testing.T) {
	chain := NewChain()

	sec1 := &testSecretMiddleware{name: "sec1", priority: 1}
	sec2 := &testSecretMiddleware{name: "sec2", priority: 2}
	plain := &testMiddleware{name: "plain", priority: 3, marker: new(string)}

	chain.Use(sec1).Use(plain).Use(sec2)

	chain.ZeroSecrets()

	if !sec1.zeroed {
		t.Error("sec1 should have been zeroed")
	}
	if !sec2.zeroed {
		t.Error("sec2 should have been zeroed")
	}
}

func TestChain_ZeroSecrets_EmptyChain(t *testing.T) {
	chain := NewChain()
	chain.ZeroSecrets() // Should not panic
}

func TestChain_ZeroSecrets_NoZeroers(t *testing.T) {
	chain := NewChain()
	chain.Use(&testMiddleware{name: "m1", priority: 1, marker: new(string)})
	chain.ZeroSecrets() // Should not panic
}
