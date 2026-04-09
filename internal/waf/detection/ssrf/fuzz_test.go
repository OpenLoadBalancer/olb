package ssrf

import (
	"testing"

	"github.com/openloadbalancer/olb/internal/waf/detection"
)

func FuzzSSRFDetector(f *testing.F) {
	f.Add("http://169.254.169.254/latest/meta-data/")
	f.Add("http://127.0.0.1:8080/admin")
	f.Add("http://[::1]/admin")
	f.Add("http://0x7f000001/")
	f.Add("http://example.com/normal")
	f.Add("")
	f.Add("gopher://internal-host:25/")
	f.Add("http://192.168.1.1/")
	f.Add("https://valid-website.com/path")
	f.Add("http://0177.0.0.1/")

	d := New()

	f.Fuzz(func(t *testing.T, input string) {
		ctx := &detection.RequestContext{
			DecodedQuery: input,
			BodyParams:   make(map[string]string),
			Headers:      make(map[string][]string),
			Cookies:      make(map[string]string),
		}
		findings := d.Detect(ctx)
		for _, finding := range findings {
			if finding.Score < 0 || finding.Score > 100 {
				t.Errorf("invalid score %d for input %q", finding.Score, input)
			}
		}
	})
}
