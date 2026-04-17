package pathtraversal

import (
	"testing"

	"github.com/openloadbalancer/olb/internal/waf/detection"
)

func newCtx(path, query string) *detection.RequestContext {
	return &detection.RequestContext{
		Path:         path,
		DecodedPath:  path,
		DecodedQuery: query,
		BodyParams:   make(map[string]string),
		Headers:      make(map[string][]string),
		Cookies:      make(map[string]string),
	}
}

func TestPathTraversal_BasicTraversal(t *testing.T) {
	d := New()
	attacks := []struct {
		name  string
		path  string
		query string
	}{
		{"single dot-dot", "/../etc/passwd", ""},
		{"triple traversal", "/../../../etc/passwd", ""},
		{"in query", "/page", "file=../../etc/passwd"},
		{"sensitive file", "/etc/passwd", ""},
		{"etc shadow", "/etc/shadow", ""},
		{"proc self", "/proc/self/environ", ""},
	}

	for _, tt := range attacks {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newCtx(tt.path, tt.query)
			findings := d.Detect(ctx)
			if len(findings) == 0 {
				t.Errorf("expected path traversal detection for path=%q query=%q", tt.path, tt.query)
			}
		})
	}
}

func TestPathTraversal_EncodedTraversal(t *testing.T) {
	d := New()
	tests := []string{
		"..%2fetc/passwd",
		"..%252f..%252f",
		"%c0%afetc/passwd",
	}

	for _, input := range tests {
		ctx := newCtx("/"+input, "")
		findings := d.Detect(ctx)
		if len(findings) == 0 {
			t.Errorf("expected detection for encoded traversal %q", input)
		}
	}
}

func TestPathTraversal_Benign(t *testing.T) {
	d := New()
	benign := []string{
		"/api/users",
		"/static/css/style.css",
		"/health",
	}

	for _, path := range benign {
		ctx := newCtx(path, "")
		findings := d.Detect(ctx)
		totalScore := 0
		for _, f := range findings {
			totalScore += f.Score
		}
		if totalScore >= 50 {
			t.Errorf("expected no significant detection for benign path %q, got score %d", path, totalScore)
		}
	}
}

func TestDetector_Name(t *testing.T) {
	d := New()
	if d.Name() != "path_traversal" {
		t.Errorf("expected name 'path_traversal', got %q", d.Name())
	}
}

func TestDetect_EmptyPaths(t *testing.T) {
	d := New()
	ctx := &detection.RequestContext{
		Path:         "",
		DecodedPath:  "",
		RawPath:      "",
		DecodedQuery: "",
		BodyParams:   make(map[string]string),
		Headers:      make(map[string][]string),
		Cookies:      make(map[string]string),
	}
	findings := d.Detect(ctx)
	if len(findings) != 0 {
		t.Errorf("expected no findings for empty paths, got %d", len(findings))
	}
}

func TestDetect_RawPath(t *testing.T) {
	d := New()
	// If Path and DecodedPath are empty but RawPath has traversal
	ctx := &detection.RequestContext{
		Path:         "",
		DecodedPath:  "",
		RawPath:      "/../../../etc/passwd",
		DecodedQuery: "",
		BodyParams:   make(map[string]string),
		Headers:      make(map[string][]string),
		Cookies:      make(map[string]string),
	}
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected detection from RawPath")
	}
}

func TestDetect_BodyParams(t *testing.T) {
	d := New()
	ctx := &detection.RequestContext{
		Path:         "/api",
		DecodedPath:  "/api",
		DecodedQuery: "",
		BodyParams:   map[string]string{"file": "../../../../etc/passwd"},
		Headers:      make(map[string][]string),
		Cookies:      make(map[string]string),
	}
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected detection from body param")
	}
}

