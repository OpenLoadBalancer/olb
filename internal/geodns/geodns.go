// Package geodns provides Geo-location based DNS routing for OpenLoadBalancer.
package geodns

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Location represents a geographic location.
type Location struct {
	Country   string // ISO 3166-1 alpha-2
	Region    string // State/Province code
	City      string
	Latitude  float64
	Longitude float64
}

// GeoRule defines routing rules based on geographic location.
type GeoRule struct {
	ID       string
	Country  string            // "US", "EU", "*" for any
	Region   string            // State/region code
	Pool     string            // Target backend pool
	Fallback string            // Fallback pool if this one is down
	Weight   int               // Weight for this route
	Headers  map[string]string // Headers to add for this region
}

// GeoDNS provides geographic routing capabilities.
type GeoDNS struct {
	mu          sync.RWMutex
	rules       []GeoRule
	defaultPool string

	// GeoIP database
	geoDB    map[string]*Location // legacy CIDR -> Location
	mmdb     *mmdbReader          // MaxMind DB reader (nil if not loaded)
	mmdbPath string               // path for hot-reload

	// Pool health status
	poolHealth map[string]bool
}

// Config configures GeoDNS.
type Config struct {
	Enabled     bool
	DefaultPool string
	Rules       []GeoRule
	DBPath      string // Path to MaxMind GeoLite2-Country MMDB file
}

// New creates a new GeoDNS router.
func New(cfg Config) *GeoDNS {
	g := &GeoDNS{
		rules:       cfg.Rules,
		defaultPool: cfg.DefaultPool,
		geoDB:       make(map[string]*Location),
		poolHealth:  make(map[string]bool),
		mmdbPath:    cfg.DBPath,
	}

	// Load built-in geo data (fallback)
	g.loadDefaultGeoData()

	// Load MMDB if configured
	if cfg.DBPath != "" {
		reader, err := newMMDBReader(cfg.DBPath)
		if err == nil {
			g.mmdb = reader
		}
		// Soft-fail: if MMDB can't be loaded, use legacy heuristics
	}

	return g
}

// Route determines the appropriate backend pool for a request based on client location.
func (g *GeoDNS) Route(r *http.Request) (pool string, location *Location, err error) {
	if g == nil {
		return "", nil, nil
	}

	// Extract client IP
	clientIP := g.extractClientIP(r)

	// Lookup location
	location = g.lookupLocation(clientIP)

	// Find matching rule
	g.mu.RLock()
	rules := g.rules
	poolHealth := g.poolHealth
	defaultPool := g.defaultPool
	g.mu.RUnlock()

	for _, rule := range rules {
		if g.matchesRule(location, &rule) {
			// Check if pool is healthy
			if healthy, ok := poolHealth[rule.Pool]; !ok || healthy {
				return rule.Pool, location, nil
			}
			// Use fallback
			if rule.Fallback != "" {
				if healthy, ok := poolHealth[rule.Fallback]; !ok || healthy {
					return rule.Fallback, location, nil
				}
			}
		}
	}

	return defaultPool, location, nil
}

// AddRule adds a new GeoDNS rule.
func (g *GeoDNS) AddRule(rule GeoRule) error {
	if rule.ID == "" {
		return errors.New("rule ID is required")
	}
	if rule.Pool == "" {
		return errors.New("rule pool is required")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Check for duplicate ID
	for _, r := range g.rules {
		if r.ID == rule.ID {
			return fmt.Errorf("rule with ID %s already exists", rule.ID)
		}
	}

	g.rules = append(g.rules, rule)
	return nil
}

// RemoveRule removes a GeoDNS rule by ID.
func (g *GeoDNS) RemoveRule(id string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	for i, rule := range g.rules {
		if rule.ID == id {
			g.rules = append(g.rules[:i], g.rules[i+1:]...)
			return true
		}
	}
	return false
}

// SetPoolHealth sets the health status of a pool.
func (g *GeoDNS) SetPoolHealth(pool string, healthy bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.poolHealth[pool] = healthy
}

// AddGeoData adds custom geo-location data for an IP range.
func (g *GeoDNS) AddGeoData(cidr string, loc *Location) error {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid CIDR: %w", err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	g.geoDB[ipNet.String()] = loc
	return nil
}

// Stats returns GeoDNS statistics.
type Stats struct {
	Rules      int
	GeoEntries int
	DBLoaded   bool
	DBPath     string
}

// Stats returns current statistics.
func (g *GeoDNS) Stats() Stats {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return Stats{
		Rules:      len(g.rules),
		GeoEntries: len(g.geoDB),
		DBLoaded:   g.mmdb != nil,
		DBPath:     g.mmdbPath,
	}
}

// Close releases resources held by the GeoDNS instance.
func (g *GeoDNS) Close() {
	g.mu.Lock()
	g.mmdb = nil
	g.mu.Unlock()
}

// ReloadDB reloads the MMDB database from disk.
// Safe to call while lookups are in progress.
func (g *GeoDNS) ReloadDB() error {
	if g.mmdbPath == "" {
		return errors.New("no database path configured")
	}

	reader, err := newMMDBReader(g.mmdbPath)
	if err != nil {
		return fmt.Errorf("failed to reload MMDB: %w", err)
	}

	g.mu.Lock()
	g.mmdb = reader
	g.mu.Unlock()

	return nil
}

// extractClientIP extracts the real client IP from the request.
// Only trusts X-Forwarded-For / X-Real-IP headers when the direct peer
// is a private/loopback address (i.e., a trusted internal proxy).
func (g *GeoDNS) extractClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	// Only trust forwarding headers from private/loopback addresses
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsPrivate() || ip.IsLoopback()) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ips := strings.Split(xff, ",")
			if len(ips) > 0 {
				if clientIP := strings.TrimSpace(ips[0]); clientIP != "" {
					return clientIP
				}
			}
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return xri
		}
	}

	return host
}

