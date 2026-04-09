// Package admin provides the Admin API HTTP handlers for OpenLoadBalancer.
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/pkg/errors"
	"github.com/openloadbalancer/olb/pkg/version"
)

// writeError writes an error response with the given status code and error details.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := ErrorResponse(code, message)
	json.NewEncoder(w).Encode(resp)
}

// writeSuccess writes a success response with data.
func writeSuccess(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := SuccessResponse(data)
	json.NewEncoder(w).Encode(resp)
}

// readBody reads and returns the request body, limited to 1MB.
func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(io.LimitReader(r.Body, 1<<20))
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

// getVersion handles GET /api/v1/version
func (s *Server) getVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	writeSuccess(w, map[string]string{
		"version":    version.Version,
		"commit":     version.Commit,
		"build_date": version.Date,
		"go_version": version.GoVersion,
		"platform":   version.Platform,
	})
}

// listPools handles GET /api/v1/pools
func (s *Server) listPools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	if s.poolManager == nil {
		writeSuccess(w, []PoolInfo{})
		return
	}

	pools := s.poolManager.GetAllPools()
	response := make([]PoolInfo, 0, len(pools))
	for _, pool := range pools {
		response = append(response, poolToInfo(pool))
	}

	writeSuccess(w, response)
}

// handlePoolDetail handles requests to /api/v1/pools/...
func (s *Server) handlePoolDetail(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")

	// /api/v1/pools/:pool (4 parts)
	if len(parts) == 4 {
		switch r.Method {
		case http.MethodGet:
			s.getPoolByName(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		}
	} else {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "invalid path")
	}
}

// getPoolByName handles GET /api/v1/pools/:pool
func (s *Server) getPoolByName(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		writeError(w, http.StatusBadRequest, "INVALID_POOL", "pool name is required")
		return
	}
	poolName := parts[3]

	if s.poolManager == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found")
		return
	}

	pool := s.poolManager.GetPool(poolName)
	if pool == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found: "+poolName)
		return
	}

	writeSuccess(w, poolToInfo(pool))
}

// getSystemInfo handles GET /api/v1/system/info
func (s *Server) getSystemInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	info := SystemInfo{
		Version:   version.Version,
		Commit:    version.Commit,
		BuildDate: version.Date,
		GoVersion: version.GoVersion,
		Uptime:    time.Since(s.startTime).String(),
		State:     s.GetState(),
	}

	writeSuccess(w, info)
}

// getSystemHealth handles GET /api/v1/system/health
func (s *Server) getSystemHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	status := "healthy"
	checks := make(map[string]Check)

	// Check router
	if s.router != nil {
		routes := s.router.Routes()
		if len(routes) > 0 {
			checks["router"] = Check{Status: "ok", Message: "router operational"}
		} else {
			checks["router"] = Check{Status: "warning", Message: "no routes configured"}
		}
	} else {
		checks["router"] = Check{Status: "error", Message: "router not available"}
		status = "degraded"
	}

	// Check pool manager
	if s.poolManager != nil {
		pools := s.poolManager.GetAllPools()
		if len(pools) > 0 {
			checks["pool_manager"] = Check{Status: "ok", Message: "pool manager operational"}
		} else {
			checks["pool_manager"] = Check{Status: "warning", Message: "no pools configured"}
		}
	} else {
		checks["pool_manager"] = Check{Status: "error", Message: "pool manager not available"}
		status = "degraded"
	}

	// Check health checker
	if s.healthChecker != nil {
		statuses := s.healthChecker.ListStatuses()
		healthyCount := 0
		for _, st := range statuses {
			if st == health.StatusHealthy {
				healthyCount++
			}
		}
		checks["health_checker"] = Check{
			Status:  "ok",
			Message: "health checker operational",
		}
		checks["backends"] = Check{
			Status:  "ok",
			Message: "healthy",
			Count:   healthyCount,
			Total:   len(statuses),
		}
	} else {
		checks["health_checker"] = Check{Status: "warning", Message: "health checker not available"}
	}

	health := HealthStatus{
		Status:    status,
		Checks:    checks,
		Timestamp: time.Now(),
	}

	writeSuccess(w, health)
}

// reloadConfig handles POST /api/v1/system/reload
func (s *Server) reloadConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is allowed")
		return
	}

	if s.onReload == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_AVAILABLE", "reload callback not configured")
		return
	}

	err := s.circuitBreaker.Execute(func(ctx context.Context) error {
		return s.onReload()
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "RELOAD_FAILED", err.Error())
		return
	}

	writeSuccess(w, map[string]string{"message": "configuration reloaded successfully"})
}

