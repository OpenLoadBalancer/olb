package ssrf

import (
	"net"
	"strings"
	"testing"

	"github.com/openloadbalancer/olb/internal/waf/detection"
)

func newCtx(query string) *detection.RequestContext {
	return &detection.RequestContext{
		DecodedQuery: query,
		DecodedBody:  "",
		BodyParams:   make(map[string]string),
		Headers:      make(map[string][]string),
		Cookies:      make(map[string]string),
	}
}

func TestSSRFDetector_InternalURLs(t *testing.T) {
	d := New()
	attacks := []struct {
		name  string
		input string
	}{
		{"localhost", "url=http://localhost/admin"},
		{"loopback", "url=http://127.0.0.1/admin"},
		{"cloud metadata", "url=http://169.254.169.254/latest/meta-data/"},
		{"private 10.x", "url=http://10.0.0.1/internal"},
		{"private 192.168", "url=http://192.168.1.1/admin"},
	}

	for _, tt := range attacks {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newCtx(tt.input)
			findings := d.Detect(ctx)
			if len(findings) == 0 {
				t.Errorf("expected SSRF detection for %q", tt.input)
			}
		})
	}
}

func TestSSRFDetector_ExternalURLs(t *testing.T) {
	d := New()
	benign := []string{
		"url=http://example.com/api",
		"url=http://cdn.example.com/image.png",
		"callback=http://webhook.site/abc123",
	}

	for _, input := range benign {
		ctx := newCtx(input)
		findings := d.Detect(ctx)
		totalScore := 0
		for _, f := range findings {
			totalScore += f.Score
		}
		if totalScore >= 50 {
			t.Errorf("expected no significant SSRF for external URL %q, got score %d", input, totalScore)
		}
	}
}

func TestSSRFDetector_BodyParams(t *testing.T) {
	d := New()
	ctx := &detection.RequestContext{
		DecodedQuery: "",
		DecodedBody:  "",
		BodyParams: map[string]string{
			"webhook": "http://169.254.169.254/latest/meta-data/",
		},
		Headers: make(map[string][]string),
		Cookies: make(map[string]string),
	}

	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected SSRF detection for cloud metadata URL in body param")
	}
}

func TestSSRFDetector_DecimalIP(t *testing.T) {
	d := New()
	// 2130706433 = 127.0.0.1 in decimal
	ctx := newCtx("url=http://2130706433/admin")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected SSRF detection for decimal IP encoding")
	}
	for _, f := range findings {
		if f.Rule == "decimal_ip" {
			if f.Score < 80 {
				t.Errorf("expected score >= 80 for decimal IP, got %d", f.Score)
			}
			return
		}
	}
	// May also match as localhost
}

func TestSSRFDetector_OctalIP(t *testing.T) {
	d := New()
	// 0177.0.0.1 = 127.0.0.1 in octal
	ctx := newCtx("url=http://0177.0.0.01/admin")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected SSRF detection for octal IP encoding")
	}
	foundOctal := false
	for _, f := range findings {
		if f.Rule == "octal_ip" {
			foundOctal = true
			if f.Score < 80 {
				t.Errorf("expected score >= 80 for octal IP, got %d", f.Score)
			}
		}
	}
	if !foundOctal {
		// May be matched by other rules, that's acceptable
		t.Log("octal IP not detected as 'octal_ip' rule, but may be caught by other rules")
	}
}

func TestSSRFDetector_CredentialBypass(t *testing.T) {
	d := New()
	// http://attacker@127.0.0.1 bypass
	ctx := newCtx("url=http://attacker@127.0.0.1/admin")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected SSRF detection for credential bypass URL")
	}
}

func TestSSRFDetector_CredentialBypassPrivateIP(t *testing.T) {
	d := New()
	ctx := newCtx("url=http://user@10.0.0.1/internal")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected SSRF detection for credential bypass to private IP")
	}
}

