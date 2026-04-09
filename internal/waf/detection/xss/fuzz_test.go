package xss

import (
	"testing"

	"github.com/openloadbalancer/olb/internal/waf/detection"
)

func FuzzXSSDetector(f *testing.F) {
	f.Add("<script>alert(1)</script>")
	f.Add("<img src=x onerror=alert(1)>")
	f.Add("javascript:alert(1)")
	f.Add("<svg/onload=alert(1)>")
	f.Add("hello world")
	f.Add("<b>bold text</b>")
	f.Add("")
	f.Add("<<script>script>alert(1)</script>")
	f.Add("%3Cscript%3Ealert(1)%3C/script%3E")
	f.Add("<a href=\"data:text/html,<script>alert(1)</script>\">click</a>")

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
			if finding.Score < 0 {
				t.Errorf("invalid negative score %d for input %q", finding.Score, input)
			}
		}
	})
}
