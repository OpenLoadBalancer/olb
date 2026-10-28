package waf

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openloadbalancer/olb/internal/config"
)

func serveAttack(t *testing.T, cfg *config.WAFConfig) int {
	t.Helper()
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	h := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/search?q=1%27%20UNION%20SELECT%20password%20FROM%20users--", nil)
	req.RemoteAddr = "203.0.113.9:1234"
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestWAF_DetectionLayerMode(t *testing.T) {
	base := func(d *config.WAFDetectionConfig) *config.WAFConfig {
		return &config.WAFConfig{Enabled: true, Mode: "enforce", Detection: d}
	}
	off := false
	if c := serveAttack(t, base(nil)); c != http.StatusForbidden {
		t.Fatalf("baseline attack not blocked: %d", c)
	}
	if c := serveAttack(t, base(&config.WAFDetectionConfig{Mode: "monitor"})); c != http.StatusOK {
		t.Errorf("detection.mode monitor must only log: %d", c)
	}
	if c := serveAttack(t, base(&config.WAFDetectionConfig{Enabled: &off})); c != http.StatusOK {
		t.Errorf("detection.enabled false must skip the layer: %d", c)
	}
	if c := serveAttack(t, &config.WAFConfig{Enabled: true, Mode: "monitor", Detection: &config.WAFDetectionConfig{Mode: "enforce"}}); c != http.StatusOK {
		t.Errorf("global monitor must win over layer enforce: %d", c)
	}
}
