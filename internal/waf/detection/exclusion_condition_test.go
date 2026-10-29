package detection

import "testing"

func TestExclusionCondition(t *testing.T) {
	ex := func(cond string) *Engine {
		return &Engine{exclusions: []Exclusion{{PathPattern: "/hook", Detectors: []string{"sqli"}, Condition: cond}}}
	}
	cases := []struct {
		cond        string
		whitelisted bool
		want        bool
	}{
		{"", false, true},
		{"always", false, true},
		{"whitelist", false, false},
		{"whitelist", true, true},
	}
	for _, c := range cases {
		got := ex(c.cond).isExcluded(&RequestContext{Path: "/hook", IsWhitelisted: c.whitelisted}, "sqli")
		if got != c.want {
			t.Errorf("condition %q whitelisted=%v: excluded=%v, want %v", c.cond, c.whitelisted, got, c.want)
		}
	}
}