// listBackends handles GET /api/v1/backends
func (s *Server) listBackends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	if s.poolManager == nil {
		writeSuccess(w, []string{})
		return
	}

	pools := s.poolManager.GetAllPools()
	names := make([]string, 0, len(pools))
	for _, pool := range pools {
		names = append(names, pool.Name)
	}

	writeSuccess(w, names)
}

// getPool handles GET /api/v1/backends/:pool
func (s *Server) getPool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	poolName := extractPoolName(r.URL.Path)
	if poolName == "" {
		writeError(w, http.StatusBadRequest, "INVALID_POOL", "pool name is required")
		return
	}

	if s.poolManager == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found")
		return
	}

	pool := s.poolManager.GetPool(poolName)
	if pool == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found: "+poolName)
		return
	}

	response := poolToInfo(pool)
	writeSuccess(w, response)
}

// addBackend handles POST /api/v1/backends/:pool
func (s *Server) addBackend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is allowed")
		return
	}

	poolName := extractPoolName(r.URL.Path)
	if poolName == "" {
		writeError(w, http.StatusBadRequest, "INVALID_POOL", "pool name is required")
		return
	}

	if s.poolManager == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found")
		return
	}

	pool := s.poolManager.GetPool(poolName)
	if pool == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found: "+poolName)
		return
	}

	var req AddBackendRequest
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON payload")
		return
	}

	// Validate required fields
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_FIELD", "backend ID is required")
		return
	}
	if req.Address == "" {
		writeError(w, http.StatusBadRequest, "MISSING_FIELD", "backend address is required")
		return
	}

	// Validate address format (host:port)
	if _, err := net.ResolveTCPAddr("tcp", req.Address); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ADDRESS", "backend address must be in host:port format")
		return
	}

	// Check if backend already exists
	if existing := pool.GetBackend(req.ID); existing != nil {
		writeError(w, http.StatusConflict, "ALREADY_EXISTS", "backend already exists: "+req.ID)
		return
	}

	// Raft mode: propose the backend addition through consensus
	if s.raftProposer != nil {
		backendJSON, _ := json.Marshal(map[string]any{
			"id":      req.ID,
			"address": req.Address,
			"weight":  req.Weight,
		})
		if err := s.raftProposer.ProposeUpdateBackend(poolName, backendJSON); err != nil {
			writeError(w, http.StatusConflict, "RAFT_ERROR",
				"failed to propose backend addition: "+err.Error())
			return
		}
		writeSuccess(w, map[string]string{
			"status":  "proposed",
			"pool":    poolName,
			"backend": req.ID,
		})
		return
	}

	// Create backend (standalone mode)
	b := backend.NewBackend(req.ID, req.Address)
	if req.Weight > 0 {
		b.Weight = int32(req.Weight)
	}

	if err := pool.AddBackend(b); err != nil {
		if errors.Is(err, errors.ErrAlreadyExist) {
			writeError(w, http.StatusConflict, "ALREADY_EXISTS", "backend already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
		return
	}

	writeSuccess(w, backendToInfo(b))
}

// removeBackend handles DELETE /api/v1/backends/:pool/:backend
func (s *Server) removeBackend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only DELETE is allowed")
		return
	}

	poolName, backendID := extractBackendID(r.URL.Path)
	if poolName == "" || backendID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "pool and backend IDs are required")
		return
	}

	if s.poolManager == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found")
		return
	}

	pool := s.poolManager.GetPool(poolName)
	if pool == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found: "+poolName)
		return
	}

	// Raft mode: propose the backend removal through consensus
	if s.raftProposer != nil {
		if err := s.raftProposer.ProposeDeleteBackend(poolName, backendID); err != nil {
			writeError(w, http.StatusConflict, "RAFT_ERROR",
				"failed to propose backend removal: "+err.Error())
			return
		}
		writeSuccess(w, map[string]string{
			"status":  "proposed",
			"pool":    poolName,
			"backend": backendID,
		})
		return
	}

	// Standalone mode: direct removal
	if err := pool.RemoveBackend(backendID); err != nil {
		if errors.Is(err, errors.ErrBackendNotFound) {
			writeError(w, http.StatusNotFound, "BACKEND_NOT_FOUND", "backend not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
		return
	}

	writeSuccess(w, map[string]string{"message": "backend removed successfully"})
}

type UpdateBackendRequest struct {
	Weight   *int32 `json:"weight,omitempty"`
	MaxConns *int32 `json:"max_conns,omitempty"`
}

// updateBackend handles PATCH /api/v1/backends/:pool/:backend
func (s *Server) updateBackend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only PATCH is allowed")
		return
	}

	poolName, backendID := extractBackendID(r.URL.Path)
	if poolName == "" || backendID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "pool and backend IDs are required")
		return
	}

	if s.poolManager == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found")
		return
	}

	pool := s.poolManager.GetPool(poolName)
	if pool == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found: "+poolName)
		return
	}

	b := pool.GetBackend(backendID)
	if b == nil {
		writeError(w, http.StatusNotFound, "BACKEND_NOT_FOUND", "backend not found: "+backendID)
		return
	}

	var req UpdateBackendRequest
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON payload")
		return
	}

	if req.Weight != nil {
		if *req.Weight < 0 || *req.Weight > 1000 {
			writeError(w, http.StatusBadRequest, "INVALID_WEIGHT", "weight must be between 0 and 1000")
			return
		}
	}

	// Raft mode: propose the backend update through consensus
	if s.raftProposer != nil {
		backendJSON, _ := json.Marshal(map[string]any{
			"id":      backendID,
			"address": b.Address,
			"weight":  b.Weight,
		})
		if req.Weight != nil {
			backendJSON, _ = json.Marshal(map[string]any{
				"id":      backendID,
				"address": b.Address,
				"weight":  *req.Weight,
			})
		}
		if err := s.raftProposer.ProposeUpdateBackend(poolName, backendJSON); err != nil {
			writeError(w, http.StatusConflict, "RAFT_ERROR",
				"failed to propose backend update: "+err.Error())
			return
		}
		writeSuccess(w, map[string]string{
			"status":  "proposed",
			"pool":    poolName,
			"backend": backendID,
		})
		return
	}

	// Standalone mode: direct update
	if req.Weight != nil {
		b.Weight = *req.Weight
	}
	if req.MaxConns != nil {
		if *req.MaxConns < 0 {
			writeError(w, http.StatusBadRequest, "INVALID_MAX_CONNS", "max connections must be non-negative")
			return
		}
		b.MaxConns = *req.MaxConns
	}

	writeSuccess(w, backendToInfo(b))
}

