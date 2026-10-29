// Package ipacl provides IP-based access control for the WAF layer.
package ipacl

import (
	"sync"
	"time"

	"github.com/openloadbalancer/olb/pkg/utils"
)

// Action represents the result of an IP ACL check.
type Action int

const (
	// ActionAllow means the IP is not in any list, proceed to next layer.
	ActionAllow Action = iota
	// ActionBlock means the IP is blacklisted.
	ActionBlock
	// ActionBypass means the IP is whitelisted, skip all subsequent WAF layers.
	ActionBypass
)

// RuleMetadata holds metadata about an IP ACL rule.
type RuleMetadata struct {
	ID        string    `json:"id"`
	CIDR      string    `json:"cidr"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"` // zero = never
	Reason    string    `json:"reason"`
	Source    string    `json:"source"` // "manual", "auto-ban", "raft-sync"
}

// IsExpired returns true if the rule has expired.
func (r *RuleMetadata) IsExpired() bool {
	if r.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(r.ExpiresAt)
}

// IPAccessList provides whitelist/blacklist IP access control using radix tries.
type IPAccessList struct {
	mu        sync.RWMutex
	whitelist *utils.CIDRMatcher
	blacklist *utils.CIDRMatcher
	whiteMeta map[string]*RuleMetadata // CIDR → metadata
	blackMeta map[string]*RuleMetadata
	autoBan   AutoBanConfig
	stopCh    chan struct{}
	idCounter int
}

// AutoBanConfig configures automatic IP banning.
type AutoBanConfig struct {
	Enabled    bool
	DefaultTTL time.Duration
	MaxTTL     time.Duration
}

// Config configures the IP access list.
type Config struct {
	Whitelist []EntryConfig
	Blacklist []EntryConfig
	AutoBan   AutoBanConfig
}

// EntryConfig represents a single whitelist/blacklist entry from config.
type EntryConfig struct {
	CIDR    string
	Reason  string
	Expires time.Time
}

// New creates a new IPAccessList.
func New(cfg Config) (*IPAccessList, error) {
	acl := &IPAccessList{
		whitelist: utils.NewCIDRMatcher(),
		blacklist: utils.NewCIDRMatcher(),
		whiteMeta: make(map[string]*RuleMetadata),
		blackMeta: make(map[string]*RuleMetadata),
		autoBan:   cfg.AutoBan,
		stopCh:    make(chan struct{}),
	}

	// Load initial whitelist
	for _, entry := range cfg.Whitelist {
		if err := acl.AddWhitelist(entry.CIDR, entry.Reason, entry.Expires); err != nil {
			return nil, err
		}
	}

	// Load initial blacklist
	for _, entry := range cfg.Blacklist {
		if err := acl.AddBlacklist(entry.CIDR, entry.Reason, entry.Expires); err != nil {
			return nil, err
		}
	}

	// Start expiry cleanup goroutine
	go acl.cleanupLoop()

	return acl, nil
}

// Check evaluates an IP against the whitelist and blacklist.
// Returns ActionBypass (whitelisted), ActionBlock (blacklisted), or ActionAllow (neither).
func (acl *IPAccessList) Check(ip string) Action {
	acl.mu.RLock()
	defer acl.mu.RUnlock()

	// Whitelist first — bypass all security
	if acl.whitelist.Contains(ip) {
		return ActionBypass
	}

	// Blacklist — block
	if acl.blacklist.Contains(ip) {
		return ActionBlock
	}

	return ActionAllow
}

// AddWhitelist adds a CIDR to the whitelist.
func (acl *IPAccessList) AddWhitelist(cidr, reason string, expires time.Time) error {
	acl.mu.Lock()
	defer acl.mu.Unlock()

	if err := acl.whitelist.Add(cidr); err != nil {
		return err
	}

	acl.idCounter++
	acl.whiteMeta[cidr] = &RuleMetadata{
		ID:        formatID("wl", acl.idCounter),
		CIDR:      cidr,
		CreatedAt: time.Now(),
		ExpiresAt: expires,
		Reason:    reason,
		Source:    "manual",
	}
	return nil
}

