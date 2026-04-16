// Package pathtraversal provides path traversal detection for the WAF.
package pathtraversal

import (
	"strings"

	"github.com/openloadbalancer/olb/internal/waf/detection"
)

// Detector implements path traversal detection.
type Detector struct{}

// New creates a new path traversal detector.
func New() *Detector { return &Detector{} }

// Name returns the detector name.
func (d *Detector) Name() string { return "path_traversal" }

// Detect analyzes request for path traversal patterns.
func (d *Detector) Detect(ctx *detection.RequestContext) []detection.Finding {
	var findings []detection.Finding

	// Check URL path (both raw and decoded)
	for _, path := range []string{ctx.Path, ctx.DecodedPath, ctx.RawPath} {
		if path == "" {
			continue
		}
		if f := d.analyzePath(path, "path"); f != nil {
			findings = append(findings, *f)
			break // one finding per location is enough
		}
	}

	// Check query parameters
	if ctx.DecodedQuery != "" {
		if f := d.analyzePath(ctx.DecodedQuery, "query"); f != nil {
			findings = append(findings, *f)
		}
	}

	// Check body params
	for name, value := range ctx.BodyParams {
		if f := d.analyzePath(value, "param:"+name); f != nil {
			findings = append(findings, *f)
		}
	}

	return findings
}

func (d *Detector) analyzePath(input, location string) *detection.Finding {
	lower := strings.ToLower(input)
	var maxScore int
	var maxRule, maxEvidence string

	// Count traversal depth
	depth := countTraversals(lower)
	if depth > 0 {
		score := 40
		if depth >= 3 {
			score = 70
		} else if depth >= 2 {
			score = 55
		}
		if score > maxScore {
			maxScore = score
			maxRule = "traversal_sequence"
			maxEvidence = "../"
		}
	}

	// Encoded traversals (literal dots, encoded separator)
	if strings.Contains(lower, "..%2f") || strings.Contains(lower, "..%5c") {
		score := 80
		if score > maxScore {
			maxScore = score
			maxRule = "encoded_traversal"
			maxEvidence = "..%2f"
		}
	}

	// Fully encoded traversals (encoded dots, any separator)
	if strings.Contains(lower, "%2e%2e/") || strings.Contains(lower, "%2e%2e%2f") || strings.Contains(lower, "%2e%2e%5c") {
		score := 80
		if score > maxScore {
			maxScore = score
			maxRule = "encoded_traversal"
			maxEvidence = "%2e%2e"
		}
	}

	// Double-encoded traversals
	if strings.Contains(lower, "..%252f") || strings.Contains(lower, "%252e%252e") {
		score := 90
		if score > maxScore {
			maxScore = score
			maxRule = "double_encoded_traversal"
			maxEvidence = "..%252f"
		}
	}

	// Overlong UTF-8 encoding for /
	if strings.Contains(lower, "%c0%af") || strings.Contains(lower, "%c1%9c") {
		score := 95
		if score > maxScore {
			maxScore = score
			maxRule = "overlong_utf8"
			maxEvidence = "%c0%af"
		}
	}

	// Sensitive file paths
	for _, sp := range sensitivePaths {
		if strings.Contains(lower, sp.path) {
			if sp.score > maxScore {
				maxScore = sp.score
				maxRule = "sensitive_path"
				maxEvidence = sp.path
			}
		}
	}

	// file:// protocol
	if strings.Contains(lower, "file://") {
		score := 70
		if score > maxScore {
			maxScore = score
			maxRule = "file_protocol"
			maxEvidence = "file://"
		}
	}

	// Null byte path traversal
	if strings.Contains(lower, "%00") || strings.Contains(input, "\x00") {
		score := 80
		if score > maxScore {
			maxScore = score
			maxRule = "null_byte_traversal"
			maxEvidence = "%00"
		}
	}

	if maxScore == 0 {
		return nil
	}

	return &detection.Finding{
		Detector: "path_traversal",
		Score:    maxScore,
		Location: location,
		Evidence: maxEvidence,
		Rule:     maxRule,
	}
}

func countTraversals(s string) int {
	count := 0
	for i := 0; i < len(s)-2; i++ {
		if s[i] == '.' && s[i+1] == '.' && (i+2 >= len(s) || s[i+2] == '/' || s[i+2] == '\\') {
			count++
			i += 2
		}
	}
	return count
}

// sensitivePaths is defined in sensitive_paths.go
