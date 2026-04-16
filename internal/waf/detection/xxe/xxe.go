// Package xxe provides XML External Entity detection for the WAF.
package xxe

import (
	"strings"

	"github.com/openloadbalancer/olb/internal/waf/detection"
)

// Detector implements XXE detection.
type Detector struct{}

// New creates a new XXE detector.
func New() *Detector { return &Detector{} }

// Name returns the detector name.
func (d *Detector) Name() string { return "xxe" }

// Detect analyzes request for XXE patterns.
// Only active when Content-Type contains "xml".
func (d *Detector) Detect(ctx *detection.RequestContext) []detection.Finding {
	// Only check XML content
	ct := strings.ToLower(ctx.ContentType)
	if !strings.Contains(ct, "xml") {
		return nil
	}

	if len(ctx.Body) == 0 {
		return nil
	}

	body := string(ctx.Body)

	// Check UTF-7 encoded XXE attempts (e.g., +ADw-!DOCTYPE for <!DOCTYPE)
	if decoded := decodeUTF7(body); decoded != body {
		if findings := d.analyzeXML(decoded); len(findings) > 0 {
			// Mark as UTF-7 bypass attempt
			findings[0].Rule = "utf7_" + findings[0].Rule
			return findings
		}
	}

	return d.analyzeXML(body)
}

// decodeUTF7 performs minimal UTF-7 decoding to detect obfuscated XXE payloads.
// UTF-7 encodes characters as +AAAA- sequences. Attackers use this to bypass
// XML filters (e.g., +ADw- = <, +AD4- = >, +ACM- = #).
func decodeUTF7(s string) string {
	if !strings.Contains(s, "+") {
		return s
	}
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '+' && i+1 < len(s) && s[i+1] != '-' && s[i+1] != '+' {
			// Find the end of the UTF-7 sequence
			end := strings.IndexByte(s[i+1:], '-')
			if end == -1 {
				b.WriteByte(s[i])
				i++
				continue
			}
			end += i + 1 // absolute position of '-'

			// Decode the base64-like portion between + and -
			encoded := s[i+1 : end]
			decoded := decodeUTF7Sequence(encoded)
			b.WriteString(decoded)
			i = end + 1
		} else if s[i] == '+' && i+1 < len(s) && s[i+1] == '-' {
			// "+-" is an escaped +
			b.WriteByte('+')
			i += 2
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// decodeUTF7Sequence decodes a UTF-7 modified base64 sequence.
// This handles the most common characters used in XXE bypass attempts.
func decodeUTF7Sequence(s string) string {
	// UTF-7 uses modified base64 where '=' is omitted and '/' maps differently.
	// Common UTF-7 sequences used in XXE:
	// +ADw- = <  (U+003C)
	// +AD4- = >  (U+003E)
	// +ACM- = #  (U+0023)
	// +ACI- = "  (U+0022)
	// +ACY- = &  (U+0026)
	// +ACU- = %  (U+0025)
	known := map[string]string{
		"ADW": "<", "AD4": ">", "ACM": "#", "ACI": "\"", "ACY": "&", "ACU": "%",
		"AAG": " ", "AAS": "'", "AA0": "\r", "AAW": "\n",
		"AEA": "@", "AEE": "!", "AEI": "\"", "AEM": "#",
		"AD0": "=", "AA8": "/", "ACW": ",", "AC4": ".",
		"AFS": "[", "AFQ": "]", "AF0": "]", "AWA": "`", "ACA": " ",
		"AAO": ";", "ABA": "(", "ABQ": ")",
	}
	if v, ok := known[strings.ToUpper(s)]; ok {
		return v
	}
	// Return original if not a known sequence
	return "+" + s + "-"
}

func (d *Detector) analyzeXML(body string) []detection.Finding {
	lower := strings.ToLower(body)
	var findings []detection.Finding
	var maxScore int
	var maxRule, maxEvidence string

	// <!DOCTYPE
	if strings.Contains(lower, "<!doctype") {
		maxScore = 30
		maxRule = "doctype"
		maxEvidence = "<!DOCTYPE"
	}

	// <!ENTITY
	if strings.Contains(lower, "<!entity") {
		score := 70
		if score > maxScore {
			maxScore = score
			maxRule = "entity_declaration"
			maxEvidence = "<!ENTITY"
		}
	}

	// Parameter entity: <!ENTITY %
	if strings.Contains(lower, "<!entity %") || strings.Contains(lower, "<!entity%") {
		score := 85
		if score > maxScore {
			maxScore = score
			maxRule = "parameter_entity"
			maxEvidence = "<!ENTITY %"
		}
	}

	// SYSTEM with file://
	if strings.Contains(lower, "system") {
		if strings.Contains(lower, "file://") || strings.Contains(lower, "file:") {
			score := 95
			if score > maxScore {
				maxScore = score
				maxRule = "system_file"
				maxEvidence = "SYSTEM file://"
			}
		}
		// SYSTEM with http:// (SSRF via XXE)
		if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
			score := 80
			if score > maxScore {
				maxScore = score
				maxRule = "system_http"
				maxEvidence = "SYSTEM http://"
			}
		}
		// SYSTEM with expect://
		if strings.Contains(lower, "expect://") {
			score := 95
			if score > maxScore {
				maxScore = score
				maxRule = "system_expect"
				maxEvidence = "SYSTEM expect://"
			}
		}
	}

	// SSI injection: <!--#include
	if strings.Contains(lower, "<!--#include") || strings.Contains(lower, "<!--#exec") {
		score := 70
		if score > maxScore {
			maxScore = score
			maxRule = "ssi_injection"
			maxEvidence = "<!--#include"
		}
	}

	if maxScore > 0 {
		findings = append(findings, detection.Finding{
			Detector: "xxe",
			Score:    maxScore,
			Location: "body",
			Evidence: maxEvidence,
			Rule:     maxRule,
		})
	}

	return findings
}
