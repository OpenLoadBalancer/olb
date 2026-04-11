package engine

import (
	"os"
	"time"

	"github.com/openloadbalancer/olb/internal/admin"
	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/geodns"
	"github.com/openloadbalancer/olb/internal/logging"
)

// setState sets the engine state (internal use only).
func (e *Engine) setState(state State) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state = state
}

// createLoggerWithOutput creates the logger and optionally returns a rotating file
// output reference for SIGUSR1 log reopening.
func createLoggerWithOutput(cfg *config.Logging) (*logging.Logger, *logging.RotatingFileOutput) {
	var output logging.Output
	var rotatingOut *logging.RotatingFileOutput

	if cfg == nil {
		// Default to stdout JSON
		output = logging.NewJSONOutput(os.Stdout)
	} else {
		switch cfg.Output {
		case "stdout":
			if cfg.Format == "text" {
				output = logging.NewTextOutput(os.Stdout)
			} else {
				output = logging.NewJSONOutput(os.Stdout)
			}
		case "stderr":
			if cfg.Format == "text" {
				output = logging.NewTextOutput(os.Stderr)
			} else {
				output = logging.NewJSONOutput(os.Stderr)
			}
		default:
			// File output - use rotating file output
			rotatingOutput, err := logging.NewRotatingFileOutput(logging.RotatingFileOptions{
				Filename:   cfg.Output,
				MaxSize:    100 * 1024 * 1024, // 100MB
				MaxBackups: 10,
				Compress:   true,
			})
			if err != nil {
				// Fallback to stdout
				output = logging.NewJSONOutput(os.Stdout)
			} else {
				output = rotatingOutput
				rotatingOut = rotatingOutput
			}
		}
	}

	logger := logging.New(output)
	if cfg != nil {
		logger.SetLevel(logging.ParseLevel(cfg.Level))
	}
	return logger, rotatingOut
}

// getAdminAddress returns the admin server address from config.
func getAdminAddress(cfg *config.Config) string {
	if cfg.Admin != nil && cfg.Admin.Address != "" {
		return cfg.Admin.Address
	}
	return ":8080"
}

// parseDuration parses a duration string with a default value.
func parseDuration(s string, defaultVal time.Duration) time.Duration {
	if s == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultVal
	}
	return d
}

// convertGeoDNSRules converts config.GeoDNSRule to geodns.GeoRule.
func convertGeoDNSRules(rules []config.GeoDNSRule) []geodns.GeoRule {
	result := make([]geodns.GeoRule, 0, len(rules))
	for _, r := range rules {
		result = append(result, geodns.GeoRule{
			ID:       r.ID,
			Country:  r.Country,
			Region:   r.Region,
			Pool:     r.Pool,
			Fallback: r.Fallback,
			Weight:   r.Weight,
			Headers:  r.Headers,
		})
	}
	return result
}

// buildMiddlewareStatus returns a list of middleware status items from the
// current configuration. It reads the middleware config section and determines
// which middleware components are enabled.
func (e *Engine) buildMiddlewareStatus() []admin.MiddlewareStatusItem {
	e.mu.RLock()
	cfg := e.config
	e.mu.RUnlock()

	type entry struct {
		id, name, desc, category string
	}

	entries := []entry{
		{"rate_limit", "Rate Limiting", "Limit requests per IP or user", "traffic"},
		{"cors", "CORS", "Cross-Origin Resource Sharing", "security"},
		{"csp", "Content Security Policy", "CSP header management", "security"},
		{"compression", "Compression", "Gzip/Brotli response compression", "performance"},
		{"circuit_breaker", "Circuit Breaker", "Fail-fast backend protection", "performance"},
		{"retry", "Retry", "Automatic request retry", "performance"},
		{"cache", "HTTP Cache", "Response caching with TTL", "performance"},
		{"ip_filter", "IP Filter", "Allow/deny IP ranges", "security"},
		{"headers", "Headers", "Custom request/response headers", "traffic"},
		{"timeout", "Timeout", "Request timeout configuration", "traffic"},
		{"max_body_size", "Max Body Size", "Limit request body size", "security"},
		{"jwt", "JWT Auth", "JSON Web Token authentication", "security"},
		{"oauth2", "OAuth2", "OAuth 2.0 authentication", "security"},
		{"basic_auth", "Basic Auth", "HTTP Basic authentication", "security"},
		{"api_key", "API Key Auth", "API key based authentication", "security"},
		{"hmac", "HMAC Auth", "HMAC request signing", "security"},
		{"transformer", "Request Transform", "Modify request/response headers and body", "traffic"},
		{"request_id", "Request ID", "Unique request identification", "observability"},
		{"logging", "Access Logging", "Request/response logging", "observability"},
		{"metrics", "Metrics", "Prometheus metrics collection", "observability"},
		{"rewrite", "URL Rewrite", "URL path rewriting", "traffic"},
		{"forcessl", "Force SSL", "Redirect HTTP to HTTPS", "security"},
		{"csrf", "CSRF Protection", "Cross-site request forgery protection", "security"},
		{"secure_headers", "Secure Headers", "Security header management", "security"},
		{"coalesce", "Request Coalescing", "Deduplicate concurrent requests", "performance"},
		{"bot_detection", "Bot Detection", "Automated bot identification", "security"},
		{"real_ip", "Real IP", "Extract real client IP from headers", "observability"},
		{"trace", "Distributed Tracing", "OpenTelemetry-style tracing", "observability"},
		{"validator", "Validator", "Request validation", "security"},
		{"strip_prefix", "Strip Prefix", "Remove path prefix before proxying", "traffic"},
	}

	result := make([]admin.MiddlewareStatusItem, 0, len(entries))

	var mw *config.MiddlewareConfig
	if cfg != nil {
		mw = cfg.Middleware
	}

	for _, e := range entries {
		enabled := isMWEnabled(mw, e.id)
		result = append(result, admin.MiddlewareStatusItem{
			ID:          e.id,
			Name:        e.name,
			Description: e.desc,
			Enabled:     enabled,
			Category:    e.category,
		})
	}

	return result
}

