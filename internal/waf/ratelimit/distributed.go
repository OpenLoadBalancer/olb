package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Store defines the interface for rate limit storage backends.
type Store interface {
	// Allow checks if a key is allowed and updates the rate limit counter.
	// Returns allowed=true if under limit, and remaining tokens.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, remaining int, resetAt time.Time, err error)

	// Increment adds to the counter for a key without checking limit.
	Increment(ctx context.Context, key string, delta int, window time.Duration) error

	// Get retrieves current count and TTL for a key.
	Get(ctx context.Context, key string) (count int64, ttl time.Duration, err error)

	// Close closes the store connection.
	Close() error
}

// DistributedRateLimiter implements rate limiting with a pluggable storage backend.
// It supports Redis, in-memory, and other backends for distributed rate limiting.
type DistributedRateLimiter struct {
	mu     sync.RWMutex
	rules  []Rule
	store  Store
	local  *RateLimiter // fallback local rate limiter
	stopCh chan struct{}

	// Callbacks
	OnAutoBan    func(ip string, reason string)
	OnSyncError  func(error)
	OnStoreError func(key string, err error)

	// Config
	useLocalFallback bool
	syncInterval     time.Duration
}

// DistributedConfig configures the distributed rate limiter.
type DistributedConfig struct {
	Rules            []Rule
	Store            Store
	UseLocalFallback bool          // Use local memory as fallback if store fails
	SyncInterval     time.Duration // How often to sync with store
}

// NewDistributed creates a new distributed rate limiter.
func NewDistributed(cfg DistributedConfig) *DistributedRateLimiter {
	rl := &DistributedRateLimiter{
		rules:            cfg.Rules,
		store:            cfg.Store,
		useLocalFallback: cfg.UseLocalFallback,
		syncInterval:     cfg.SyncInterval,
		stopCh:           make(chan struct{}),
	}

	if cfg.UseLocalFallback {
		rl.local = New(Config{Rules: cfg.Rules})
	}

	return rl
}

// Allow checks if a request is allowed by all rate limit rules.
func (rl *DistributedRateLimiter) Allow(r *http.Request) (allowed bool, retryAfter int) {
	ip := extractIP(r.RemoteAddr)

	rl.mu.RLock()
	rules := rl.rules
	rl.mu.RUnlock()

	for _, rule := range rules {
		if len(rule.Paths) > 0 && !matchAnyPath(r.URL.Path, rule.Paths) {
			continue
		}

		key := rl.buildKey(rule, r, ip)
		allowed, remaining, resetAt, err := rl.checkStore(key, rule)

		if err != nil {
			// Store error - fallback to local if enabled
			if rl.useLocalFallback && rl.local != nil {
				if rl.OnStoreError != nil {
					rl.OnStoreError(key, err)
				}
				return rl.local.Allow(r)
			}
			// Without fallback, allow the request
			continue
		}

		if !allowed {
			rl.recordViolation(key, ip, rule)
			retry := int(time.Until(resetAt).Seconds())
			if retry < 1 {
				retry = 1
			}
			return false, retry
		}

		// Record remaining for monitoring
		_ = remaining
	}

	return true, 0
}

// checkStore checks the rate limit against the store.
func (rl *DistributedRateLimiter) checkStore(key string, rule Rule) (bool, int, time.Time, error) {
	if rl.store == nil {
		return true, rule.Limit, time.Now().Add(rule.Window), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	allowed, remaining, resetAt, err := rl.store.Allow(ctx, key, rule.Limit, rule.Window)
	return allowed, remaining, resetAt, err
}

// AddRule adds a new rate limiting rule.
func (rl *DistributedRateLimiter) AddRule(rule Rule) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.rules = append(rl.rules, rule)
	if rl.local != nil {
		rl.local.AddRule(rule)
	}
}

// RemoveRule removes a rate limiting rule by ID.
func (rl *DistributedRateLimiter) RemoveRule(id string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	for i, rule := range rl.rules {
		if rule.ID == id {
			rl.rules = append(rl.rules[:i], rl.rules[i+1:]...)
			if rl.local != nil {
				rl.local.RemoveRule(id)
			}
			return true
		}
	}
	return false
}

// Stop stops the rate limiter and closes the store.
func (rl *DistributedRateLimiter) Stop() {
	select {
	case <-rl.stopCh:
		// Already closed
		return
	default:
		close(rl.stopCh)
	}
	if rl.local != nil {
		rl.local.Stop()
	}
	if rl.store != nil {
		rl.store.Close()
	}
}

// Stats returns current statistics from the store.
func (rl *DistributedRateLimiter) Stats(ctx context.Context) (map[string]interface{}, error) {
	if rl.store == nil {
		return map[string]interface{}{"store": "none"}, nil
	}
	return map[string]interface{}{"store": "connected"}, nil
}

func (rl *DistributedRateLimiter) buildKey(rule Rule, r *http.Request, ip string) string {
	prefix := "rl:"
	switch {
	case rule.Scope == "global":
		return fmt.Sprintf("%sglobal:%s", prefix, rule.ID)
	case rule.Scope == "ip":
		return fmt.Sprintf("%sip:%s:%s", prefix, rule.ID, ip)
	case rule.Scope == "path":
		return fmt.Sprintf("%spath:%s:%s", prefix, rule.ID, r.URL.Path)
	case rule.Scope == "ip+path":
		return fmt.Sprintf("%sip+path:%s:%s:%s", prefix, rule.ID, ip, r.URL.Path)
	default:
		return fmt.Sprintf("%sip:%s:%s", prefix, rule.ID, ip)
	}
}

func (rl *DistributedRateLimiter) recordViolation(key, ip string, rule Rule) {
	if rule.AutoBanAfter <= 0 || rl.OnAutoBan == nil {
		return
	}

	// For distributed mode, we'd need to track violations in the store
	// For now, use local tracking
	rl.mu.Lock()
	// Simplified - in production, use store for distributed violation tracking
	rl.mu.Unlock()

	// Trigger auto-ban
	rl.OnAutoBan(ip, "rate limit exceeded: "+rule.ID)
}
