// Package validator provides request/response validation middleware.
package validator

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

// Config configures request/response validation.
type Config struct {
	Enabled          bool              // Enable validation
	ValidateRequest  bool              // Validate incoming requests
	ValidateResponse bool              // Validate outgoing responses
	MaxBodySize      int64             // Maximum body size to validate (default: 1MB)
	ContentTypes     []string          // Content types to validate (default: application/json)
	RequiredHeaders  map[string]string // Header name -> regex pattern
	ForbiddenHeaders []string          // Headers that must not be present
	QueryRules       map[string]string // Query param -> regex pattern
	PathPatterns     map[string]string // Path prefix -> pattern
	ExcludePaths     []string          // Paths to exclude from validation
	RejectOnFailure  bool              // Reject request on validation failure
	LogOnly          bool              // Only log violations, don't reject
}

// DefaultConfig returns default validation configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		ValidateRequest: true,
		MaxBodySize:     1024 * 1024, // 1MB
		ContentTypes:    []string{"application/json"},
		RejectOnFailure: true,
		LogOnly:         false,
	}
}

// ValidationError represents a validation failure.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Type    string `json:"type"` // "header", "query", "body", "path"
}

// Middleware provides request/response validation.
type Middleware struct {
	config   Config
	patterns map[string]*regexp.Regexp
	mu       sync.RWMutex
}

// New creates a new validation middleware.
func New(config Config) (*Middleware, error) {
	if !config.Enabled {
		return &Middleware{config: config}, nil
	}

	m := &Middleware{
		config:   config,
		patterns: make(map[string]*regexp.Regexp),
	}

	// Compile regex patterns
	for name, pattern := range config.RequiredHeaders {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		m.patterns["header:"+name] = re
	}

	for name, pattern := range config.QueryRules {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		m.patterns["query:"+name] = re
	}

	for prefix, pattern := range config.PathPatterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		m.patterns["path:"+prefix] = re
	}

	return m, nil
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "validator"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 145 // After Strip Prefix (140), before WAF (200)
}

// Wrap wraps the handler with validation.
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

		// Validate request if enabled
		if m.config.ValidateRequest {
			errors := m.validateRequest(r)
			if len(errors) > 0 {
				if !m.config.LogOnly {
					m.validationFailed(w, errors)
					return
				}
				// In log-only mode, continue but add errors to context
				ctx := WithValidationErrors(r.Context(), errors)
				r = r.WithContext(ctx)
			}
		}

		next.ServeHTTP(w, r)
	})
}

// validateRequest validates the incoming request.
func (m *Middleware) validateRequest(r *http.Request) []ValidationError {
	var errors []ValidationError

	// Validate required headers
	for headerName := range m.config.RequiredHeaders {
		value := r.Header.Get(headerName)
		if value == "" {
			errors = append(errors, ValidationError{
				Field:   headerName,
				Message: "required header missing",
				Type:    "header",
			})
			continue
		}

		if re := m.patterns["header:"+headerName]; re != nil {
			if !re.MatchString(value) {
				errors = append(errors, ValidationError{
					Field:   headerName,
					Message: "header value does not match pattern",
					Type:    "header",
				})
			}
		}
	}

	// Validate forbidden headers
	for _, headerName := range m.config.ForbiddenHeaders {
		if r.Header.Get(headerName) != "" {
			errors = append(errors, ValidationError{
				Field:   headerName,
				Message: "forbidden header present",
				Type:    "header",
			})
		}
	}

	// Validate query parameters
	for paramName := range m.config.QueryRules {
		value := r.URL.Query().Get(paramName)
		if value == "" {
			errors = append(errors, ValidationError{
				Field:   paramName,
				Message: "required query parameter missing",
				Type:    "query",
			})
			continue
		}

		if re := m.patterns["query:"+paramName]; re != nil {
			if !re.MatchString(value) {
				errors = append(errors, ValidationError{
					Field:   paramName,
					Message: "query parameter does not match pattern",
					Type:    "query",
				})
			}
		}
	}

	// Validate path pattern
	for prefix, pattern := range m.config.PathPatterns {
		if strings.HasPrefix(r.URL.Path, prefix) {
			if re := m.patterns["path:"+prefix]; re != nil {
				if !re.MatchString(r.URL.Path) {
					errors = append(errors, ValidationError{
						Field:   "path",
						Message: "path does not match pattern: " + pattern,
						Type:    "path",
					})
				}
			}
		}
	}

	// Validate body content type and size
	if m.shouldValidateBody(r) {
		if bodyErrors := m.validateBody(r); len(bodyErrors) > 0 {
			errors = append(errors, bodyErrors...)
		}
	}

	return errors
}

// shouldValidateBody checks if request body should be validated.
func (m *Middleware) shouldValidateBody(r *http.Request) bool {
	if r.Body == nil {
		return false
	}

	if r.ContentLength > m.config.MaxBodySize {
		return false
	}

	contentType := r.Header.Get("Content-Type")
	for _, ct := range m.config.ContentTypes {
		if strings.Contains(contentType, ct) {
			return true
		}
	}
	return false
}

// validateBody validates request body.
func (m *Middleware) validateBody(r *http.Request) []ValidationError {
	var errors []ValidationError

	// Read body
	body, err := io.ReadAll(io.LimitReader(r.Body, m.config.MaxBodySize))
	if err != nil {
		return []ValidationError{{
			Field:   "body",
			Message: "failed to read body: " + err.Error(),
			Type:    "body",
		}}
	}

	// Restore body for downstream handlers
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	// Check if JSON
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		if !json.Valid(body) {
			errors = append(errors, ValidationError{
				Field:   "body",
				Message: "invalid JSON",
				Type:    "body",
			})
		}
	}

	return errors
}

// validationFailed writes validation error response.
func (m *Middleware) validationFailed(w http.ResponseWriter, errors []ValidationError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":      "validation_failed",
		"message":    "request validation failed",
		"violations": errors,
	})
}

// contextKey is the key for validation errors in context.
type contextKey int

const validationErrorsKey contextKey = 0

// WithValidationErrors adds validation errors to context.
func WithValidationErrors(ctx context.Context, errors []ValidationError) context.Context {
	return context.WithValue(ctx, validationErrorsKey, errors)
}

// GetValidationErrors retrieves validation errors from context.
func GetValidationErrors(ctx context.Context) []ValidationError {
	if errors, ok := ctx.Value(validationErrorsKey).([]ValidationError); ok {
		return errors
	}
	return nil
}

// HasValidationErrors checks if there are validation errors in context.
func HasValidationErrors(ctx context.Context) bool {
	return len(GetValidationErrors(ctx)) > 0
}
