package waf

import (
	"reflect"
	"testing"

	"github.com/openloadbalancer/olb/internal/config"
)

func registeredDetectors(t *testing.T, mw *WAFMiddleware) int {
	t.Helper()
	return reflect.ValueOf(mw.detection).Elem().FieldByName("detectors").Len()
}

// A waf.detection block that only tunes thresholds used to register no
// detectors at all, because each detector needed an explicit enabled: true.
func TestWAF_PartialDetectionBlockKeepsDetectorsOn(t *testing.T) {
	off := false
	for _, tc := range []struct {
		name string
		det  *config.WAFDetectionConfig
		want int
	}{
		{"no detection block", nil, 6},
		{"threshold only", &config.WAFDetectionConfig{Threshold: config.WAFDetectionThreshold{Block: 60}}, 6},
		{"sqli explicitly off", &config.WAFDetectionConfig{Detectors: config.WAFDetectorsConfig{SQLi: config.WAFDetectorConfig{Enabled: &off}}}, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mw, err := NewWAFMiddleware(WAFMiddlewareConfig{
				Config: &config.WAFConfig{Enabled: true, Mode: "enforce", Detection: tc.det},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer mw.Stop()
			if got := registeredDetectors(t, mw); got != tc.want {
				t.Errorf("registered detectors = %d, want %d", got, tc.want)
			}
		})
	}
}

// A sanitizer block that only sets a size limit used to turn null-byte
// blocking, encoding normalisation and hop-by-hop stripping off.
func TestBuildSanitizerConfig_OmittedBoolsKeepSafeDefaults(t *testing.T) {
	sc := buildSanitizerConfig(&config.WAFSanitizerConfig{Enabled: true, MaxBodySize: 1 << 20})
	if !sc.BlockNullBytes || !sc.NormalizeEncoding || !sc.StripHopByHop {
		t.Errorf("omitted booleans turned off: %+v", sc)
	}
	off := false
	sc = buildSanitizerConfig(&config.WAFSanitizerConfig{Enabled: true, BlockNullBytes: &off})
	if sc.BlockNullBytes {
		t.Error("explicit block_null_bytes: false was ignored")
	}
}
