// Package realip extracts the real client IP from proxy headers.
package realip

import (
	"context"
	"net"
	"net/http"
	"strings"
)

// Config configures RealIP middleware.
type Config struct {
	Enabled         bool     // Enable RealIP extraction
	Headers         []string // Headers to check (in order of preference)
	TrustedProxies  []string // CIDR ranges of trusted proxies
	RejectUntrusted bool     // Reject requests from untrusted proxies
	DefaultTrusted  []string // Default trusted private ranges
}

// DefaultConfig returns default RealIP configuration.
func DefaultConfig() Config {
	return Config{
		Enabled: false,
		Headers: []string{
			"CF-Connecting-IP",         // Cloudflare
			"X-Real-IP",                // Nginx, Apache
			"X-Forwarded-For",          // Standard proxy header
			"X-Original-Forwarded-For", // AWS ALB
			"True-Client-IP",           // Akamai, Cloudflare (enterprise)
			"X-Client-IP",              // Azure
			"X-Forwarded",              // Alternative
			"Forwarded-For",            // RFC 7239 (rare)
		},
		TrustedProxies:  []string{},
		RejectUntrusted: false,
		DefaultTrusted: []string{
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"127.0.0.0/8",
			"fc00::/7", // IPv6 private
			"::1/128",  // IPv6 loopback
		},
	}
}

// Middleware extracts real client IP from proxy headers.
type Middleware struct {
	config      Config
	trustedNets []*net.IPNet
}

// New creates a new RealIP middleware.
func New(config Config) *Middleware {
	m := &Middleware{
		config: config,
	}

	// Parse trusted proxy CIDRs
	nets := make([]*net.IPNet, 0)

	// Add default trusted ranges if using them
	if len(config.TrustedProxies) == 0 {
		for _, cidr := range config.DefaultTrusted {
			if _, ipnet, err := net.ParseCIDR(cidr); err == nil {
				nets = append(nets, ipnet)
			}
		}
	} else {
		// Add user-specified trusted ranges
		for _, cidr := range config.TrustedProxies {
			if _, ipnet, err := net.ParseCIDR(cidr); err == nil {
				nets = append(nets, ipnet)
			}
		}
	}

	m.trustedNets = nets
	return m
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "realip"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 15 // Very early, after Recovery(1), before ForceSSL(70)
}

// Wrap wraps the handler with RealIP extraction.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	if !m.config.Enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if request comes from trusted proxy
		if !m.isTrustedProxy(r.RemoteAddr) && m.config.RejectUntrusted {
			http.Error(w, "Untrusted proxy", http.StatusForbidden)
			return
		}

		// Extract real IP from headers
		realIP := m.extractRealIP(r)
		if realIP != "" {
			// Store original RemoteAddr
			r = r.WithContext(contextWithOriginalIP(r.Context(), r.RemoteAddr))
			// Update RemoteAddr (preserve port if possible)
			if _, port, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				r.RemoteAddr = net.JoinHostPort(realIP, port)
			} else {
				r.RemoteAddr = realIP
			}
			// Also set X-Real-IP header for downstream
			r.Header.Set("X-Real-IP", realIP)
		}

		next.ServeHTTP(w, r)
	})
}

// isTrustedProxy checks if the remote address is from a trusted proxy.
func (m *Middleware) isTrustedProxy(remoteAddr string) bool {
	// If no trusted proxies specified, trust all (use default behavior)
	if len(m.config.TrustedProxies) == 0 && !m.config.RejectUntrusted {
		return true
	}

	// Extract IP from RemoteAddr
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr // Might be just IP without port
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	// Check against trusted networks
	for _, ipnet := range m.trustedNets {
		if ipnet.Contains(ip) {
			return true
		}
	}

	return false
}

// extractRealIP extracts the real client IP from request headers.
func (m *Middleware) extractRealIP(r *http.Request) string {
	for _, header := range m.config.Headers {
		value := r.Header.Get(header)
		if value == "" {
			continue
		}

		switch header {
		case "X-Forwarded-For", "X-Original-Forwarded-For":
			// X-Forwarded-For: client, proxy1, proxy2
			// Return the leftmost (original client) IP
			ips := strings.Split(value, ",")
			for _, ip := range ips {
				ip = strings.TrimSpace(ip)
				if net.ParseIP(ip) != nil {
					return ip
				}
			}
		case "Forwarded":
			// RFC 7239: Forwarded: for=192.0.2.60;proto=http;by=203.0.113.43
			ip := parseForwardedHeader(value)
			if ip != "" {
				return ip
			}
		default:
			// Single IP headers
			ip := strings.TrimSpace(value)
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	return ""
}

// parseForwardedHeader parses RFC 7239 Forwarded header.
func parseForwardedHeader(value string) string {
	// Format: Forwarded: for=192.0.2.60;proto=http;by=203.0.113.43
	// Also: Forwarded: for="[2001:db8:cafe::17]:4711"
	parts := strings.Split(value, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "for=") {
			ip := strings.TrimPrefix(part, "for=")
			ip = strings.Trim(ip, "\"") // Remove quotes
			// Handle bracket notation for IPv6 with port: [::1]:4711
			if strings.HasPrefix(ip, "[") {
				if idx := strings.LastIndex(ip, "]"); idx != -1 {
					ip = ip[1:idx] // Extract IP from brackets
				}
			}
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	return ""
}

// context key for original IP
type originalIPKey struct{}

// contextWithOriginalIP stores the original RemoteAddr in context.
func contextWithOriginalIP(ctx context.Context, originalIP string) context.Context {
	return context.WithValue(ctx, originalIPKey{}, originalIP)
}

// GetOriginalIP retrieves the original RemoteAddr from context.
func GetOriginalIP(ctx context.Context) string {
	if ip, ok := ctx.Value(originalIPKey{}).(string); ok {
		return ip
	}
	return ""
}
