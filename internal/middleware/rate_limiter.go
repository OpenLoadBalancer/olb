// Package middleware provides HTTP middleware infrastructure for OpenLoadBalancer.
package middleware

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimitConfig configures the rate limiter middleware.
type RateLimitConfig struct {
	// RequestsPerSecond is the rate limit (e.g., 10.0)
	RequestsPerSecond float64
	// BurstSize is the maximum burst size (e.g., 20)
	BurstSize int
	// KeyFunc generates the rate limit key from the request (default: ClientIP)
	KeyFunc func(r *http.Request) string
	// CleanupInterval is the bucket cleanup interval (default: 1m)
	CleanupInterval time.Duration
	// CleanupTimeout is the bucket expiry time (default: 10m)
	CleanupTimeout time.Duration
}

// RateLimitMiddleware implements token bucket rate limiting per key.
type RateLimitMiddleware struct {
	config  RateLimitConfig
	buckets sync.Map // map[string]*tokenBucket
	stopCh  chan struct{}
}

// tokenBucket represents a single token bucket for rate limiting.
type tokenBucket struct {
	tokens    float64
	lastCheck time.Time
	mu        sync.Mutex
}

// defaultKeyFunc extracts the real client IP from the request.
// It checks X-Forwarded-For and X-Real-IP headers before falling
// back to RemoteAddr, so rate limiting works correctly behind proxies.
func defaultKeyFunc(r *http.Request) string {
	// Check X-Forwarded-For (first IP in chain is the original client)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first (leftmost) IP — the original client
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fallback to direct connection address
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// NewRateLimitMiddleware creates a new rate limiter middleware.
func NewRateLimitMiddleware(config RateLimitConfig) (*RateLimitMiddleware, error) {
	// Set defaults
	if config.KeyFunc == nil {
		config.KeyFunc = defaultKeyFunc
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = time.Minute
	}
	if config.CleanupTimeout <= 0 {
		config.CleanupTimeout = 10 * time.Minute
	}

	m := &RateLimitMiddleware{
		config: config,
		stopCh: make(chan struct{}),
	}

	// Start cleanup goroutine
	go m.cleanupLoop()

	return m, nil
}

// Name returns the middleware name.
func (m *RateLimitMiddleware) Name() string {
	return "rate-limiter"
}

// Priority returns the middleware priority.
func (m *RateLimitMiddleware) Priority() int {
	return PriorityRateLimit
}

// Wrap wraps the next handler with rate limiting.
func (m *RateLimitMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := m.config.KeyFunc(r)

		allowed, retryAfter := m.allow(key)

		// Get bucket for header calculations
		bucketIface, _ := m.buckets.Load(key)
		bucket, _ := bucketIface.(*tokenBucket)

		// Set rate limit headers
		w.Header().Set("X-RateLimit-Limit", strconv.FormatFloat(m.config.RequestsPerSecond, 'f', -1, 64))

		if bucket != nil {
			bucket.mu.Lock()
			remaining := int(math.Floor(bucket.tokens))
			if remaining < 0 {
				remaining = 0
			}
			// Calculate reset time (when bucket will be full)
			tokensNeeded := float64(m.config.BurstSize) - bucket.tokens
			secondsToFill := tokensNeeded / m.config.RequestsPerSecond
			resetTime := time.Now().Add(time.Duration(secondsToFill * float64(time.Second)))
			// Ensure reset time is at least 1 second in the future to avoid "now" values
			minResetTime := time.Now().Add(time.Second)
			if resetTime.Before(minResetTime) {
				resetTime = minResetTime
			}
			bucket.mu.Unlock()

			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))
		} else {
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(m.config.BurstSize))
			// Reset time is when the bucket would be full from empty
			secondsToFill := float64(m.config.BurstSize) / m.config.RequestsPerSecond
			resetTime := time.Now().Add(time.Duration(secondsToFill*float64(time.Second)) + time.Second)
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))
		}

		if !allowed {
			w.Header().Set("Retry-After", strconv.FormatInt(int64(retryAfter.Seconds()), 10))
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Stop stops the cleanup goroutine.
func (m *RateLimitMiddleware) Stop() {
	close(m.stopCh)
}

// allow checks if the request is allowed and returns the retry-after duration.
func (m *RateLimitMiddleware) allow(key string) (bool, time.Duration) {
	now := time.Now()

	// Load or create bucket
	bucketIface, loaded := m.buckets.Load(key)
	if !loaded {
		newBucket := &tokenBucket{
			tokens:    float64(m.config.BurstSize),
			lastCheck: now,
		}
		actualIface, loaded := m.buckets.LoadOrStore(key, newBucket)
		bucketIface = actualIface
		if loaded {
			// Another goroutine created the bucket, use that one
			bucket := bucketIface.(*tokenBucket)
			bucket.mu.Lock()
			defer bucket.mu.Unlock()
			return m.checkAndConsume(bucket, now)
		}
		// We created the bucket, consume one token
		newBucket.tokens--
		return true, 0
	}

	bucket := bucketIface.(*tokenBucket)
	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	return m.checkAndConsume(bucket, now)
}

// checkAndConsume checks if a request can proceed and consumes a token.
// Must be called with bucket.mu held.
func (m *RateLimitMiddleware) checkAndConsume(bucket *tokenBucket, now time.Time) (bool, time.Duration) {
	// Calculate tokens to add based on elapsed time
	elapsed := now.Sub(bucket.lastCheck).Seconds()
	bucket.tokens += elapsed * m.config.RequestsPerSecond
	bucket.lastCheck = now

	// Cap at burst size
	if bucket.tokens > float64(m.config.BurstSize) {
		bucket.tokens = float64(m.config.BurstSize)
	}

	// Check if we can consume a token
	if bucket.tokens >= 1.0 {
		bucket.tokens--
		return true, 0
	}

	// Calculate retry-after (time until 1 token is available)
	tokensNeeded := 1.0 - bucket.tokens
	retryAfter := time.Duration(tokensNeeded / m.config.RequestsPerSecond * float64(time.Second))

	// Ensure retry-after is at least 1 second
	if retryAfter < time.Second {
		retryAfter = time.Second
	}

	return false, retryAfter
}

// cleanupLoop periodically removes stale buckets.
func (m *RateLimitMiddleware) cleanupLoop() {
	ticker := time.NewTicker(m.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.cleanup()
		case <-m.stopCh:
			return
		}
	}
}

// cleanup removes buckets that have been idle longer than CleanupTimeout.
func (m *RateLimitMiddleware) cleanup() {
	cutoff := time.Now().Add(-m.config.CleanupTimeout)

	m.buckets.Range(func(key, value interface{}) bool {
		bucket := value.(*tokenBucket)
		bucket.mu.Lock()
		lastCheck := bucket.lastCheck
		bucket.mu.Unlock()

		if lastCheck.Before(cutoff) {
			m.buckets.Delete(key)
		}
		return true
	})
}
