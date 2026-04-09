package cmdi

import (
	"testing"

	"github.com/openloadbalancer/olb/internal/waf/detection"
)

func FuzzCMDIDetector(f *testing.F) {
	f.Add("; cat /etc/passwd")
	f.Add("| whoami")
	f.Add("$(cat /etc/passwd)")
	f.Add("`id`")
	f.Add("hello world")
	f.Add("file.txt; echo pwned")
	f.Add("& net user")
	f.Add("127.0.0.1; ls -la")
	f.Add("normal filename.txt")
	f.Add("")
	f.Add("1 && dir")

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
