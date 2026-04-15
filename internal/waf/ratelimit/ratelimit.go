package ratelimit

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config configures the rate limiter.
type Config struct {
	Rules        []Rule
	SyncInterval time.Duration // for distributed mode
}

// Rule defines a rate limiting rule.
type Rule struct {
	ID           string
	Scope        string   // "ip", "path", "ip+path", "header:X-API-Key", "global"
	Paths        []string // glob patterns
	Limit        int      // requests per window
	Window       time.Duration
	Burst        int    // max burst above limit
	Action       string // "block", "throttle"
	AutoBanAfter int    // violations before auto-ban (0 = disabled)
}

// RateLimiter implements multi-rule rate limiting with per-key token buckets.
type RateLimiter struct {
	mu         sync.RWMutex
	rules      []Rule
	buckets    map[string]*TokenBucket // key → bucket
	violations map[string]int          // key → violation count
	stopCh     chan struct{}

	// Callback for auto-ban
	OnAutoBan func(ip string, reason string)
}

// New creates a new RateLimiter.
func New(cfg Config) *RateLimiter {
	rl := &RateLimiter{
		rules:      cfg.Rules,
		buckets:    make(map[string]*TokenBucket),
		violations: make(map[string]int),
		stopCh:     make(chan struct{}),
	}

	// Start cleanup goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[ratelimit] panic recovered in cleanup: %v", r)
			}
		}()
		rl.cleanupLoop()
	}()

	return rl
}

// Allow checks if a request is allowed by all rate limit rules.
// Returns true if allowed, false if rate limited.
// When blocked, retryAfter indicates seconds until the client can retry.
func (rl *RateLimiter) Allow(r *http.Request) (allowed bool, retryAfter int) {
	ip := extractIP(r.RemoteAddr)

	rl.mu.RLock()
	rules := rl.rules
	rl.mu.RUnlock()

	for _, rule := range rules {
		// Check if rule applies to this path
		if len(rule.Paths) > 0 && !matchAnyPath(r.URL.Path, rule.Paths) {
			continue
		}

		key := rl.buildKey(rule, r, ip)
		bucket := rl.getOrCreateBucket(key, rule)

		if !bucket.Allow() {
			// Rate limited
			rl.recordViolation(key, ip, rule)
			retry := int(rule.Window.Seconds())
			if retry < 1 {
				retry = 1
			}
			return false, retry
		}
	}

	return true, 0
}

// WriteRateLimitResponse writes a 429 response with Retry-After header.
func WriteRateLimitResponse(w http.ResponseWriter, retryAfter int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded", "layer": "rate_limit"})
}

// AddRule adds a new rate limiting rule.
func (rl *RateLimiter) AddRule(rule Rule) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.rules = append(rl.rules, rule)
}

// RemoveRule removes a rate limiting rule by ID.
func (rl *RateLimiter) RemoveRule(id string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for i, rule := range rl.rules {
		if rule.ID == id {
			rl.rules = append(rl.rules[:i], rl.rules[i+1:]...)
			return true
		}
	}
	return false
}

// Stop stops the cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stopCh)
}

func (rl *RateLimiter) buildKey(rule Rule, r *http.Request, ip string) string {
	switch {
	case rule.Scope == "global":
		return "global:" + rule.ID
	case rule.Scope == "ip":
		return "ip:" + rule.ID + ":" + ip
	case rule.Scope == "path":
		return "path:" + rule.ID + ":" + r.URL.Path
	case rule.Scope == "ip+path":
		return "ip+path:" + rule.ID + ":" + ip + ":" + r.URL.Path
	case strings.HasPrefix(rule.Scope, "header:"):
		headerName := rule.Scope[7:]
		headerVal := r.Header.Get(headerName)
		return "header:" + rule.ID + ":" + headerVal
	default:
		return "ip:" + rule.ID + ":" + ip
	}
}

func (rl *RateLimiter) getOrCreateBucket(key string, rule Rule) *TokenBucket {
	rl.mu.RLock()
	bucket, ok := rl.buckets[key]
	rl.mu.RUnlock()

	if ok {
		return bucket
	}

	// Calculate refill rate: limit tokens per window
	refillRate := float64(rule.Limit) / rule.Window.Seconds()
	maxTokens := float64(rule.Limit)
	if rule.Burst > 0 {
		maxTokens = float64(rule.Limit + rule.Burst)
	}

	bucket = NewTokenBucket(maxTokens, refillRate)

	rl.mu.Lock()
	// Double-check after acquiring write lock
	if existing, ok := rl.buckets[key]; ok {
		rl.mu.Unlock()
		return existing
	}
	rl.buckets[key] = bucket
	rl.mu.Unlock()

	return bucket
}

func (rl *RateLimiter) recordViolation(key, ip string, rule Rule) {
	if rule.AutoBanAfter <= 0 || rl.OnAutoBan == nil {
		return
	}

	rl.mu.Lock()
	rl.violations[key]++
	count := rl.violations[key]
	rl.mu.Unlock()

	if count >= rule.AutoBanAfter {
		rl.OnAutoBan(ip, "rate limit exceeded: "+rule.ID)
		// Reset violation count
		rl.mu.Lock()
		delete(rl.violations, key)
		rl.mu.Unlock()
	}
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCh:
			return
		}
	}
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Remove buckets that are full (idle) — they'll be recreated on next request
	for key, bucket := range rl.buckets {
		if bucket.Tokens() >= bucket.maxTokens {
			delete(rl.buckets, key)
		}
	}
}

func matchAnyPath(reqPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if matched, _ := path.Match(pattern, reqPath); matched {
			return true
		}
		// Simple prefix match for patterns like "/api/*"
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(reqPath, prefix) {
				return true
			}
		}
	}
	return false
}

func extractIP(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}