func TestSSRFDetector_ExtractHost(t *testing.T) {
	tests := []struct {
		url  string
		host string
	}{
		{"http://example.com/path", "example.com"},
		{"https://10.0.0.1:8080/api", "10.0.0.1"},
		{"http://user@localhost/admin", "localhost"},
		{"http://[::1]:80/path", "::1"},
		{"http://hostname", "hostname"},
	}
	d := New()
	_ = d
	for _, tt := range tests {
		got := extractHost(tt.url)
		if got != tt.host {
			t.Errorf("extractHost(%q) = %q, want %q", tt.url, got, tt.host)
		}
	}
}

func TestSSRFDetector_IsPrivateIP_AllRanges(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"172.15.0.1", false},
		{"172.32.0.1", false},
		{"192.168.0.1", true},
		{"192.168.255.255", true},
		{"127.0.0.1", true},
		{"127.0.0.2", true},
		{"169.254.0.1", true},
		{"169.254.169.254", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"::1", true},
		{"fd00::1", true},
		{"2001:db8::1", false},
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("failed to parse IP %q", tt.ip)
		}
		got := isPrivateIP(ip)
		if got != tt.private {
			t.Errorf("isPrivateIP(%q) = %v, want %v", tt.ip, got, tt.private)
		}
	}
}

func TestSSRFDetector_IsInternalHost(t *testing.T) {
	tests := []struct {
		host     string
		internal bool
	}{
		{"localhost", true},
		{"127.0.0.1", true},
		{"[::1]", true},
		{"::1", true},
		{"0.0.0.0", true},
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"example.com", false},
		{"8.8.8.8", false},
	}

	for _, tt := range tests {
		got := isInternalHost(tt.host)
		if got != tt.internal {
			t.Errorf("isInternalHost(%q) = %v, want %v", tt.host, got, tt.internal)
		}
	}
}

func TestSSRFDetector_CloudMetadata(t *testing.T) {
	d := New()
	metadataURLs := []string{
		"url=http://169.254.169.254/latest/meta-data/",
		"url=http://metadata.google.internal/computeMetadata/v1/",
		"url=http://100.100.100.200/latest/meta-data/",
	}

	for _, input := range metadataURLs {
		ctx := newCtx(input)
		findings := d.Detect(ctx)
		if len(findings) == 0 {
			t.Errorf("expected SSRF detection for cloud metadata %q", input)
		}
	}
}

func TestSSRFDetector_NoURLs(t *testing.T) {
	d := New()
	ctx := newCtx("just some plain text without URLs")
	findings := d.Detect(ctx)
	if len(findings) != 0 {
		t.Errorf("expected no findings for plain text, got %d", len(findings))
	}
}

func TestSSRFDetector_EmptyInputs(t *testing.T) {
	d := New()
	ctx := &detection.RequestContext{
		DecodedQuery: "",
		DecodedBody:  "",
		BodyParams:   make(map[string]string),
		Headers:      make(map[string][]string),
		Cookies:      make(map[string]string),
	}
	findings := d.Detect(ctx)
	if len(findings) != 0 {
		t.Errorf("expected no findings for empty inputs, got %d", len(findings))
	}
}

func TestSSRFDetector_BodyDetection(t *testing.T) {
	d := New()
	ctx := &detection.RequestContext{
		DecodedQuery: "",
		DecodedBody:  "redirect to http://127.0.0.1/admin please",
		BodyParams:   make(map[string]string),
		Headers:      make(map[string][]string),
		Cookies:      make(map[string]string),
	}
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected SSRF detection in body content")
	}
}

func TestSSRFDetector_Truncate(t *testing.T) {
	short := truncate("short", 80)
	if short != "short" {
		t.Errorf("expected 'short', got %q", short)
	}

	long := truncate(strings.Repeat("a", 100), 80)
	if len(long) != 83 { // 80 + "..."
		t.Errorf("expected length 83, got %d", len(long))
	}
}

func TestSSRFDetector_IPv6Loopback(t *testing.T) {
	d := New()
	ctx := newCtx("url=http://[::1]/admin")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected SSRF detection for IPv6 loopback [::1]")
	}
}

