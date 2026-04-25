package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/openloadbalancer/olb/internal/health"
	"github.com/openloadbalancer/olb/pkg/version"
)

var (
	lastReloadMu      sync.Mutex
	lastReloadTime    time.Time
	minReloadInterval = 30 * time.Second
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

	// Enforce reload cooldown to prevent reload storms
	lastReloadMu.Lock()
	if time.Since(lastReloadTime) < minReloadInterval {
		lastReloadMu.Unlock()
		writeError(w, http.StatusTooManyRequests, "RELOAD_COOLDOWN",
			fmt.Sprintf("reload cooldown: please wait %v between reloads", minReloadInterval))
		return
	}
	lastReloadTime = time.Now()
	lastReloadMu.Unlock()

	err := s.circuitBreaker.Execute(r.Context(), func(ctx context.Context) error {
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
	sanitized, err := sanitizeConfigForAPI(cfg)
	if err != nil {
		// If sanitization fails (unexpected), fall back to original.
		// The json:"-" tags still provide a baseline level of protection.
		writeSuccess(w, cfg)
		return
	}
	writeSuccess(w, sanitized)
}

// secretJSONKeys lists well-known JSON key names that must never appear in
// the config API response. This provides defense-in-depth: even if a
// developer forgets to add json:"-" to a secret field, it will be stripped
// here. The keys match the json tag names used in config structs.
var secretJSONKeys = map[string]bool{
	// Admin credentials
	"password":     true,
	"bearer_token": true,
	"mcp_token":    true,
	"username":     true, // admin username is also sensitive
	// Auth secrets
	"secret":        true,
	"client_secret": true,
	"shared_secret": true,
	"token":         true,
	// Key material (map values contain credentials)
	"users": true, // BasicAuth users map (username -> hashed password)
	"keys":  true, // APIKey keys map (key_id -> api_key)
}

// sanitizeConfigForAPI serializes the config to JSON and then strips any
// well-known secret fields. This is a defense-in-depth measure: the primary
// protection is json:"-" tags on config struct fields, but this prevents
// accidental leakage if a tag is forgotten.
func sanitizeConfigForAPI(cfg any) (any, error) {
	// Serialize to JSON first (respects json:"-" tags)
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}

	// Deserialize into a generic map for recursive stripping
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}

	stripSecrets(generic)
	return generic, nil
}

// stripSecrets recursively walks a JSON-derived value and removes keys that
// match known secret field names.
func stripSecrets(v any) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for key, val := range m {
		if secretJSONKeys[key] {
			delete(m, key)
			continue
		}
		stripSecrets(val)
		// Also recurse into arrays of objects
		if arr, ok := val.([]any); ok {
			for _, elem := range arr {
				stripSecrets(elem)
			}
		}
	}
}

// updateConfig handles PUT /api/v1/config
func (s *Server) updateConfig(w http.ResponseWriter, r *http.Request) {
	if s.raftProposer != nil {
		// Clustered mode: validate config locally before proposing through Raft
		body, err := readBody(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
			return
		}
		// Validate the proposed config before proposing to Raft
		if s.configValidator != nil {
			if valErr := s.configValidator(body); valErr != nil {
				writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", valErr.Error())
				return
			}
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

	// Enforce reload cooldown to prevent reload storms
	lastReloadMu.Lock()
	if time.Since(lastReloadTime) < minReloadInterval {
		lastReloadMu.Unlock()
		writeError(w, http.StatusTooManyRequests, "RELOAD_COOLDOWN",
			fmt.Sprintf("reload cooldown: please wait %v between reloads", minReloadInterval))
		return
	}
	lastReloadTime = time.Now()
	lastReloadMu.Unlock()

	err := s.circuitBreaker.Execute(r.Context(), func(ctx context.Context) error {
		return s.onReload()
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "RELOAD_FAILED", "configuration reload failed")
		return
	}
	writeSuccess(w, map[string]string{"status": "reloaded"})
}

// rotateTokenRequest is the request body for token rotation.
type rotateTokenRequest struct {
	OldToken string `json:"old_token"`
	NewToken string `json:"new_token"`
}

// rotateToken handles POST /api/v1/auth/rotate-token.
// It allows operators to rotate bearer tokens without restarting the server.
// Requires admin authentication.
func (s *Server) rotateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is allowed")
		return
	}

	if s.config == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_AVAILABLE", "authentication not configured")
		return
	}

	var req rotateTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}

	if err := s.config.RotateBearerToken(req.OldToken, req.NewToken); err != nil {
		writeError(w, http.StatusBadRequest, "ROTATION_FAILED", err.Error())
		return
	}

	writeSuccess(w, map[string]string{"message": "bearer token rotated successfully"})
}
