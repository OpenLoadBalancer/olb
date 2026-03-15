package utils

import (
	"encoding/binary"
	"net"
	"strings"
	"sync"
)

// CIDRMatcher provides fast IP matching against CIDR ranges using a radix trie.
// Supports both IPv4 and IPv6 addresses. All methods are safe for concurrent use.
type CIDRMatcher struct {
	mu    sync.RWMutex
	root4 *cidrNode
	root6 *cidrNode
}

type cidrNode struct {
	child  [2]*cidrNode // 0 or 1
	isLeaf bool
}

// NewCIDRMatcher creates a new CIDR matcher.
func NewCIDRMatcher() *CIDRMatcher {
	return &CIDRMatcher{
		root4: &cidrNode{},
		root6: &cidrNode{},
	}
}

// Add adds a CIDR range to the matcher.
// Returns error if the CIDR is invalid. Safe for concurrent use.
func (cm *CIDRMatcher) Add(cidr string) error {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	if ipnet.IP.To4() != nil {
		cm.addCIDR(cm.root4, ipnet.IP.To4(), ipnet.Mask)
	} else {
		cm.addCIDR(cm.root6, ipnet.IP.To16(), ipnet.Mask)
	}

	return nil
}

// addCIDR adds a CIDR to the trie.
func (cm *CIDRMatcher) addCIDR(root *cidrNode, ip net.IP, mask net.IPMask) {
	node := root
	bits := len(ip) * 8
	ones, _ := mask.Size()

	for i := 0; i < ones && i < bits; i++ {
		byteIdx := i / 8
		bitIdx := 7 - (i % 8)
		bit := (ip[byteIdx] >> bitIdx) & 1

		if node.child[bit] == nil {
			node.child[bit] = &cidrNode{}
		}
		node = node.child[bit]
	}

	node.isLeaf = true
}

// Contains checks if an IP address matches any of the added CIDR ranges.
// Safe for concurrent use.
func (cm *CIDRMatcher) Contains(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if parsedIP.To4() != nil {
		return cm.match(cm.root4, parsedIP.To4())
	}
	return cm.match(cm.root6, parsedIP.To16())
}

// match checks if IP matches the trie.
func (cm *CIDRMatcher) match(root *cidrNode, ip net.IP) bool {
	node := root
	if node.isLeaf {
		return true
	}

	bits := len(ip) * 8

	for i := 0; i < bits; i++ {
		byteIdx := i / 8
		bitIdx := 7 - (i % 8)
		bit := (ip[byteIdx] >> bitIdx) & 1

		if node.child[bit] == nil {
			// Check if current node is a leaf (CIDR matched)
			return node.isLeaf
		}

		node = node.child[bit]
		if node.isLeaf {
			return true
		}
	}

	return node.isLeaf
}

// ContainsIP checks if a net.IP matches any of the added CIDR ranges.
// Safe for concurrent use.
func (cm *CIDRMatcher) ContainsIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if ip.To4() != nil {
		return cm.match(cm.root4, ip.To4())
	}
	return cm.match(cm.root6, ip.To16())
}

// Len returns the number of CIDR ranges added. Safe for concurrent use.
func (cm *CIDRMatcher) Len() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.count(cm.root4) + cm.count(cm.root6)
}

// count recursively counts leaves in the trie.
func (cm *CIDRMatcher) count(node *cidrNode) int {
	if node == nil {
		return 0
	}
	if node.isLeaf {
		return 1
	}
	return cm.count(node.child[0]) + cm.count(node.child[1])
}

// Clear removes all CIDR ranges. Safe for concurrent use.
func (cm *CIDRMatcher) Clear() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.root4 = &cidrNode{}
	cm.root6 = &cidrNode{}
}

// ExtractIP extracts the IP address from a string.
// Handles "IP:port" format.
func ExtractIP(addr string) string {
	// Check if it has a port
	if strings.Contains(addr, ":") {
		// Could be IPv6 or IPv4 with port
		host, _, err := net.SplitHostPort(addr)
		if err == nil {
			return host
		}
	}
	return addr
}

// ParsePort parses a port string and returns the port number.
// Returns 0 if invalid.
func ParsePort(port string) int {
	if port == "" {
		return 0
	}
	p, err := parseUint16(port)
	if err != nil || p == 0 {
		return 0
	}
	return int(p)
}

// parseUint16 parses a string as uint16.
func parseUint16(s string) (uint16, error) {
	// Fast path for common cases
	if s == "" {
		return 0, nil
	}

	var n uint16
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, nil
		}
		n = n*10 + uint16(c-'0')
		if n > 65535 {
			return 0, nil
		}
	}
	return n, nil
}

// IsPrivateIP checks if an IP address is in a private range.
func IsPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	// IPv4 private ranges
	if ip4 := ip.To4(); ip4 != nil {
		// 10.0.0.0/8
		if ip4[0] == 10 {
			return true
		}
		// 172.16.0.0/12
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		// 192.168.0.0/16
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		// 127.0.0.0/8 (loopback)
		if ip4[0] == 127 {
			return true
		}
		// 169.254.0.0/16 (link-local)
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		return false
	}

	// IPv6
	// ::1/128 (loopback)
	if ip.Equal(net.ParseIP("::1")) {
		return true
	}
	// fc00::/7 (ULA)
	if ip[0]&0xfe == 0xfc {
		return true
	}
	// fe80::/10 (link-local)
	if ip[0] == 0xfe && (ip[1]&0xc0) == 0x80 {
		return true
	}

	return false
}

// IPToUint32 converts an IPv4 address to uint32.
// Returns 0 for invalid or IPv6 addresses.
func IPToUint32(ipStr string) uint32 {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return 0
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return 0
	}
	return binary.BigEndian.Uint32(ip4)
}

// Uint32ToIP converts a uint32 to an IPv4 address string.
func Uint32ToIP(n uint32) string {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, n)
	return ip.String()
}

// IsValidIP checks if a string is a valid IP address.
func IsValidIP(ipStr string) bool {
	return net.ParseIP(ipStr) != nil
}

// IsIPv4 checks if a string is a valid IPv4 address.
func IsIPv4(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return ip.To4() != nil
}

// IsIPv6 checks if a string is a valid IPv6 address.
func IsIPv6(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return ip.To4() == nil && ip.To16() != nil
}
