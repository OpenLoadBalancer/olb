package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/pkg/version"
)

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
	s.mu.RLock()
	hc := s.healthChecker
	s.mu.RUnlock()
	if hc != nil {
		statuses := hc.ListStatuses()
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

	healthResp := HealthStatus{
		Status:    status,
		Checks:    checks,
		Timestamp: time.Now(),
	}

	writeSuccess(w, healthResp)
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
		writeError(w, http.StatusServiceUnavailable, "RELOAD_FAILED", "configuration reload failed")
		return
	}

	writeSuccess(w, map[string]string{"message": "configuration reloaded successfully"})
}

// handleConfig handles requests to /api/v1/config
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

// getConfig handles GET /api/v1/config
func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	if s.configGetter == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_AVAILABLE", "config provider not available")
		return
	}

	cfg := s.configGetter.GetConfig()
	writeSuccess(w, cfg)
}

// updateConfig handles PUT /api/v1/config
func (s *Server) updateConfig(w http.ResponseWriter, r *http.Request) {
	if s.raftProposer != nil {
		// Clustered mode: propose through Raft
		body, err := readBody(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
			return
		}
		if err := s.raftProposer.ProposeSetConfig(body); err != nil {
			writeError(w, http.StatusConflict, "RAFT_ERROR",
				"failed to propose config change")
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
		writeError(w, http.StatusInternalServerError, "RELOAD_ERROR", "configuration reload failed")
		return
	}
	writeSuccess(w, map[string]string{"status": "reloaded"})
}
