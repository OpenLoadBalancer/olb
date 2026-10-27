package waf

import (
	"github.com/openloadbalancer/olb/internal/config"
)

// DefaultWAFConfig returns a WAF configuration with sensible defaults.
func DefaultWAFConfig() *config.WAFConfig {
	return &config.WAFConfig{
		Enabled: true,
		Mode:    "enforce",
		IPACL: &config.WAFIPACLConfig{
			Enabled: true,
			AutoBan: &config.WAFAutoBanConfig{
				Enabled:    true,
				DefaultTTL: "1h",
				MaxTTL:     "24h",
			},
		},
		Sanitizer: &config.WAFSanitizerConfig{
			Enabled:           true,
			MaxHeaderSize:     8192,
			MaxHeaderCount:    100,
			MaxBodySize:       10 * 1024 * 1024,
			MaxURLLength:      8192,
			MaxCookieSize:     4096,
			MaxCookieCount:    50,
			BlockNullBytes:    boolPtr(true),
			NormalizeEncoding: boolPtr(true),
			StripHopByHop:     boolPtr(true),
		},
		Detection: &config.WAFDetectionConfig{
			Enabled: true,
			Mode:    "enforce",
			Threshold: config.WAFDetectionThreshold{
				Block: 50,
				Log:   25,
			},
			Detectors: config.WAFDetectorsConfig{
				SQLi:          config.WAFDetectorConfig{Enabled: boolPtr(true), ScoreMultiplier: 1.0},
				XSS:           config.WAFDetectorConfig{Enabled: boolPtr(true), ScoreMultiplier: 1.0},
				PathTraversal: config.WAFDetectorConfig{Enabled: boolPtr(true), ScoreMultiplier: 1.0},
				CMDi:          config.WAFDetectorConfig{Enabled: boolPtr(true), ScoreMultiplier: 1.0},
				XXE:           config.WAFDetectorConfig{Enabled: boolPtr(true), ScoreMultiplier: 1.0},
				SSRF:          config.WAFDetectorConfig{Enabled: boolPtr(true), ScoreMultiplier: 1.0},
			},
		},
		BotDetection: &config.WAFBotConfig{
			Enabled: true,
			Mode:    "monitor",
			UserAgent: &config.WAFUserAgentConfig{
				Enabled:            true,
				BlockEmpty:         true,
				BlockKnownScanners: true,
			},
			Behavior: &config.WAFBehaviorConfig{
				Enabled:            true,
				Window:             "5m",
				RPSThreshold:       10,
				ErrorRateThreshold: 30,
			},
		},
		Response: &config.WAFResponseConfig{
			SecurityHeaders: &config.WAFSecurityHeadersConfig{
				Enabled:             true,
				XContentTypeOptions: true,
				XFrameOptions:       "SAMEORIGIN",
				ReferrerPolicy:      "strict-origin-when-cross-origin",
				HSTS: &config.WAFHSTSConfig{
					Enabled:           true,
					MaxAge:            31536000,
					IncludeSubdomains: true,
				},
			},
			DataMasking: &config.WAFDataMaskingConfig{
				Enabled:          true,
				MaskCreditCards:  true,
				MaskAPIKeys:      true,
				StripStackTraces: true,
			},
			ErrorPages: &config.WAFErrorPagesConfig{
				Enabled: true,
				Mode:    "production",
			},
		},
		Logging: &config.WAFLoggingConfig{
			Level:      "info",
			Format:     "json",
			LogBlocked: true,
		},
	}
}

func boolPtr(b bool) *bool { return &b }
