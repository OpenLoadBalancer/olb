package admin

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

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

	s.mu.RLock()
	hc := s.healthChecker
	s.mu.RUnlock()
	if hc == nil {
		writeSuccess(w, []HealthStatusInfo{})
		return
	}

	statuses := hc.ListStatuses()
	response := make([]HealthStatusInfo, 0, len(statuses))

	for backendID, status := range statuses {
		hcs := HealthStatusInfo{
			BackendID: backendID,
			Status:    status.String(),
		}

		// Get last result if available
		if result := hc.GetResult(backendID); result != nil {
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
	if _, err := w.Write([]byte(prometheusOutput)); err != nil {
		slog.Warn("Failed to write Prometheus metrics response", "error", err)
	}
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
	s.mu.RLock()
	hc := s.healthChecker
	s.mu.RUnlock()
	if hc != nil && s.poolManager != nil {
		pools := s.poolManager.GetAllPools()
		for _, pool := range pools {
			backends := pool.GetAllBackends()
			healthy := 0
			for _, b := range backends {
				if b.IsHealthy() {
					healthy++
				}
				// Check individual backend health result
				result := hc.GetResult(b.ID)
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