func TestCountTraversals(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"../../etc", 2},
		{"../../../etc", 3},
		{"no-traversal", 0},
		{"..\\..\\etc", 2}, // backslash
		{"a..b", 0},        // not traversal (no / after)
		{"../", 1},
	}
	for _, tt := range tests {
		got := countTraversals(tt.input)
		if got != tt.want {
			t.Errorf("countTraversals(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestPathTraversal_DoubleEncoded(t *testing.T) {
	d := New()
	ctx := newCtx("/..%252f..%252fetc/passwd", "")
	findings := d.Detect(ctx)
	found := false
	for _, f := range findings {
		if f.Rule == "double_encoded_traversal" {
			found = true
		}
	}
	if !found {
		t.Error("expected double_encoded_traversal rule")
	}
}

func TestPathTraversal_DoubleEncodedDotDot(t *testing.T) {
	d := New()
	ctx := newCtx("/%252e%252e/etc/passwd", "")
	findings := d.Detect(ctx)
	found := false
	for _, f := range findings {
		if f.Rule == "double_encoded_traversal" {
			found = true
		}
	}
	if !found {
		t.Error("expected double_encoded_traversal for %252e%252e")
	}
}

func TestPathTraversal_OverlongUTF8(t *testing.T) {
	d := New()
	tests := []string{
		"/..%c0%afetc/passwd",
		"/..%c1%9cetc/passwd",
	}
	for _, path := range tests {
		ctx := newCtx(path, "")
		findings := d.Detect(ctx)
		found := false
		for _, f := range findings {
			if f.Rule == "overlong_utf8" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected overlong_utf8 rule for %q", path)
		}
	}
}

func TestPathTraversal_FileProtocol(t *testing.T) {
	d := New()
	// Use file:// without a sensitive path so file_protocol is not overridden by higher scoring rules
	ctx := newCtx("/page", "url=file:///tmp/something")
	findings := d.Detect(ctx)
	found := false
	for _, f := range findings {
		if f.Rule == "file_protocol" {
			found = true
		}
	}
	if !found {
		t.Error("expected file_protocol rule")
	}
}

func TestPathTraversal_NullByte(t *testing.T) {
	d := New()
	// %00 null byte injection — use a path without sensitive files so null_byte_traversal isn't overridden
	ctx := newCtx("/somefile%00.jpg", "")
	findings := d.Detect(ctx)
	found := false
	for _, f := range findings {
		if f.Rule == "null_byte_traversal" {
			found = true
		}
	}
	if !found {
		t.Error("expected null_byte_traversal rule for %00")
	}
}

func TestPathTraversal_NullByteRaw(t *testing.T) {
	d := New()
	// Raw null byte — use a path without sensitive files
	ctx := newCtx("/somefile\x00.jpg", "")
	findings := d.Detect(ctx)
	found := false
	for _, f := range findings {
		if f.Rule == "null_byte_traversal" {
			found = true
		}
	}
	if !found {
		t.Error("expected null_byte_traversal rule for raw null byte")
	}
}

func TestPathTraversal_EncodedBackslash(t *testing.T) {
	d := New()
	ctx := newCtx("/..%5c..%5cetc/passwd", "")
	findings := d.Detect(ctx)
	found := false
	for _, f := range findings {
		if f.Rule == "encoded_traversal" {
			found = true
		}
	}
	if !found {
		t.Error("expected encoded_traversal rule for ..%5c")
	}
}

func TestPathTraversal_AllSensitivePaths(t *testing.T) {
	d := New()
	paths := []string{
		"/etc/passwd",
		"/etc/shadow",
		"/etc/hosts",
		"/proc/self/environ",
		"/proc/version",
		"/proc/cmdline",
		"/path/to/win.ini",
		"/path/to/boot.ini",
		"/path/to/web.config",
		"/path/to/.htaccess",
		"/path/to/.htpasswd",
		"/path/to/.env",
		"/path/to/.git/config",
		"/path/to/.ssh/",
		"/path/to/id_rsa",
		"/path/to/authorized_keys",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			ctx := newCtx(path, "")
			findings := d.Detect(ctx)
			found := false
			for _, f := range findings {
				if f.Rule == "sensitive_path" {
					found = true
				}
			}
			if !found {
				t.Errorf("expected sensitive_path rule for %q", path)
			}
		})
	}
}

func TestPathTraversal_DepthScoring(t *testing.T) {
	d := New()

	// Single traversal => score 40
	ctx1 := newCtx("/../safe", "")
	findings1 := d.Detect(ctx1)
	var score1 int
	for _, f := range findings1 {
		if f.Rule == "traversal_sequence" {
			score1 = f.Score
		}
	}

	// Double traversal => score 55
	ctx2 := newCtx("/../../safe", "")
	findings2 := d.Detect(ctx2)
	var score2 int
	for _, f := range findings2 {
		if f.Rule == "traversal_sequence" {
			score2 = f.Score
		}
	}

	// Triple traversal => score 70
	ctx3 := newCtx("/../../../safe", "")
	findings3 := d.Detect(ctx3)
	var score3 int
	for _, f := range findings3 {
		if f.Rule == "traversal_sequence" {
			score3 = f.Score
		}
	}

	if score1 != 40 {
		t.Errorf("expected score 40 for single traversal, got %d", score1)
	}
	if score2 != 55 {
		t.Errorf("expected score 55 for double traversal, got %d", score2)
	}
	if score3 != 70 {
		t.Errorf("expected score 70 for triple traversal, got %d", score3)
	}
}