// drainBackend handles POST /api/v1/backends/:pool/:backend/drain
func (s *Server) drainBackend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is allowed")
		return
	}

	// Extract pool and backend from path like /api/v1/backends/:pool/:backend/drain
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 6 {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "pool and backend IDs are required")
		return
	}

	poolName := parts[3]
	backendID := parts[4]

	if s.poolManager == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found")
		return
	}

	pool := s.poolManager.GetPool(poolName)
	if pool == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found: "+poolName)
		return
	}

	if err := pool.DrainBackend(backendID); err != nil {
		if errors.Is(err, errors.ErrBackendNotFound) {
			writeError(w, http.StatusNotFound, "BACKEND_NOT_FOUND", "backend not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
		return
	}

	writeSuccess(w, map[string]string{"message": "backend drained successfully"})
}

// getBackendDetail handles GET /api/v1/backends/:pool/:backend
func (s *Server) getBackendDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	poolName, backendID := extractBackendID(r.URL.Path)
	if poolName == "" || backendID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "pool and backend IDs are required")
		return
	}

	if s.poolManager == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found")
		return
	}

	pool := s.poolManager.GetPool(poolName)
	if pool == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found: "+poolName)
		return
	}

	b := pool.GetBackend(backendID)
	if b == nil {
		writeError(w, http.StatusNotFound, "BACKEND_NOT_FOUND", "backend not found: "+backendID)
		return
	}

	writeSuccess(w, backendToInfo(b))
}

// listRoutes handles GET /api/v1/routes
func (s *Server) listRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	if s.router == nil {
		writeSuccess(w, []Route{})
		return
	}

	routes := s.router.Routes()
	response := make([]Route, 0, len(routes))
	for _, route := range routes {
		response = append(response, Route{
			Name:        route.Name,
			Host:        route.Host,
			Path:        route.Path,
			Methods:     route.Methods,
			Headers:     route.Headers,
			BackendPool: route.BackendPool,
			Priority:    route.Priority,
		})
	}

	writeSuccess(w, response)
}

// getHealthStatus handles GET /api/v1/health
func (s *Server) getHealthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	if s.healthChecker == nil {
		writeSuccess(w, []HealthStatusInfo{})
		return
	}

	statuses := s.healthChecker.ListStatuses()
	response := make([]HealthStatusInfo, 0, len(statuses))

	for backendID, status := range statuses {
		hcs := HealthStatusInfo{
			BackendID: backendID,
			Status:    status.String(),
		}

		// Get last result if available
		if result := s.healthChecker.GetResult(backendID); result != nil {
			hcs.LastCheck = result.Timestamp
			if result.Latency > 0 {
				hcs.Latency = result.Latency.String()
			}
			if result.Error != nil {
				hcs.Error = "unhealthy"
			}
		}

		response = append(response, hcs)
	}

	writeSuccess(w, response)
}

