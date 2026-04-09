// Package admin provides the Admin API server for OpenLoadBalancer.
package admin

import (
	"time"
)

// Response is the standard API response format.
type Response struct {
	Success bool       `json:"success"`
	Data    any        `json:"data,omitempty"`
	Error   *ErrorInfo `json:"error,omitempty"`
}

// ErrorInfo provides structured error information.
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SystemInfo response containing version and build information.
type SystemInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	Uptime    string `json:"uptime"`
	State     string `json:"state"`
	GoVersion string `json:"go_version"`
}

// HealthStatus response for system health checks.
type HealthStatus struct {
	Status    string           `json:"status"`
	Checks    map[string]Check `json:"checks"`
	Timestamp time.Time        `json:"timestamp"`
}

// Check represents a single health check result.
type Check struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Count   int    `json:"count,omitempty"`
	Total   int    `json:"total,omitempty"`
}

// BackendPool response for pool information.
type BackendPool struct {
	Name      string    `json:"name"`
	Algorithm string    `json:"algorithm"`
	Backends  []Backend `json:"backends"`
}

// Backend response for backend information.
type Backend struct {
	ID       string `json:"id"`
	Address  string `json:"address"`
	Weight   int    `json:"weight"`
	State    string `json:"state"`
	Healthy  bool   `json:"healthy"`
	Requests int64  `json:"requests"`
	Errors   int64  `json:"errors"`
}

// Route response for route information.
type Route struct {
	Name        string            `json:"name"`
	Host        string            `json:"host,omitempty"`
	Path        string            `json:"path"`
	Methods     []string          `json:"methods,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	BackendPool string            `json:"backend_pool"`
	Priority    int               `json:"priority"`
}

// AddBackendRequest for adding a new backend.
type AddBackendRequest struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Weight  int    `json:"weight,omitempty"`
}

// MiddlewareStatusItem describes a single middleware's status.
type MiddlewareStatusItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Category    string `json:"category"`
}

// EventItem describes a system event for the activity feed.
type EventItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"` // success, info, warning, error
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// ErrorResponse creates an error response.
func ErrorResponse(code, message string) *Response {
	return &Response{
		Success: false,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
		},
	}
}

// SuccessResponse creates a success response with data.
func SuccessResponse(data any) *Response {
	return &Response{
		Success: true,
		Data:    data,
	}
}