func TestPathTraversal_NoTraversal(t *testing.T) {
	d := New()
	ctx := newCtx("/normal/path", "q=hello")
	findings := d.Detect(ctx)
	if len(findings) != 0 {
		t.Errorf("expected no findings for normal path, got %d", len(findings))
	}
}

func TestCountTraversals_ShortString(t *testing.T) {
	// Strings shorter than 3 chars
	if countTraversals("") != 0 {
		t.Error("expected 0 for empty string")
	}
	if countTraversals("ab") != 0 {
		t.Error("expected 0 for 2-char string")
	}
}

func TestPathTraversal_QueryWithTraversal(t *testing.T) {
	d := New()
	ctx := newCtx("/api", "../../etc/shadow")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected detection for query with traversal")
	}
}

func TestPathTraversal_FullyEncodedDots(t *testing.T) {
	d := New()
	tests := []struct {
		name string
		path string
	}{
		{"encoded dots with literal slash", "/%2e%2e/safe/path"},
		{"encoded dots with encoded slash", "/%2e%2e%2fsafe%2fpath"},
		{"encoded dots with encoded backslash", "/%2e%2e%5csafe%5cpath"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newCtx(tt.path, "")
			findings := d.Detect(ctx)
			found := false
			for _, f := range findings {
				if f.Rule == "encoded_traversal" {
					found = true
					if f.Score != 80 {
						t.Errorf("expected score 80, got %d", f.Score)
					}
				}
			}
			if !found {
				t.Errorf("expected encoded_traversal for %q", tt.path)
			}
		})
	}
}

func TestPathTraversal_MixedEncoding(t *testing.T) {
	d := New()
	tests := []struct {
		name string
		path string
		rule string
	}{
		{"literal + encoded slash", "/..%2f..%2fsafe", "encoded_traversal"},
		{"encoded dots + literal", "/%2e%2e/../safe", "encoded_traversal"},
		{"case-insensitive encoding", "/%2E%2E%2Fsafe", "encoded_traversal"},
		{"case-insensitive dots", "/..%2F..%2Fsafe", "encoded_traversal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newCtx(tt.path, "")
			findings := d.Detect(ctx)
			if len(findings) == 0 {
				t.Fatalf("expected detection for %q", tt.path)
			}
			found := false
			for _, f := range findings {
				if f.Rule == tt.rule {
					found = true
				}
			}
			if !found {
				t.Errorf("expected rule %q for %q, got rules: %v", tt.rule, tt.path, findingsToRules(findings))
			}
		})
	}
}

func TestPathTraversal_RepeatedEncodedTraversal(t *testing.T) {
	d := New()
	// Deep traversal with all encoded components
	ctx := newCtx("/%2e%2e/%2e%2e/%2e%2e/safe", "")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected detection for repeated encoded traversal")
	}
}

func TestPathTraversal_EncodedDotsInQuery(t *testing.T) {
	d := New()
	ctx := newCtx("/page", "file=%2e%2e/%2e%2e/safe")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected detection for encoded dots in query")
	}
	found := false
	for _, f := range findings {
		if f.Rule == "encoded_traversal" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected encoded_traversal rule in query, got: %v", findingsToRules(findings))
	}
}

func TestPathTraversal_NullByteWithTraversal(t *testing.T) {
	d := New()
	ctx := newCtx("/../safe%00.jpg", "")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected detection for null byte with traversal")
	}
}

func TestPathTraversal_BackslashTraversal(t *testing.T) {
	d := New()
	ctx := newCtx("/..\\..\\safe", "")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected detection for backslash traversal")
	}
}

func TestPathTraversal_EncodedBackslashDots(t *testing.T) {
	d := New()
	ctx := newCtx("/%2e%2e%5c%2e%2e%5csafe", "")
	findings := d.Detect(ctx)
	if len(findings) == 0 {
		t.Error("expected detection for encoded backslash traversal")
	}
}

func findingsToRules(findings []detection.Finding) []string {
	rules := make([]string, len(findings))
	for i, f := range findings {
		rules[i] = f.Rule
	}
	return rules
}
