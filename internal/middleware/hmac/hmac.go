// Package hmac provides HMAC signature verification middleware.
// Useful for webhook verification and API request signing.
package hmac

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"hash"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config configures HMAC signature verification.
type Config struct {
	Enabled         bool     // Enable HMAC verification
	Secret          string   // HMAC secret key
	Algorithm       string   // Hash algorithm: "sha256", "sha512" (default: "sha256")
	Header          string   // Header containing signature (default: "X-Signature")
	Prefix          string   // Signature prefix (e.g., "sha256=")
	Encoding        string   // Signature encoding: "hex", "base64" (default: "hex")
	UseBody         bool     // Include request body in signature
	ExcludePaths    []string // Paths to exclude
	TimestampHeader string   // Optional timestamp header for replay protection
	MaxAge          string   // Maximum age for timestamp (e.g., "5m")
}

// DefaultConfig returns default HMAC configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:   false,
		Algorithm: "sha256",
		Header:    "X-Signature",
		Encoding:  "hex",
		UseBody:   true,
		MaxAge:    "5m",
	}
}

// Middleware provides HMAC signature verification.
type Middleware struct {
	config Config
	hasher func() hash.Hash
	maxAge time.Duration // Parsed MaxAge, resolved once at construction
	mu     sync.RWMutex
}

// New creates a new HMAC middleware.
func New(config Config) (*Middleware, error) {
	if !config.Enabled {
		return &Middleware{config: config}, nil
	}

	// Validate secret is not empty
	if config.Secret == "" {
		return nil, errorf("hmac: secret must not be empty when enabled")
	}

	m := &Middleware{
		config: config,
	}

	// Select hash algorithm
	switch strings.ToLower(config.Algorithm) {
	case "sha256":
		m.hasher = sha256.New
	case "sha512":
		m.hasher = sha512.New
	default:
		m.hasher = sha256.New
	}

	// Parse MaxAge once at construction time
	if config.MaxAge != "" {
		maxAge, err := time.ParseDuration(config.MaxAge)
		if err != nil {
			return nil, errorf("hmac: invalid max_age duration: " + config.MaxAge)
		}
		m.maxAge = maxAge
	}

	return m, nil
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "hmac"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 213 // After OAuth2 (212), before API Key (215)
}

// Wrap wraps the handler with HMAC verification.
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

		// Extract signature
		signature, err := m.extractSignature(r)
		if err != nil {
			m.unauthorized(w, "missing signature")
			return
		}

		// Replay protection via timestamp validation
		if m.config.TimestampHeader != "" && m.maxAge > 0 {
			tsStr := r.Header.Get(m.config.TimestampHeader)
			if tsStr == "" {
				m.unauthorized(w, "missing timestamp")
				return
			}
			tsUnix, parseErr := strconv.ParseInt(tsStr, 10, 64)
			if parseErr != nil {
				// Try RFC3339 format
				ts, timeErr := time.Parse(time.RFC3339, tsStr)
				if timeErr != nil {
					m.unauthorized(w, "invalid timestamp format")
					return
				}
				tsUnix = ts.Unix()
			}
			now := time.Now().Unix()
			diff := now - tsUnix
			if diff < 0 {
				diff = -diff
			}
			if time.Duration(diff)*time.Second > m.maxAge {
				m.unauthorized(w, "request timestamp expired")
				return
			}
		}

		// Compute expected signature
		expected, err := m.computeSignature(r)
		if err != nil {
			m.unauthorized(w, "failed to compute signature")
			return
		}

		// Compare signatures using constant-time comparison
		if !hmac.Equal([]byte(signature), []byte(expected)) {
			m.unauthorized(w, "invalid signature")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// extractSignature extracts the signature from the request.
func (m *Middleware) extractSignature(r *http.Request) (string, error) {
	sig := r.Header.Get(m.config.Header)
	if sig == "" {
		return "", errorf("signature header missing")
	}

	// Remove prefix if present
	if m.config.Prefix != "" {
		sig = strings.TrimPrefix(sig, m.config.Prefix)
	}

	return sig, nil
}

// computeSignature computes the expected HMAC signature.
func (m *Middleware) computeSignature(r *http.Request) (string, error) {
	// Build message to sign
	var message strings.Builder

	// Add method and path
	message.WriteString(r.Method)
	message.WriteString("\n")
	message.WriteString(r.URL.Path)
	message.WriteString("\n")

	// Add query string if present
	if r.URL.RawQuery != "" {
		message.WriteString(r.URL.RawQuery)
		message.WriteString("\n")
	}

	// Add timestamp if replay protection is configured
	if m.config.TimestampHeader != "" {
		message.WriteString(r.Header.Get(m.config.TimestampHeader))
		message.WriteString("\n")
	}

	// Add body if configured
	if m.config.UseBody && r.Body != nil {
		const maxBodySize = 10 << 20 // 10 MB
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize+1))
		if err != nil {
			return "", err
		}
		if len(body) > maxBodySize {
			return "", errorf("request body exceeds maximum allowed size")
		}
		// Restore body for downstream handlers
		r.Body = io.NopCloser(bytes.NewReader(body))
		message.Write(body)
	}

	// Compute HMAC
	h := hmac.New(m.hasher, []byte(m.config.Secret))
	h.Write([]byte(message.String()))
	sum := h.Sum(nil)

	// Encode signature
	switch m.config.Encoding {
	case "base64":
		return base64.StdEncoding.EncodeToString(sum), nil
	case "hex":
		return hex.EncodeToString(sum), nil
	default:
		return hex.EncodeToString(sum), nil
	}
}

// unauthorized writes unauthorized response.
func (m *Middleware) unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   "unauthorized",
		"message": message,
	})
}

// GenerateSignature generates an HMAC signature for a message.
// Useful for testing and client implementations.
func GenerateSignature(secret, message, algorithm, encoding string) (string, error) {
	var hasher func() hash.Hash
	switch strings.ToLower(algorithm) {
	case "sha256":
		hasher = sha256.New
	case "sha512":
		hasher = sha512.New
	default:
		hasher = sha256.New
	}

	h := hmac.New(hasher, []byte(secret))
	h.Write([]byte(message))
	sum := h.Sum(nil)

	switch encoding {
	case "base64":
		return base64.StdEncoding.EncodeToString(sum), nil
	case "hex":
		return hex.EncodeToString(sum), nil
	default:
		return hex.EncodeToString(sum), nil
	}
}

// errorf returns a simple error.
func errorf(msg string) error {
	return &simpleError{msg: msg}
}

type simpleError struct {
	msg string
}

func (e *simpleError) Error() string {
	return e.msg
}

// ZeroSecrets clears the HMAC secret from memory.
// Note: Go strings are immutable and their backing memory cannot be reliably zeroed.
// This sets the reference to empty to prevent further use; the GC will reclaim the old value.
func (m *Middleware) ZeroSecrets() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.Secret = ""
	m.config.Enabled = false
}
