package ssrf

import (
	"net"
	"strings"
)

// isPrivateIP checks if an IP address is in a private/reserved range.
func isPrivateIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 10 {
			return true
		}
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		if ip4[0] == 127 {
			return true
		}
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		return false
	}
	// IPv6 loopback and ULA
	if ip.Equal(net.ParseIP("::1")) {
		return true
	}
	if ip[0]&0xfe == 0xfc {
		return true
	}
	return false
}

// isInternalHost checks if a host string resolves to an internal/private address.
func isInternalHost(host string) bool {
	lower := strings.ToLower(host)
	// Strip IPv6 brackets if present
	lower = strings.TrimPrefix(lower, "[")
	lower = strings.TrimSuffix(lower, "]")
	if lower == "localhost" || lower == "127.0.0.1" || lower == "::1" || lower == "0.0.0.0" {
		return true
	}
	ip := net.ParseIP(lower)
	if ip != nil {
		return isPrivateIP(ip)
	}
	return false
}

// extractHost extracts the hostname from a URL string.
// Strips brackets from IPv6 addresses so the result is parseable by net.ParseIP.
func extractHost(u string) string {
	s := u
	if idx := strings.Index(s, "://"); idx >= 0 {
		s = s[idx+3:]
	}
	if idx := strings.IndexByte(s, '/'); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.IndexByte(s, '@'); idx >= 0 {
		s = s[idx+1:]
	}
	// Strip IPv6 brackets: [::1]:80 -> ::1
	if strings.Contains(s, "[") {
		s = strings.TrimPrefix(s, "[")
		if closeIdx := strings.IndexByte(s, ']'); closeIdx >= 0 {
			s = s[:closeIdx]
		}
	} else if idx := strings.LastIndexByte(s, ':'); idx >= 0 {
		// Strip port for non-IPv6 hosts
		s = s[:idx]
	}
	return s
}

// cloudMetadataHosts are cloud provider metadata endpoint addresses.
var cloudMetadataHosts = []string{
	"169.254.169.254",
	"metadata.google",
	"metadata.google.internal",
	"100.100.100.200", // Alibaba Cloud
	"fd00:ec2::254",   // AWS IPv6
}
