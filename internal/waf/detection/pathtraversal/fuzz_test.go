package pathtraversal

import (
	"testing"

	"github.com/openloadbalancer/olb/internal/waf/detection"
)

func FuzzPathTraversalDetector(f *testing.F) {
	f.Add("../../../etc/passwd")
	f.Add("..\\..\\..\\windows\\system32")
	f.Add("/var/www/../../../etc/shadow")
	f.Add("normal/path/file.txt")
	f.Add("/%2e%2e/%2e%2e/etc/passwd")
	f.Add("....//....//etc/passwd")
	f.Add("/image.png")
	f.Add("")
	f.Add("..%252f..%252f..%252fetc/passwd")
	f.Add("/static/css/style.css")

	d := New()

	f.Fuzz(func(t *testing.T, input string) {
		ctx := &detection.RequestContext{
			Path:         input,
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
