package utils

import (
	"fmt"
	"net"
	"sync"
	"testing"
)

func TestCIDRMatcher_Basic(t *testing.T) {
	cm := NewCIDRMatcher()

	// Add CIDR ranges
	if err := cm.Add("10.0.0.0/8"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := cm.Add("192.168.0.0/16"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Should match
	if !cm.Contains("10.1.2.3") {
		t.Error("Should match 10.1.2.3")
	}
	if !cm.Contains("192.168.1.1") {
		t.Error("Should match 192.168.1.1")
	}

	// Should not match
	if cm.Contains("172.16.0.1") {
		t.Error("Should not match 172.16.0.1")
	}
	if cm.Contains("8.8.8.8") {
		t.Error("Should not match 8.8.8.8")
	}
}

func TestCIDRMatcher_IPv6(t *testing.T) {
	cm := NewCIDRMatcher()

	if err := cm.Add("2001:db8::/32"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Should match
	if !cm.Contains("2001:db8::1") {
		t.Error("Should match 2001:db8::1")
	}

	// Should not match
	if cm.Contains("2001:db9::1") {
		t.Error("Should not match 2001:db9::1")
	}
}

func TestCIDRMatcher_InvalidCIDR(t *testing.T) {
	cm := NewCIDRMatcher()

	err := cm.Add("invalid")
	if err == nil {
		t.Error("Should fail for invalid CIDR")
	}
}

func TestCIDRMatcher_Clear(t *testing.T) {
	cm := NewCIDRMatcher()

	cm.Add("10.0.0.0/8")
	if !cm.Contains("10.1.2.3") {
		t.Error("Should match before Clear")
	}

	cm.Clear()

	if cm.Contains("10.1.2.3") {
		t.Error("Should not match after Clear")
	}
}

func TestCIDRMatcher_ContainsIP(t *testing.T) {
	cm := NewCIDRMatcher()
	cm.Add("10.0.0.0/8")

	ip := net.ParseIP("10.1.2.3")
	if !cm.ContainsIP(ip) {
		t.Error("Should match IP")
	}

	if cm.ContainsIP(nil) {
		t.Error("Should not match nil")
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.0.1", true},
		{"192.168.255.255", true},
		{"127.0.0.1", true},
		{"127.255.255.255", true},
		{"169.254.0.1", true},
		{"169.254.255.255", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"172.32.0.1", false},
		{"192.169.0.1", false},
		{"::1", true},
		{"fe80::1", true},
		{"fc00::1", true},
		{"2001:db8::1", false},
	}

	for _, tt := range tests {
		if got := IsPrivateIP(tt.ip); got != tt.private {
			t.Errorf("IsPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
		}
	}
}

func TestIsPrivateIP_Invalid(t *testing.T) {
	if IsPrivateIP("invalid") {
		t.Error("Should return false for invalid IP")
	}
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.1", "192.168.1.1"},
		{"192.168.1.1:8080", "192.168.1.1"},
		{"[::1]:8080", "::1"},
		{"10.0.0.1", "10.0.0.1"},
	}

	for _, tt := range tests {
		if got := ExtractIP(tt.input); got != tt.expected {
			t.Errorf("ExtractIP(%s) = %s, want %s", tt.input, got, tt.expected)
		}
	}
}

func TestParsePort(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"80", 80},
		{"8080", 8080},
		{"443", 443},
		{"65535", 65535},
		{"0", 0},
		{"65536", 0},
		{"abc", 0},
		{"", 0},
	}

	for _, tt := range tests {
		if got := ParsePort(tt.input); got != tt.expected {
			t.Errorf("ParsePort(%s) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestIsValidIP(t *testing.T) {
	tests := []struct {
		ip    string
		valid bool
	}{
		{"192.168.1.1", true},
		{"10.0.0.1", true},
		{"::1", true},
		{"2001:db8::1", true},
		{"invalid", false},
		{"", false},
		{"256.1.1.1", false},
	}

	for _, tt := range tests {
		if got := IsValidIP(tt.ip); got != tt.valid {
			t.Errorf("IsValidIP(%s) = %v, want %v", tt.ip, got, tt.valid)
		}
	}
}

func TestIsIPv4(t *testing.T) {
	tests := []struct {
		ip     string
		isIPv4 bool
	}{
		{"192.168.1.1", true},
		{"10.0.0.1", true},
		{"::1", false},
		{"2001:db8::1", false},
		{"invalid", false},
	}

	for _, tt := range tests {
		if got := IsIPv4(tt.ip); got != tt.isIPv4 {
			t.Errorf("IsIPv4(%s) = %v, want %v", tt.ip, got, tt.isIPv4)
		}
	}
}

func TestIsIPv6(t *testing.T) {
	tests := []struct {
		ip     string
		isIPv6 bool
	}{
		{"192.168.1.1", false},
		{"10.0.0.1", false},
		{"::1", true},
		{"2001:db8::1", true},
		{"::", true},
		{"invalid", false},
	}

	for _, tt := range tests {
		if got := IsIPv6(tt.ip); got != tt.isIPv6 {
			t.Errorf("IsIPv6(%s) = %v, want %v", tt.ip, got, tt.isIPv6)
		}
	}
}

func TestIPToUint32(t *testing.T) {
	tests := []struct {
		ip       string
		expected uint32
	}{
		{"0.0.0.0", 0},
		{"0.0.0.1", 1},
		{"1.0.0.0", 1 << 24},
		{"192.168.1.1", 0xC0A80101},
		{"255.255.255.255", 0xFFFFFFFF},
		{"::1", 0}, // IPv6 returns 0
		{"invalid", 0},
	}

	for _, tt := range tests {
		if got := IPToUint32(tt.ip); got != tt.expected {
			t.Errorf("IPToUint32(%s) = %d (0x%08x), want %d (0x%08x)",
				tt.ip, got, got, tt.expected, tt.expected)
		}
	}
}

func TestUint32ToIP(t *testing.T) {
	tests := []struct {
		n        uint32
		expected string
	}{
		{0, "0.0.0.0"},
		{1, "0.0.0.1"},
		{1 << 24, "1.0.0.0"},
		{0xC0A80101, "192.168.1.1"},
		{0xFFFFFFFF, "255.255.255.255"},
	}

	for _, tt := range tests {
		if got := Uint32ToIP(tt.n); got != tt.expected {
			t.Errorf("Uint32ToIP(%d) = %s, want %s", tt.n, got, tt.expected)
		}
	}
}

// TestExtractIPVariousFormats tests ExtractIP with various input formats
func TestExtractIPVariousFormats(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// IPv4 without port
		{"192.168.1.1", "192.168.1.1"},
		{"10.0.0.1", "10.0.0.1"},
		{"127.0.0.1", "127.0.0.1"},
		// IPv4 with port
		{"192.168.1.1:80", "192.168.1.1"},
		{"10.0.0.1:8080", "10.0.0.1"},
		{"127.0.0.1:3000", "127.0.0.1"},
		{"192.168.1.1:65535", "192.168.1.1"},
		// IPv6 without port
		{"::1", "::1"},
		{"2001:db8::1", "2001:db8::1"},
		{"fe80::1", "fe80::1"},
		// IPv6 with port (bracket notation)
		{"[::1]:80", "::1"},
		{"[2001:db8::1]:443", "2001:db8::1"},
		{"[fe80::1]:8080", "fe80::1"},
		// Edge cases
		{"[::1]", "[::1]"}, // No port, brackets remain (not valid but handled gracefully)
	}

	for _, tt := range tests {
		got := ExtractIP(tt.input)
		if got != tt.expected {
			t.Errorf("ExtractIP(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// TestIsPrivateIPEdgeCases tests IsPrivateIP with edge cases
func TestIsPrivateIPEdgeCases(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		// IPv4 private ranges
		{"10.0.0.0", true},
		{"10.255.255.255", true},
		{"172.16.0.0", true},
		{"172.31.255.255", true},
		{"192.168.0.0", true},
		{"192.168.255.255", true},
		{"127.0.0.0", true},
		{"127.255.255.255", true},
		{"169.254.0.0", true},
		{"169.254.255.255", true},
		// IPv4 public ranges
		{"9.255.255.255", false},
		{"11.0.0.0", false},
		{"172.15.255.255", false},
		{"172.32.0.0", false},
		{"192.167.255.255", false},
		{"192.169.0.0", false},
		{"1.1.1.1", false},
		{"8.8.8.8", false},
		// IPv6 private ranges
		{"::1", true},
		{"fe80::", true},
		{"fe80::ffff", true},
		{"febf::", true},
		{"fc00::", true},
		{"fdff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", true},
		// IPv6 public ranges
		{"fe00::", false},
		{"fec0::", false},
		{"2001:db8::", false},
		{"2000::", false},
		// Invalid inputs
		{"invalid", false},
		{"", false},
		{"256.1.1.1", false},
		{"192.168.1", false},
	}

	for _, tt := range tests {
		got := IsPrivateIP(tt.ip)
		if got != tt.private {
			t.Errorf("IsPrivateIP(%q) = %v, want %v", tt.ip, got, tt.private)
		}
	}
}

// TestParsePortEdgeCases tests ParsePort with edge cases
func TestParsePortEdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		// Valid ports
		{"1", 1},
		{"80", 80},
		{"443", 443},
		{"8080", 8080},
		{"65535", 65535},
		// Invalid ports
		{"0", 0},     // Port 0 is invalid
		{"65536", 0}, // Out of range
		{"-1", 0},    // Negative
		{"abc", 0},   // Non-numeric
		{"80abc", 0}, // Mixed
		{"", 0},      // Empty
		{" 80", 0},   // Leading space
		{"80 ", 0},   // Trailing space
		{"80 80", 0}, // Space in middle
		{"0x50", 0},  // Hex notation
	}

	for _, tt := range tests {
		got := ParsePort(tt.input)
		if got != tt.expected {
			t.Errorf("ParsePort(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

// TestCIDRMatcher_InvalidCIDRDetailed tests adding invalid CIDR ranges
func TestCIDRMatcher_InvalidCIDRDetailed(t *testing.T) {
	cm := NewCIDRMatcher()

	invalidCIDRs := []string{
		"invalid",
		"",
		"192.168.1.1",     // Missing mask
		"192.168.1.1/",    // Empty mask
		"192.168.1.1/abc", // Invalid mask
		"192.168.1.1/33",  // Mask too large for IPv4
		"256.1.1.1/24",    // Invalid IP
		"192.168.1/24",    // Incomplete IP
		"::1/129",         // Mask too large for IPv6
	}

	for _, cidr := range invalidCIDRs {
		err := cm.Add(cidr)
		if err == nil {
			t.Errorf("Add(%q) should fail", cidr)
		}
	}
}

// TestCIDRMatcher_ContainsInvalidIP tests Contains with invalid IPs
func TestCIDRMatcher_ContainsInvalidIP(t *testing.T) {
	cm := NewCIDRMatcher()
	cm.Add("10.0.0.0/8")

	invalidIPs := []string{
		"invalid",
		"",
		"256.1.1.1",
		"192.168.1",
		"not-an-ip",
	}

	for _, ip := range invalidIPs {
		if cm.Contains(ip) {
			t.Errorf("Contains(%q) should be false for invalid IP", ip)
		}
	}
}

// TestCIDRMatcher_IPv6Matching tests IPv6 CIDR matching
func TestCIDRMatcher_IPv6Matching(t *testing.T) {
	cm := NewCIDRMatcher()

	// Add IPv6 ranges
	ranges := []string{
		"2001:db8::/32", // Documentation range
		"fe80::/10",     // Link-local
		"fc00::/7",      // ULA
		"::1/128",       // Loopback
	}

	for _, r := range ranges {
		if err := cm.Add(r); err != nil {
			t.Fatalf("Failed to add %s: %v", r, err)
		}
	}

	tests := []struct {
		ip      string
		matches bool
	}{
		// 2001:db8::/32
		{"2001:db8::1", true},
		{"2001:db8:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"2001:db9::1", false},
		{"2001:db7::1", false},
		// fe80::/10
		{"fe80::1", true},
		{"fe80::ffff", true},
		{"febf::1", true},
		{"fec0::1", false},
		// fc00::/7
		{"fc00::1", true},
		{"fd00::1", true},
		{"fdff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"fe00::1", false},
		// ::1/128
		{"::1", true},
		{"::2", false},
	}

	for _, tt := range tests {
		got := cm.Contains(tt.ip)
		if got != tt.matches {
			t.Errorf("Contains(%q) = %v, want %v", tt.ip, got, tt.matches)
		}
	}
}

func TestCIDRMatcher_Len(t *testing.T) {
	cm := NewCIDRMatcher()
	if cm.Len() != 0 {
		t.Errorf("empty Len = %d", cm.Len())
	}
	cm.Add("10.0.0.0/8")
	if cm.Len() != 1 {
		t.Errorf("after add Len = %d", cm.Len())
	}
	cm.Add("192.168.0.0/16")
	if cm.Len() != 2 {
		t.Errorf("after 2 adds Len = %d", cm.Len())
	}
}

// TestCIDRMatcher_ConcurrentAccess tests concurrent access to CIDR matcher
func TestCIDRMatcher_ConcurrentAccess(t *testing.T) {
	cm := NewCIDRMatcher()

	// Add initial ranges
	ranges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}
	for _, r := range ranges {
		cm.Add(r)
	}

	const numGoroutines = 50
	const iterations = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Readers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				cm.Contains("10.0.0.1")
				cm.Contains("192.168.1.1")
				cm.Contains("8.8.8.8")
			}
		}(i)
	}

	// Writers (adding new ranges)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				cm.Add(fmt.Sprintf("10.%d.%d.0/24", id, j))
			}
		}(i)
	}

	wg.Wait()
}
