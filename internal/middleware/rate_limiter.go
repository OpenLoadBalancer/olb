// Package middleware provides HTTP middleware infrastructure for OpenLoadBalancer.
package middleware

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	// MaxBuckets limits the number of tracked rate-limit keys (default: 100000)
	MaxBuckets int
	// TrustedProxies is a list of CIDR ranges for trusted proxy servers.
	// When set, X-Forwarded-For and X-Real-IP headers are only trusted if
	// the direct connection (RemoteAddr) originates from a trusted proxy.
	// When empty (default), forwarded headers are ignored and only RemoteAddr
	// is used, preventing header-spoofing bypass attacks.
	TrustedProxies []string
}

// RateLimitMiddleware implements token bucket rate limiting per key.
type RateLimitMiddleware struct {
	config      RateLimitConfig
	buckets     sync.Map // map[string]*tokenBucket
	bucketCount atomic.Int64
	trustedNets []*net.IPNet
	stopCh      chan struct{}
	wg          sync.WaitGroup // tracks cleanup goroutine for clean shutdown
	// Pre-computed header values to avoid per-request allocations.
	limitStr      string
	burstStr      string
	burstSecondsF float64
}

// tokenBucket represents a single token bucket for rate limiting.
type tokenBucket struct {
	tokens    float64
	lastCheck time.Time
	mu        sync.Mutex
}

// keyFunc extracts the client IP to use as the rate-limit key.
// If trusted proxy networks are configured and the direct connection
// originates from one of them, X-Forwarded-For and X-Real-IP headers
// are consulted. Otherwise, only RemoteAddr is used.
func (m *RateLimitMiddleware) keyFunc(r *http.Request) string {
	remoteIP := remoteAddrIP(r.RemoteAddr)

	if m.isTrustedProxy(remoteIP) {
		// Check X-Forwarded-For (first IP in chain is the original client)
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			return strings.TrimSpace(first)
		}

		// Check X-Real-IP
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}

	return remoteIP
}

// remoteAddrIP extracts the host portion from an address in "host:port" form.
func remoteAddrIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// isTrustedProxy reports whether ip belongs to one of the configured trusted
// proxy networks. If no trusted networks are configured it always returns
// false, which means forwarded headers are never trusted.
func (m *RateLimitMiddleware) isTrustedProxy(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, cidr := range m.trustedNets {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// NewRateLimitMiddleware creates a new rate limiter middleware.
func NewRateLimitMiddleware(config RateLimitConfig) (*RateLimitMiddleware, error) {
	// Guard against division by zero in rate calculations.
	if config.RequestsPerSecond <= 0 {
		return nil, fmt.Errorf("rate limiter: RequestsPerSecond must be > 0, got %f", config.RequestsPerSecond)
	}
	if config.BurstSize <= 0 {
		return nil, fmt.Errorf("rate limiter: BurstSize must be > 0, got %d", config.BurstSize)
	}

	// Set defaults
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = time.Minute
	}
	if config.CleanupTimeout <= 0 {
		config.CleanupTimeout = 10 * time.Minute
	}
	if config.MaxBuckets <= 0 {
		config.MaxBuckets = 100000
	}

	// Parse trusted proxy CIDRs
	var trustedNets []*net.IPNet
	for _, cidr := range config.TrustedProxies {
		// Allow bare IPs (treat as /32 or /128)
		if !strings.Contains(cidr, "/") {
			ip := net.ParseIP(cidr)
			if ip == nil {
				return nil, fmt.Errorf("invalid trusted proxy IP: %s", cidr)
			}
			if ipv4 := ip.To4(); ipv4 != nil {
				cidr += "/32"
			} else {
				cidr += "/128"
			}
		}
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy CIDR %q: %w", cidr, err)
		}
		trustedNets = append(trustedNets, ipNet)
	}

	m := &RateLimitMiddleware{
		config:        config,
		trustedNets:   trustedNets,
		stopCh:        make(chan struct{}),
		limitStr:      strconv.FormatFloat(config.RequestsPerSecond, 'f', -1, 64),
		burstStr:      strconv.Itoa(config.BurstSize),
		burstSecondsF: float64(config.BurstSize) / config.RequestsPerSecond,
	}

	// Wire the default key function only when none was provided.
	if config.KeyFunc == nil {
		m.config.KeyFunc = m.keyFunc
	}

	// Start cleanup goroutine
	m.wg.Add(1)
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

		// Set rate limit headers (limitStr is pre-computed to avoid per-request allocation)
		w.Header().Set("X-RateLimit-Limit", m.limitStr)

		if bucket != nil {
			bucket.mu.Lock()
			remaining := int(math.Floor(bucket.tokens))
			remaining = max(remaining, 0)
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
			w.Header().Set("X-RateLimit-Remaining", m.burstStr)
			// Reset time is when the bucket would be full from empty
			resetTime := time.Now().Add(time.Duration(m.burstSecondsF*float64(time.Second)) + time.Second)
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))
		}

		if !allowed {
			w.Header().Set("Retry-After", strconv.FormatInt(int64(retryAfter.Seconds()), 10))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Stop stops the cleanup goroutine and waits for it to exit.
func (m *RateLimitMiddleware) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

// allow checks if the request is allowed and returns the retry-after duration.
func (m *RateLimitMiddleware) allow(key string) (bool, time.Duration) {
	now := time.Now()

	// Load or create bucket
	bucketIface, loaded := m.buckets.Load(key)
	if !loaded {
		// Check bucket count limit before creating a new one
		maxBuckets := m.config.MaxBuckets
		if maxBuckets <= 0 {
			maxBuckets = 100000
		}
		if m.bucketCount.Load() >= int64(maxBuckets) {
			// Too many unique keys — reject to prevent memory exhaustion
			return false, time.Duration(1e9 / float64(m.config.RequestsPerSecond))
		}

		newBucket := &tokenBucket{
			tokens:    float64(m.config.BurstSize),
			lastCheck: now,
		}
		actualIface, loaded := m.buckets.LoadOrStore(key, newBucket)
		bucketIface = actualIface
		bucket := bucketIface.(*tokenBucket)
		bucket.mu.Lock()
		if loaded {
			// Another goroutine created the bucket, use that one
			ok, retry := m.checkAndConsume(bucket, now)
			bucket.mu.Unlock()
			return ok, retry
		}
		// We created the bucket, track it
		m.bucketCount.Add(1)
		bucket.tokens--
		bucket.mu.Unlock()
		return true, 0
	}

	bucket := bucketIface.(*tokenBucket)
	bucket.mu.Lock()
	ok, retry := m.checkAndConsume(bucket, now)
	bucket.mu.Unlock()
	return ok, retry
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
	retryAfter = max(retryAfter, time.Second)

	return false, retryAfter
}

// cleanupLoop periodically removes stale buckets.
func (m *RateLimitMiddleware) cleanupLoop() {
	defer m.wg.Done()
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

	m.buckets.Range(func(key, value any) bool {
		bucket := value.(*tokenBucket)
		bucket.mu.Lock()
		lastCheck := bucket.lastCheck
		bucket.mu.Unlock()

		if lastCheck.Before(cutoff) {
			m.buckets.Delete(key)
			m.bucketCount.Add(-1)
		}
		return true
	})
}