// AddBlacklist adds a CIDR to the blacklist.
func (acl *IPAccessList) AddBlacklist(cidr, reason string, expires time.Time) error {
	acl.mu.Lock()
	defer acl.mu.Unlock()

	if err := acl.blacklist.Add(cidr); err != nil {
		return err
	}

	acl.idCounter++
	acl.blackMeta[cidr] = &RuleMetadata{
		ID:        formatID("bl", acl.idCounter),
		CIDR:      cidr,
		CreatedAt: time.Now(),
		ExpiresAt: expires,
		Reason:    reason,
		Source:    "manual",
	}
	return nil
}

// RemoveWhitelist removes a CIDR from the whitelist.
func (acl *IPAccessList) RemoveWhitelist(cidr string) bool {
	acl.mu.Lock()
	defer acl.mu.Unlock()

	if _, ok := acl.whiteMeta[cidr]; !ok {
		return false
	}
	delete(acl.whiteMeta, cidr)
	acl.rebuildWhitelist()
	return true
}

// RemoveBlacklist removes a CIDR from the blacklist.
func (acl *IPAccessList) RemoveBlacklist(cidr string) bool {
	acl.mu.Lock()
	defer acl.mu.Unlock()

	if _, ok := acl.blackMeta[cidr]; !ok {
		return false
	}
	delete(acl.blackMeta, cidr)
	acl.rebuildBlacklist()
	return true
}

// Ban adds an IP to the blacklist with a TTL (auto-ban).
func (acl *IPAccessList) Ban(ip string, ttl time.Duration, reason string) error {
	if !acl.autoBan.Enabled {
		return nil
	}
	if ttl > acl.autoBan.MaxTTL && acl.autoBan.MaxTTL > 0 {
		ttl = acl.autoBan.MaxTTL
	}
	if ttl <= 0 {
		ttl = acl.autoBan.DefaultTTL
	}
	if ttl <= 0 {
		ttl = time.Hour
	}

	cidr := ip + "/32"
	expires := time.Now().Add(ttl)

	acl.mu.Lock()
	defer acl.mu.Unlock()

	if err := acl.blacklist.Add(cidr); err != nil {
		return err
	}

	acl.idCounter++
	acl.blackMeta[cidr] = &RuleMetadata{
		ID:        formatID("ab", acl.idCounter),
		CIDR:      cidr,
		CreatedAt: time.Now(),
		ExpiresAt: expires,
		Reason:    reason,
		Source:    "auto-ban",
	}
	return nil
}

// ListRules returns all rules of the specified type.
// listType: "whitelist", "blacklist", or "" for all.
func (acl *IPAccessList) ListRules(listType string) []RuleMetadata {
	acl.mu.RLock()
	defer acl.mu.RUnlock()

	var rules []RuleMetadata
	if listType == "" || listType == "all" || listType == "whitelist" {
		for _, meta := range acl.whiteMeta {
			rules = append(rules, *meta)
		}
	}
	if listType == "" || listType == "all" || listType == "blacklist" {
		for _, meta := range acl.blackMeta {
			rules = append(rules, *meta)
		}
	}
	return rules
}

// Stop stops the cleanup goroutine.
func (acl *IPAccessList) Stop() {
	close(acl.stopCh)
}

func (acl *IPAccessList) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			acl.cleanupExpired()
		case <-acl.stopCh:
			return
		}
	}
}

func (acl *IPAccessList) cleanupExpired() {
	acl.mu.Lock()
	defer acl.mu.Unlock()

	changed := false
	for cidr, meta := range acl.blackMeta {
		if meta.IsExpired() {
			delete(acl.blackMeta, cidr)
			changed = true
		}
	}
	if changed {
		acl.rebuildBlacklist()
	}

	changed = false
	for cidr, meta := range acl.whiteMeta {
		if meta.IsExpired() {
			delete(acl.whiteMeta, cidr)
			changed = true
		}
	}
	if changed {
		acl.rebuildWhitelist()
	}
}

func (acl *IPAccessList) rebuildBlacklist() {
	acl.blacklist.Clear()
	for cidr := range acl.blackMeta {
		acl.blacklist.Add(cidr)
	}
}

func (acl *IPAccessList) rebuildWhitelist() {
	acl.whitelist.Clear()
	for cidr := range acl.whiteMeta {
		acl.whitelist.Add(cidr)
	}
}

func formatID(prefix string, counter int) string {
	// Simple ID generation without fmt.Sprintf to avoid import
	s := prefix + "-"
	if counter == 0 {
		return s + "0"
	}
	digits := make([]byte, 0, 10)
	n := counter
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	// Reverse
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return s + string(digits)
}
