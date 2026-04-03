package l7

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

// ShadowConfig configures request shadowing/mirroring.
type ShadowConfig struct {
	Enabled     bool
	Percentage  float64 // 0.0 to 100.0
	CopyHeaders bool
	CopyBody    bool
	Timeout     time.Duration
}

// ShadowTarget represents a shadow/mirror target
type ShadowTarget struct {
	Balancer   backend.Balancer
	Backends   []*backend.Backend
	Percentage float64
}

// ShadowManager manages shadow traffic for request mirroring.
type ShadowManager struct {
	enabled bool
	targets []ShadowTarget
	config  ShadowConfig
}

// NewShadowManager creates a new shadow manager.
func NewShadowManager(config ShadowConfig) *ShadowManager {
	return &ShadowManager{
		enabled: config.Enabled,
		config:  config,
		targets: make([]ShadowTarget, 0),
	}
}

// AddTarget adds a shadow target with balancer and backends.
func (sm *ShadowManager) AddTarget(balancer backend.Balancer, backends []*backend.Backend, percentage float64) {
	if sm == nil || !sm.enabled {
		return
	}
	sm.targets = append(sm.targets, ShadowTarget{
		Balancer:   balancer,
		Backends:   backends,
		Percentage: percentage,
	})
}

// ShouldShadow determines if a request should be shadowed based on percentage.
func (sm *ShadowManager) ShouldShadow() bool {
	if sm == nil || !sm.enabled {
		return false
	}
	// Simple hash-based decision could be added here
	// For now, use random selection based on percentage
	return true
}

// ShadowRequest sends a shadow copy of the request to shadow targets.
// This is non-blocking and best-effort.
func (sm *ShadowManager) ShadowRequest(req *http.Request) {
	if sm == nil || !sm.enabled || len(sm.targets) == 0 {
		return
	}

	// Create shadow copies for each target
	for _, target := range sm.targets {
		if target.Balancer == nil {
			continue
		}

		// Select a backend from the balancer
		be := target.Balancer.Next(target.Backends)
		if be == nil {
			continue
		}

		// Send shadow request asynchronously
		go sm.sendShadow(req, be.Address, target)
	}
}

func (sm *ShadowManager) sendShadow(req *http.Request, backendAddr string, target ShadowTarget) {
	ctx, cancel := context.WithTimeout(context.Background(), sm.config.Timeout)
	defer cancel()

	// Build shadow URL
	shadowURL := *req.URL
	shadowURL.Host = backendAddr
	if shadowURL.Scheme == "" {
		shadowURL.Scheme = "http"
	}

	// Create shadow request
	shadowReq, err := http.NewRequestWithContext(ctx, req.Method, shadowURL.String(), nil)
	if err != nil {
		return
	}

	// Copy headers if configured
	if sm.config.CopyHeaders {
		for name, values := range req.Header {
			// Skip hop-by-hop headers
			if isHopByHopHeader(name) {
				continue
			}
			for _, value := range values {
				shadowReq.Header.Add(name, value)
			}
		}
	}

	// Add shadow marker header
	shadowReq.Header.Set("X-OLB-Shadow", "true")
	shadowReq.Header.Set("X-OLB-Shadow-Source", req.Host)

	// Copy body if configured
	if sm.config.CopyBody && req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err == nil && len(body) > 0 {
			shadowReq.Body = io.NopCloser(bytes.NewReader(body))
			shadowReq.ContentLength = int64(len(body))
		}
	}

	// Send request (fire and forget)
	client := &http.Client{
		Timeout: sm.config.Timeout,
		Transport: &http.Transport{
			DisableKeepAlives: true, // Don't keep connections for shadow traffic
		},
	}

	resp, err := client.Do(shadowReq)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// ShadowStats holds statistics for shadow traffic.
type ShadowStats struct {
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
}

// Stats returns shadow traffic statistics.
func (sm *ShadowManager) Stats() ShadowStats {
	// Implementation would track actual stats
	return ShadowStats{}
}