// getMetricsJSON handles GET /api/v1/metrics
func (s *Server) getMetricsJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	if s.metrics == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_AVAILABLE", "metrics not available")
		return
	}

	metrics := s.metrics.GetAllMetrics()
	writeSuccess(w, metrics)
}

// getMetricsPrometheus handles GET /metrics
func (s *Server) getMetricsPrometheus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	if s.metrics == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_AVAILABLE", "metrics not available")
		return
	}

	prometheusOutput := s.metrics.PrometheusFormat()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.Write([]byte(prometheusOutput))
}

// getConfig handles GET /api/v1/config
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getConfig(w, r)
	case http.MethodPut:
		s.updateConfig(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED",
			"only GET and PUT are allowed")
	}
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	if s.configGetter == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_AVAILABLE", "config provider not available")
		return
	}

	cfg := s.configGetter.GetConfig()
	writeSuccess(w, cfg)
}

func (s *Server) updateConfig(w http.ResponseWriter, r *http.Request) {
	if s.raftProposer != nil {
		// Clustered mode: propose through Raft
		body, err := readBody(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
			return
		}
		if err := s.raftProposer.ProposeSetConfig(body); err != nil {
			writeError(w, http.StatusConflict, "RAFT_ERROR",
				"failed to propose config change: "+err.Error())
			return
		}
		writeSuccess(w, map[string]string{"status": "proposed"})
		return
	}

	// Standalone mode: trigger a reload from disk
	if s.onReload == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_AVAILABLE",
			"no reload handler available")
		return
	}
	if err := s.onReload(); err != nil {
		writeError(w, http.StatusInternalServerError, "RELOAD_ERROR", err.Error())
		return
	}
	writeSuccess(w, map[string]string{"status": "reloaded"})
}

// getCertificates handles GET /api/v1/certificates
func (s *Server) getCertificates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	if s.certLister == nil {
		writeSuccess(w, []CertInfoView{})
		return
	}

	certs := s.certLister.ListCertificates()
	writeSuccess(w, certs)
}

// getWAFStatus handles GET /api/v1/waf/status
func (s *Server) getWAFStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	if s.wafStatus == nil {
		writeSuccess(w, map[string]any{"enabled": false})
		return
	}

	writeSuccess(w, s.wafStatus())
}

// getMiddlewareStatus handles GET /api/v1/middleware/status
func (s *Server) getMiddlewareStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	if s.middlewareStatus == nil {
		writeSuccess(w, []MiddlewareStatusItem{})
		return
	}

	writeSuccess(w, s.middlewareStatus())
}

// getEvents handles GET /api/v1/events
func (s *Server) getEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	events := make([]EventItem, 0)

	// Collect backend health events
	if s.healthChecker != nil && s.poolManager != nil {
		pools := s.poolManager.GetAllPools()
		for _, pool := range pools {
			backends := pool.GetAllBackends()
			healthy := 0
			for _, b := range backends {
				if b.IsHealthy() {
					healthy++
				}
				// Check individual backend health result
				result := s.healthChecker.GetResult(b.ID)
				if result != nil {
					eventType := "success"
					msg := "healthy"
					if !result.Healthy {
						eventType = "warning"
						if result.Error != nil {
							msg = result.Error.Error()
						} else {
							msg = "unhealthy"
						}
					}
					events = append(events, EventItem{
						ID:        "health-" + b.ID,
						Type:      eventType,
						Message:   "Backend " + b.ID + " (" + b.Address + ") is " + msg,
						Timestamp: result.Timestamp.Format(time.RFC3339),
					})
				}
			}

			// Pool summary
			if len(backends) > 0 && healthy < len(backends) {
				events = append(events, EventItem{
					ID:        "pool-" + pool.Name,
					Type:      "warning",
					Message:   fmt.Sprintf("Pool %s: %d/%d backends healthy", pool.Name, healthy, len(backends)),
					Timestamp: time.Now().Format(time.RFC3339),
				})
			}
		}
	}

	// Add system uptime event
	events = append(events, EventItem{
		ID:        "system-start",
		Type:      "info",
		Message:   "System started",
		Timestamp: s.startTime.Format(time.RFC3339),
	})

	writeSuccess(w, events)
}

// Helper functions

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
