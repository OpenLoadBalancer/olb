// Package csp provides Content Security Policy middleware.
package csp

import (
	crypto_rand "crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
)

// Config configures Content Security Policy.
type Config struct {
	Enabled         bool     // Enable CSP
	DefaultSrc      []string // default-src directive
	ScriptSrc       []string // script-src directive
	StyleSrc        []string // style-src directive
	ImgSrc          []string // img-src directive
	ConnectSrc      []string // connect-src directive
	FontSrc         []string // font-src directive
	ObjectSrc       []string // object-src directive
	MediaSrc        []string // media-src directive
	FrameSrc        []string // frame-src directive
	FrameAncestors  []string // frame-ancestors directive
	FormAction      []string // form-action directive
	BaseURI         []string // base-uri directive
	UpgradeInsecure bool     // upgrade-insecure-requests
	BlockAllMixed   bool     // block-all-mixed-content
	ReportURI       string   // report-uri directive
	ReportTo        string   // report-to directive
	NonceScript     bool     // Generate nonce for scripts
	NonceStyle      bool     // Generate nonce for styles
	UnsafeInline    bool     // Allow 'unsafe-inline' (not recommended)
	UnsafeEval      bool     // Allow 'unsafe-eval' (not recommended)
	ExcludePaths    []string // Paths to exclude
}

// DefaultConfig returns a restrictive default CSP configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		DefaultSrc:      []string{"'self'"},
		ScriptSrc:       []string{"'self'"},
		StyleSrc:        []string{"'self'"},
		ImgSrc:          []string{"'self'"},
		ConnectSrc:      []string{"'self'"},
		FontSrc:         []string{"'self'"},
		ObjectSrc:       []string{"'none'"},
		FrameAncestors:  []string{"'self'"},
		FormAction:      []string{"'self'"},
		UpgradeInsecure: true,
	}
}

// Middleware provides CSP headers.
type Middleware struct {
	config Config
	policy string // Pre-computed policy string
}

// New creates a new CSP middleware.
func New(config Config) (*Middleware, error) {
	if !config.Enabled {
		return &Middleware{config: config}, nil
	}

	m := &Middleware{
		config: config,
	}

	// Pre-compute policy string
	m.policy = m.buildPolicy()

	return m, nil
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "csp"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 610 // After CORS (600), before headers (700)
}

// Wrap wraps the handler with CSP headers.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	if !m.config.Enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check excluded paths
		for _, path := range m.config.ExcludePaths {
			if strings.HasPrefix(r.URL.Path, path) && (len(r.URL.Path) == len(path) || r.URL.Path[len(path)] == '/' || path[len(path)-1] == '/') {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Generate nonces if needed
		var scriptNonce, styleNonce string
		if m.config.NonceScript {
			scriptNonce = generateNonce()
			w.Header().Set("X-Script-Nonce", scriptNonce)
		}
		if m.config.NonceStyle {
			styleNonce = generateNonce()
			w.Header().Set("X-Style-Nonce", styleNonce)
		}

		// Set CSP header
		policy := m.policy
		if m.config.NonceScript || m.config.NonceStyle {
			policy = m.buildPolicyWithNonces(scriptNonce, styleNonce)
		}

		w.Header().Set("Content-Security-Policy", policy)

		next.ServeHTTP(w, r)
	})
}

// buildPolicy builds the CSP policy string.
func (m *Middleware) buildPolicy() string {
	var directives []string

	if len(m.config.DefaultSrc) > 0 {
		directives = append(directives, "default-src "+strings.Join(m.config.DefaultSrc, " "))
	}

	scriptSrc := m.config.ScriptSrc
	if m.config.UnsafeInline {
		scriptSrc = append(scriptSrc, "'unsafe-inline'")
	}
	if m.config.UnsafeEval {
		scriptSrc = append(scriptSrc, "'unsafe-eval'")
	}
	if len(scriptSrc) > 0 {
		directives = append(directives, "script-src "+strings.Join(scriptSrc, " "))
	}

	if len(m.config.StyleSrc) > 0 {
		directives = append(directives, "style-src "+strings.Join(m.config.StyleSrc, " "))
	}

	if len(m.config.ImgSrc) > 0 {
		directives = append(directives, "img-src "+strings.Join(m.config.ImgSrc, " "))
	}

	if len(m.config.ConnectSrc) > 0 {
		directives = append(directives, "connect-src "+strings.Join(m.config.ConnectSrc, " "))
	}

	if len(m.config.FontSrc) > 0 {
		directives = append(directives, "font-src "+strings.Join(m.config.FontSrc, " "))
	}

	if len(m.config.ObjectSrc) > 0 {
		directives = append(directives, "object-src "+strings.Join(m.config.ObjectSrc, " "))
	}

	if len(m.config.MediaSrc) > 0 {
		directives = append(directives, "media-src "+strings.Join(m.config.MediaSrc, " "))
	}

	if len(m.config.FrameSrc) > 0 {
		directives = append(directives, "frame-src "+strings.Join(m.config.FrameSrc, " "))
	}

	if len(m.config.FrameAncestors) > 0 {
		directives = append(directives, "frame-ancestors "+strings.Join(m.config.FrameAncestors, " "))
	}

	if len(m.config.FormAction) > 0 {
		directives = append(directives, "form-action "+strings.Join(m.config.FormAction, " "))
	}

	if len(m.config.BaseURI) > 0 {
		directives = append(directives, "base-uri "+strings.Join(m.config.BaseURI, " "))
	}

	if m.config.UpgradeInsecure {
		directives = append(directives, "upgrade-insecure-requests")
	}

	if m.config.BlockAllMixed {
		directives = append(directives, "block-all-mixed-content")
	}

	if m.config.ReportURI != "" {
		directives = append(directives, "report-uri "+m.config.ReportURI)
	}

	if m.config.ReportTo != "" {
		directives = append(directives, "report-to "+m.config.ReportTo)
	}

	return strings.Join(directives, "; ")
}

// buildPolicyWithNonces builds policy with nonces.
func (m *Middleware) buildPolicyWithNonces(scriptNonce, styleNonce string) string {
	policy := m.policy

	if m.config.NonceScript && scriptNonce != "" {
		policy = strings.ReplaceAll(policy, "script-src ", "script-src 'nonce-"+scriptNonce+"' ")
	}

	if m.config.NonceStyle && styleNonce != "" {
		policy = strings.ReplaceAll(policy, "style-src ", "style-src 'nonce-"+styleNonce+"' ")
	}

	return policy
}

// generateNonce generates a cryptographically random nonce.
func generateNonce() string {
	b := make([]byte, 16)
	_, _ = crypto_rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// GetPolicy returns the current CSP policy string.
func (m *Middleware) GetPolicy() string {
	return m.policy
}