// isMWEnabled checks if a middleware component is enabled in config.
func isMWEnabled(mw *config.MiddlewareConfig, id string) bool {
	if mw == nil {
		return false
	}
	switch id {
	case "rate_limit":
		return mw.RateLimit != nil && mw.RateLimit.Enabled
	case "cors":
		return mw.CORS != nil && mw.CORS.Enabled
	case "csp":
		return mw.CSP != nil && mw.CSP.Enabled
	case "compression":
		return mw.Compression != nil && mw.Compression.Enabled
	case "circuit_breaker":
		return mw.CircuitBreaker != nil && mw.CircuitBreaker.Enabled
	case "retry":
		return mw.Retry != nil && mw.Retry.Enabled
	case "cache":
		return mw.Cache != nil && mw.Cache.Enabled
	case "ip_filter":
		return mw.IPFilter != nil && mw.IPFilter.Enabled
	case "headers":
		return mw.Headers != nil && mw.Headers.Enabled
	case "timeout":
		return mw.Timeout != nil && mw.Timeout.Enabled
	case "max_body_size":
		return mw.MaxBodySize != nil && mw.MaxBodySize.Enabled
	case "jwt":
		return mw.JWT != nil && mw.JWT.Enabled
	case "oauth2":
		return mw.OAuth2 != nil && mw.OAuth2.Enabled
	case "basic_auth":
		return mw.BasicAuth != nil && mw.BasicAuth.Enabled
	case "api_key":
		return mw.APIKey != nil && mw.APIKey.Enabled
	case "hmac":
		return mw.HMAC != nil && mw.HMAC.Enabled
	case "transformer":
		return mw.Transformer != nil && mw.Transformer.Enabled
	case "request_id":
		return mw.RequestID != nil && mw.RequestID.Enabled
	case "logging":
		return mw.Logging != nil && mw.Logging.Enabled
	case "metrics":
		return mw.Metrics != nil && mw.Metrics.Enabled
	case "rewrite":
		return mw.Rewrite != nil && mw.Rewrite.Enabled
	case "forcessl":
		return mw.ForceSSL != nil && mw.ForceSSL.Enabled
	case "csrf":
		return mw.CSRF != nil && mw.CSRF.Enabled
	case "secure_headers":
		return mw.SecureHeaders != nil && mw.SecureHeaders.Enabled
	case "coalesce":
		return mw.Coalesce != nil && mw.Coalesce.Enabled
	case "bot_detection":
		return mw.BotDetection != nil && mw.BotDetection.Enabled
	case "real_ip":
		return mw.RealIP != nil && mw.RealIP.Enabled
	case "trace":
		return mw.Trace != nil && mw.Trace.Enabled
	case "validator":
		return mw.Validator != nil && mw.Validator.Enabled
	case "strip_prefix":
		return mw.StripPrefix != nil && mw.StripPrefix.Enabled
	default:
		return false
	}
}
