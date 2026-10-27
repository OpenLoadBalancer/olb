package l7

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/balancer"
)

// A shadow request is fire-and-forget: it must still be sent when the
// original request is already finished (its context cancelled), which is
// the normal case. It used to derive its context from the client request
// and was aborted, so mirroring was lost whenever the primary response won
// the race.
func TestShadowRequest_SurvivesCancelledClientContext(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-OLB-Shadow") == "true" {
			hits.Add(1)
		}
	}))
	defer srv.Close()

	be := backend.NewBackend("s", srv.Listener.Addr().String())
	be.SetState(backend.StateUp)
	sm := NewShadowManager(ShadowConfig{Enabled: true, Percentage: 100, Timeout: 5 * time.Second})
	sm.AddTarget(balancer.NewRoundRobin(), []*backend.Backend{be}, 100)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "http://example.invalid/x", nil).WithContext(ctx)
	sm.ShadowRequest(req)
	cancel() // the primary request finished before the shadow goroutine ran
	sm.Wait()

	if hits.Load() != 1 {
		t.Errorf("shadow requests received = %d, want 1 (aborted by the cancelled client context)", hits.Load())
	}
}

// The shadow goroutine must not read the live request: the handler keeps
// using (and may modify) it after ShadowRequest returns.
func TestShadowRequest_DoesNotRaceWithRequestMutation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	be := backend.NewBackend("s", srv.Listener.Addr().String())
	be.SetState(backend.StateUp)
	sm := NewShadowManager(ShadowConfig{Enabled: true, Percentage: 100, CopyHeaders: true, Timeout: 5 * time.Second})
	sm.AddTarget(balancer.NewRoundRobin(), []*backend.Backend{be}, 100)

	for i := 0; i < 50; i++ {
		req := httptest.NewRequest(http.MethodGet, "http://example.invalid/x", nil)
		req.Header.Set("X-A", "1")
		sm.ShadowRequest(req)
		// What the proxy does to the request after mirroring.
		req.Header.Del("X-A")
		req.Header.Set("X-B", "2")
		req.URL.Path = "/changed"
	}
	sm.Wait()
}
