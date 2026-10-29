package waf

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/openloadbalancer/olb/internal/config"
)

func TestWAF_ErrorRateThresholdReachesTracker(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{Config: &config.WAFConfig{
		Enabled: true, Mode: "enforce",
		BotDetection: &config.WAFBotConfig{Enabled: true, Behavior: &config.WAFBehaviorConfig{Enabled: true, ErrorRateThreshold: 77}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	bt := reflect.ValueOf(mw.botDetect).Elem().FieldByName("behavior").Elem()
	if got := bt.FieldByName("ErrorRateThreshold").Float(); got != 77 {
		t.Errorf("ErrorRateThreshold = %v, want 77", got)
	}
}

func TestWAF_PermissionsPolicyHeader(t *testing.T) {
	mw, err := NewWAFMiddleware(WAFMiddlewareConfig{Config: &config.WAFConfig{
		Enabled: true, Mode: "enforce",
		Response: &config.WAFResponseConfig{SecurityHeaders: &config.WAFSecurityHeadersConfig{
			Enabled: true, PermissionsPolicy: "camera=(self)",
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.9:1"
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Permissions-Policy"); got != "camera=(self)" {
		t.Errorf("Permissions-Policy = %q", got)
	}
}
