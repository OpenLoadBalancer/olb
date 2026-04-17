package l7

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/metrics"
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
	counter atomic.Uint64
	client  *http.Client

	// sem bounds the number of concurrent in-flight shadow requests.
	sem chan struct{}
	wg  sync.WaitGroup

	// Metrics
	requestsTotal *metrics.Counter
	errorsTotal   *metrics.Counter
	droppedTotal  *metrics.Counter
}

const maxConcurrentShadow = 1000

// NewShadowManager creates a new shadow manager.
func NewShadowManager(config ShadowConfig) *ShadowManager {
	transport := &http.Transport{
		DisableKeepAlives:   true, // Don't keep connections for shadow traffic
		MaxIdleConns:        100,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}
	return &ShadowManager{
		enabled: config.Enabled,
		config:  config,
		targets: make([]ShadowTarget, 0),
		sem:     make(chan struct{}, maxConcurrentShadow),
		client: &http.Client{
			Timeout:   config.Timeout,
			Transport: transport,
		},
		requestsTotal: metrics.NewCounter("shadow_requests_total", "Total shadow requests sent"),
		errorsTotal:   metrics.NewCounter("shadow_errors_total", "Total shadow request errors"),
		droppedTotal:  metrics.NewCounter("shadow_dropped_total", "Total shadow requests dropped (semaphore full)"),
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
// Uses a deterministic counter-based approach: every Nth request is shadowed
// based on the configured percentage (e.g., 10% means every 10th request).
func (sm *ShadowManager) ShouldShadow() bool {
	if sm == nil || !sm.enabled || len(sm.targets) == 0 {
		return false
	}
	pct := sm.config.Percentage
	if pct <= 0 {
		return false
	}
	if pct >= 100 {
		return true
	}
	// Deterministic: shadow 1 in every (100/pct) requests
	n := sm.counter.Add(1)
	threshold := uint64(100.0 / pct)
	return n%threshold == 1
}

// Wait blocks until all in-flight shadow requests have completed.
func (sm *ShadowManager) Wait() {
	if sm != nil {
		sm.wg.Wait()
	}
}

// ShouldShadowRequest determines if a specific request should be shadowed
// based solely on the configured percentage.
func (sm *ShadowManager) ShouldShadowRequest(req *http.Request) bool {
	if sm == nil || !sm.enabled {
		return false
	}
	return sm.ShouldShadow()
}

// ShadowRequest sends a shadow copy of the request to shadow targets.
// This is non-blocking and best-effort.
func (sm *ShadowManager) ShadowRequest(req *http.Request) {
	if sm == nil || !sm.enabled || len(sm.targets) == 0 {
		return
	}

	// Buffer the body once before spawning goroutines to avoid a data race
	// where multiple sendShadow goroutines read req.Body concurrently.
	var bodyBuf []byte
	if sm.config.CopyBody && req.Body != nil {
		const maxShadowBodySize = 4 * 1024 * 1024 // 4MB max shadow body
		var err error
		bodyBuf, err = io.ReadAll(io.LimitReader(req.Body, maxShadowBodySize+1))
		if err == nil && len(bodyBuf) > 0 {
			if len(bodyBuf) <= maxShadowBodySize {
				// Restore the original body so the main proxy can still read it
				req.Body = io.NopCloser(bytes.NewReader(bodyBuf))
			} else {
				// Body exceeds max shadow size - restore consumed bytes so
				// the main proxy can still read the full request body.
				req.Body = io.NopCloser(io.MultiReader(bytes.NewReader(bodyBuf), req.Body))
				bodyBuf = nil
			}
		} else {
			bodyBuf = nil
		}
	}

	// Create shadow copies for each target
	for _, target := range sm.targets {
		if target.Balancer == nil {
			continue
		}

		// Select a backend from the balancer
		be := target.Balancer.Next(nil, target.Backends)
		if be == nil {
			continue
		}

		// Send shadow request asynchronously (bounded by semaphore)
		sm.wg.Add(1)
		go func(req *http.Request, addr string, t ShadowTarget, body []byte) {
			defer sm.wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[shadow] panic recovered: %v", r)
				}
			}()
			select {
			case sm.sem <- struct{}{}:
				sm.sendShadow(req, addr, t, body)
				<-sm.sem
			default:
				// Semaphore full - drop shadow request
				sm.droppedTotal.Inc()
			}
		}(req, be.Address, target, bodyBuf)
	}
}

func (sm *ShadowManager) sendShadow(req *http.Request, backendAddr string, target ShadowTarget, bodyBuf []byte) {
	ctx, cancel := context.WithTimeout(req.Context(), sm.config.Timeout)
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

	// Attach pre-buffered body (already read by ShadowRequest to avoid races)
	if bodyBuf != nil {
		shadowReq.Body = io.NopCloser(bytes.NewReader(bodyBuf))
		shadowReq.ContentLength = int64(len(bodyBuf))
	}

	// Send request (fire and forget)
	sm.requestsTotal.Inc()
	resp, err := sm.client.Do(shadowReq)
	if err != nil {
		sm.errorsTotal.Inc()
		return
	}
	io.Copy(io.Discard, resp.Body)
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
