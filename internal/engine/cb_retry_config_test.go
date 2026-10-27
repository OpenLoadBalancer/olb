package engine

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
)

func TestCircuitBreakerConfigFrom(t *testing.T) {
	cfg, err := circuitBreakerConfigFrom(&config.CircuitBreakerConfig{
		Enabled: true, ErrorThreshold: 9, ErrorRateThreshold: 0.8, OpenDuration: "45s",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ErrorThreshold != 9 || cfg.ErrorRateThreshold != 0.8 || cfg.OpenDuration != 45*time.Second {
		t.Errorf("settings not applied: %+v", cfg)
	}

	def, err := circuitBreakerConfigFrom(&config.CircuitBreakerConfig{Enabled: true})
	if err != nil || def.ErrorThreshold != 5 || def.ErrorRateThreshold != 0.5 || def.OpenDuration != 30*time.Second {
		t.Errorf("defaults changed: %+v, %v", def, err)
	}

	for _, bad := range []config.CircuitBreakerConfig{
		{ErrorRateThreshold: 1.5},
		{ErrorRateThreshold: -0.1},
		{OpenDuration: "soon"},
		{OpenDuration: "-5s"},
	} {
		if _, err := circuitBreakerConfigFrom(&bad); err == nil {
			t.Errorf("circuitBreakerConfigFrom(%+v) accepted an invalid value", bad)
		}
	}
}

func TestRetryConfigFrom_AppliesRetryOn(t *testing.T) {
	cfg := retryConfigFrom(&config.RetryConfig{Enabled: true, MaxRetries: 2, RetryOn: []int{http.StatusTooManyRequests}})
	if cfg.MaxRetries != 2 || !reflect.DeepEqual(cfg.RetryOn, []int{http.StatusTooManyRequests}) {
		t.Errorf("settings not applied: %+v", cfg)
	}
	if def := retryConfigFrom(&config.RetryConfig{Enabled: true}); def.MaxRetries != 3 || len(def.RetryOn) != 3 {
		t.Errorf("defaults changed: %+v", def)
	}
}

// A circuit breaker config the engine can't apply must reject the config
// like any other enabled middleware.
func TestCircuitBreaker_InvalidOpenDurationRejected(t *testing.T) {
	cfg := createTestConfig()
	cfg.Middleware = &config.MiddlewareConfig{
		CircuitBreaker: &config.CircuitBreakerConfig{Enabled: true, OpenDuration: "later"},
	}
	if _, err := New(cfg, ""); err == nil {
		t.Fatal("New accepted an unparsable circuit_breaker.open_duration")
	}
}
