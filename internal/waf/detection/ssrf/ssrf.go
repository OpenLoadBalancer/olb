// Package ssrf provides Server-Side Request Forgery detection for the WAF.
package ssrf

import (
	"net"
	"regexp"
	"strings"

	"github.com/openloadbalancer/olb/internal/waf/detection"
)

// Detector implements SSRF detection.
type Detector struct {
	// Precompiled patterns
	urlPattern   *regexp.Regexp
	decimalIPPat *regexp.Regexp
	octalIPPat   *regexp.Regexp
}

// New creates a new SSRF detector.
func New() *Detector {
	return &Detector{
		urlPattern:   regexp.MustCompile(`(?i)https?://[^\s"'<>]+`),
		decimalIPPat: regexp.MustCompile(`(?i)https?://\d{8,10}(?:/|$|\s)`),
		octalIPPat:   regexp.MustCompile(`(?i)https?://0\d+\.0?\d+\.0?\d+\.0?\d+`),
	}
}

// Name returns the detector name.
func (d *Detector) Name() string { return "ssrf" }

// Detect analyzes request fields for SSRF patterns.
func (d *Detector) Detect(ctx *detection.RequestContext) []detection.Finding {
	var findings []detection.Finding

	// Check query parameters, body params, and body for URLs
	targets := []struct {
		value    string
		location string
	}{
		{ctx.DecodedQuery, "query"},
		{ctx.DecodedBody, "body"},
	}
	for name, value := range ctx.BodyParams {
		targets = append(targets, struct {
			value    string
			location string
		}{value, "param:" + name})
	}

	for _, target := range targets {
		if target.value == "" {
			continue
		}
		if f := d.analyzeURLs(target.value, target.location); f != nil {
			findings = append(findings, *f)
		}
	}

	return findings
}

func (d *Detector) analyzeURLs(input, location string) *detection.Finding {
	urls := d.urlPattern.FindAllString(input, 10) // limit to 10 URLs per field
	if len(urls) == 0 {
		return nil
	}

	var maxScore int
	var maxRule, maxEvidence string

	for _, u := range urls {
		score, rule := d.scoreURL(u)
		if score > maxScore {
			maxScore = score
			maxRule = rule
			maxEvidence = truncate(u, 80)
		}
	}

	// Check for decimal IP encoding: http://2130706433
	if d.decimalIPPat.MatchString(input) {
		score := 90
		if score > maxScore {
			maxScore = score
			maxRule = "decimal_ip"
			maxEvidence = "decimal IP encoding"
		}
	}

	// Check for octal IP encoding: http://0177.0.0.1
	if d.octalIPPat.MatchString(input) {
		score := 90
		if score > maxScore {
			maxScore = score
			maxRule = "octal_ip"
			maxEvidence = "octal IP encoding"
		}
	}

	// Check for URL with @ (credential-based bypass): http://a@127.0.0.1
	if strings.Contains(input, "@") {
		for _, u := range urls {
			if strings.Contains(u, "@") {
				atIdx := strings.Index(u, "@")
				host := u[atIdx+1:]
				if slashIdx := strings.Index(host, "/"); slashIdx > 0 {
					host = host[:slashIdx]
				}
				host = strings.TrimSuffix(host, ":")
				if h, _, err := net.SplitHostPort(host); err == nil {
					host = h
				} else if colonIdx := strings.LastIndex(host, ":"); colonIdx > 0 {
					host = host[:colonIdx]
				}
				if isInternalHost(host) {
					score := 75
					if score > maxScore {
						maxScore = score
						maxRule = "credential_bypass"
						maxEvidence = truncate(u, 80)
					}
				}
			}
		}
	}

	if maxScore == 0 {
		return nil
	}

	return &detection.Finding{
		Detector: "ssrf",
		Score:    maxScore,
		Location: location,
		Evidence: maxEvidence,
		Rule:     maxRule,
	}
}

func (d *Detector) scoreURL(u string) (int, string) {
	// Extract host from URL
	host := extractHost(u)
	if host == "" {
		return 0, ""
	}

	lower := strings.ToLower(host)

	// Cloud metadata endpoints
	for _, meta := range cloudMetadataHosts {
		if lower == meta || strings.HasPrefix(lower, meta) {
			return 95, "cloud_metadata"
		}
	}

	// Localhost variants
	if lower == "localhost" || lower == "127.0.0.1" || lower == "[::1]" ||
		lower == "0.0.0.0" || lower == "0" {
		return 80, "localhost"
	}

	// Private IP ranges
	ip := net.ParseIP(host)
	if ip != nil {
		if isPrivateIP(ip) {
			return 70, "private_ip"
		}
	}

	return 0, ""
}

// isPrivateIP, isInternalHost, extractHost, cloudMetadataHosts are in ipcheck.go

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