func TestSSRFDetector_IPv6ULA(t *testing.T) {
	d := New()
	ctx := newCtx("url=http://[fd00::1]/internal")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected SSRF detection for IPv6 ULA fd00::1")
	}
}

func TestSSRFDetector_AWSIPv6Metadata(t *testing.T) {
	d := New()
	ctx := newCtx("url=http://[fd00:ec2::254]/latest/meta-data/")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected SSRF detection for AWS IPv6 metadata endpoint")
	}
}

func TestSSRFDetector_MixedCaseHostname(t *testing.T) {
	d := New()
	ctx := newCtx("url=http://LoCaLhOsT/admin")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected SSRF detection for mixed-case localhost")
	}
}

func TestSSRFDetector_ShortIPForms(t *testing.T) {
	d := New()
	tests := []struct {
		name  string
		input string
	}{
		{"zero ip", "url=http://0/"},
		{"zero zero zero zero", "url=http://0.0.0.0/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newCtx(tt.input)
			findings := d.Detect(ctx)
			if len(findings) == 0 {
				t.Errorf("expected SSRF detection for %q", tt.input)
			}
		})
	}
}

func TestSSRFDetector_AlibabaMetadata(t *testing.T) {
	d := New()
	ctx := newCtx("url=http://100.100.100.200/latest/meta-data/")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected SSRF detection for Alibaba Cloud metadata 100.100.100.200")
	}
}

func TestSSRFDetector_GCPMetadata(t *testing.T) {
	d := New()
	tests := []struct {
		name  string
		input string
	}{
		{"google.internal", "url=http://metadata.google.internal/computeMetadata/v1/"},
		{"metadata.google", "url=http://metadata.google/computeMetadata/v1/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newCtx(tt.input)
			findings := d.Detect(ctx)
			if len(findings) == 0 {
				t.Errorf("expected SSRF detection for GCP metadata %q", tt.input)
			}
			found := false
			for _, f := range findings {
				if f.Rule == "cloud_metadata" {
					found = true
					if f.Score < 90 {
						t.Errorf("expected score >= 90 for cloud metadata, got %d", f.Score)
					}
				}
			}
			if !found {
				t.Error("expected cloud_metadata rule")
			}
		})
	}
}

func TestSSRFDetector_MultipleURLs(t *testing.T) {
	d := New()
	ctx := newCtx("url=http://example.com/safe callback=http://127.0.0.1/secret")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected SSRF detection when second URL targets localhost")
	}
}

func TestSSRFDetector_Private172Range(t *testing.T) {
	// Test boundary values of 172.16-31 range
	tests := []struct {
		ip      string
		private bool
	}{
		{"172.16.0.1", true},
		{"172.20.0.1", true},
		{"172.31.255.255", true},
		{"172.15.255.255", false},
		{"172.32.0.1", false},
	}
	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		got := isPrivateIP(ip)
		if got != tt.private {
			t.Errorf("isPrivateIP(%q) = %v, want %v", tt.ip, got, tt.private)
		}
	}
}

func TestSSRFDetector_LinkLocal(t *testing.T) {
	d := New()
	// 169.254.x.x is link-local (used for cloud metadata)
	ctx := newCtx("url=http://169.254.0.1/")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected detection for link-local IP 169.254.0.1")
	}
}

func TestSSRFDetector_CredentialBypassExternal(t *testing.T) {
	d := New()
	// Credential bypass to external host should not trigger
	ctx := newCtx("url=http://user:pass@example.com/api")
	findings := d.Detect(ctx)
	totalScore := 0
	for _, f := range findings {
		totalScore += f.Score
	}
	if totalScore >= 50 {
		t.Errorf("expected no significant SSRF for credential URL to external host, got score %d", totalScore)
	}
}

func TestSSRFDetector_URLWithPort(t *testing.T) {
	d := New()
	ctx := newCtx("url=http://127.0.0.1:8080/admin")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected SSRF detection for localhost with port")
	}
}