// lookupLocation looks up the geographic location for an IP.
func (g *GeoDNS) lookupLocation(ip string) *Location {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return nil
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	// Priority 1: MMDB lookup (O(log n) tree traversal)
	if g.mmdb != nil {
		iso, name, ok := g.mmdb.lookupCountry(parsedIP)
		if ok {
			return &Location{
				Country: iso,
				City:    name,
			}
		}
	}

	// Priority 2: Legacy CIDR scan
	for cidr, loc := range g.geoDB {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ipNet.Contains(parsedIP) {
			return loc
		}
	}

	// Priority 3: Heuristic fallback
	return g.guessLocationFromIP(ip)
}

// matchesRule checks if a location matches a rule.
func (g *GeoDNS) matchesRule(loc *Location, rule *GeoRule) bool {
	if loc == nil {
		return rule.Country == "*"
	}

	// Check country match
	if rule.Country != "" && rule.Country != "*" {
		if strings.ToUpper(loc.Country) != strings.ToUpper(rule.Country) {
			return false
		}
	}

	// Check region match
	if rule.Region != "" {
		if !strings.EqualFold(loc.Region, rule.Region) {
			return false
		}
	}

	return true
}

// guessLocationFromIP attempts to determine location from IP address.
// This is a simplified implementation. In production, use MaxMind GeoIP2.
func (g *GeoDNS) guessLocationFromIP(ip string) *Location {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return nil
	}

	// Simple heuristic based on RFC 1918 and common ranges
	// Private IPs
	if isPrivateIP(parsedIP) {
		return &Location{
			Country:   "PRIVATE",
			Region:    "LOCAL",
			City:      "Private",
			Latitude:  0,
			Longitude: 0,
		}
	}

	// Loopback
	if parsedIP.IsLoopback() {
		return &Location{
			Country:   "LOCAL",
			Region:    "LOOPBACK",
			City:      "Localhost",
			Latitude:  0,
			Longitude: 0,
		}
	}

	// Known IP ranges (simplified)
	ipInt := ipToInt(parsedIP)

	// US ranges (simplified examples)
	if ipInt >= ipToInt(net.ParseIP("3.0.0.0")) && ipInt <= ipToInt(net.ParseIP("3.255.255.255")) {
		return &Location{Country: "US", Region: "", City: "", Latitude: 37.09, Longitude: -95.71}
	}

	// European ranges
	if ipInt >= ipToInt(net.ParseIP("5.0.0.0")) && ipInt <= ipToInt(net.ParseIP("5.255.255.255")) {
		return &Location{Country: "EU", Region: "", City: "", Latitude: 48.85, Longitude: 2.35}
	}

	// Default to unknown
	return &Location{
		Country:   "UNKNOWN",
		Region:    "",
		City:      "",
		Latitude:  0,
		Longitude: 0,
	}
}

// isPrivateIP checks if an IP is private.
func isPrivateIP(ip net.IP) bool {
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
		"127.0.0.0/8",
		"fc00::/7",
	}

	for _, cidr := range privateRanges {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

// ipToInt converts an IP address to an integer.
func ipToInt(ip net.IP) uint32 {
	ip = ip.To4()
	if ip == nil {
		return 0
	}
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

// loadDefaultGeoData loads built-in geographic data.
func (g *GeoDNS) loadDefaultGeoData() {
	// Add some example ranges
	// In production, this would load from MaxMind GeoIP2 or similar
	g.geoDB["8.8.8.0/24"] = &Location{Country: "US", Region: "CA", City: "Mountain View", Latitude: 37.42, Longitude: -122.09}
	g.geoDB["1.1.1.0/24"] = &Location{Country: "AU", Region: "NSW", City: "Sydney", Latitude: -33.87, Longitude: 151.21}
}

// Middleware returns an HTTP middleware that adds geo-location headers.
func (g *GeoDNS) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, loc, _ := g.Route(r)
		if loc != nil {
			r.Header.Set("X-Geo-Country", loc.Country)
			r.Header.Set("X-Geo-Region", loc.Region)
			r.Header.Set("X-Geo-City", loc.City)
			r.Header.Set("X-Geo-Lat", strconv.FormatFloat(loc.Latitude, 'f', -1, 64))
			r.Header.Set("X-Geo-Lon", strconv.FormatFloat(loc.Longitude, 'f', -1, 64))
		}
		next.ServeHTTP(w, r)
	})
}
