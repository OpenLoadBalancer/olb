// Package admin provides the Admin API HTTP handlers for OpenLoadBalancer.
package admin

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

// writeError writes an error response with the given status code and error details.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := ErrorResponse(code, message)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("admin: failed to encode error response: %v", err)
	}
}

// writeSuccess writes a success response with data.
func writeSuccess(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := SuccessResponse(data)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("admin: failed to encode success response: %v", err)
	}
}

// readBody reads and returns the request body, limited to 1MB.
func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(io.LimitReader(r.Body, 1<<20))
}

// readJSONBody reads a JSON request body with size limiting and strict decoding.
// It limits the body to 1MB and disallows unknown fields.
func readJSONBody(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// Helper type for extended backend info
type BackendInfo struct {
	ID            string            `json:"id"`
	Address       string            `json:"address"`
	Weight        int32             `json:"weight"`
	MaxConns      int32             `json:"max_conns"`
	State         string            `json:"state"`
	Healthy       bool              `json:"healthy"`
	ActiveConns   int64             `json:"active_conns"`
	TotalRequests int64             `json:"total_requests"`
	TotalErrors   int64             `json:"total_errors"`
	TotalBytes    int64             `json:"total_bytes"`
	AvgLatency    string            `json:"avg_latency"`
	LastLatency   string            `json:"last_latency"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// Helper type for extended pool info
type PoolInfo struct {
	Name        string           `json:"name"`
	Algorithm   string           `json:"algorithm"`
	Backends    []BackendInfo    `json:"backends"`
	Total       int              `json:"total"`
	Healthy     int              `json:"healthy"`
	HealthCheck *HealthCheckInfo `json:"health_check,omitempty"`
}

// HealthCheckInfo contains health check configuration.
type HealthCheckInfo struct {
	Enabled  bool          `json:"enabled"`
	Interval time.Duration `json:"interval"`
	Timeout  time.Duration `json:"timeout"`
	Path     string        `json:"path,omitempty"`
	Port     int           `json:"port,omitempty"`
}

// Helper type for health status with extended info
type HealthStatusInfo struct {
	BackendID string    `json:"backend_id"`
	Status    string    `json:"status"`
	LastCheck time.Time `json:"last_check,omitempty"`
	Latency   string    `json:"latency,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// UpdateBackendRequest for updating backend settings.
type UpdateBackendRequest struct {
	Weight   *int32 `json:"weight,omitempty"`
	MaxConns *int32 `json:"max_conns,omitempty"`
}

// extractPoolName extracts the pool name from a URL path like /api/v1/backends/:pool
func extractPoolName(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Path format: api/v1/backends/:pool
	if len(parts) >= 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "backends" {
		return parts[3]
	}
	return ""
}

// extractBackendID extracts pool and backend IDs from a URL path like /api/v1/backends/:pool/:backend
func extractBackendID(path string) (pool, backend string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Path format: api/v1/backends/:pool/:backend
	if len(parts) >= 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "backends" {
		return parts[3], parts[4]
	}
	return "", ""
}

// poolToInfo converts a Pool to PoolInfo.
func poolToInfo(pool *backend.Pool) PoolInfo {
	backends := pool.GetAllBackends()
	response := PoolInfo{
		Name:      pool.Name,
		Algorithm: pool.Algorithm,
		Backends:  make([]BackendInfo, 0, len(backends)),
		Total:     len(backends),
	}

	for _, b := range backends {
		response.Backends = append(response.Backends, backendToInfo(b))
		if b.IsHealthy() {
			response.Healthy++
		}
	}

	if pool.HealthCheck != nil {
		response.HealthCheck = &HealthCheckInfo{
			Enabled:  pool.HealthCheck.Enabled,
			Interval: pool.HealthCheck.Interval,
			Timeout:  pool.HealthCheck.Timeout,
			Path:     pool.HealthCheck.Path,
			Port:     pool.HealthCheck.Port,
		}
	}

	return response
}

// backendToInfo converts a Backend to BackendInfo.
func backendToInfo(b *backend.Backend) BackendInfo {
	return BackendInfo{
		ID:            b.ID,
		Address:       b.Address,
		Weight:        b.Weight,
		MaxConns:      b.MaxConns,
		State:         b.State().String(),
		Healthy:       b.IsHealthy(),
		ActiveConns:   b.ActiveConns(),
		TotalRequests: b.TotalRequests(),
		TotalErrors:   b.TotalErrors(),
		TotalBytes:    b.TotalBytes(),
		AvgLatency:    b.AvgLatency().String(),
		LastLatency:   b.LastLatency().String(),
		Metadata:      b.GetAllMetadata(),
	}
}
