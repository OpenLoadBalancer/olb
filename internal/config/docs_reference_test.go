package config

import (
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/openloadbalancer/olb/internal/config/yaml"
)

// docs/configuration.md is the reference users copy from. Unknown keys are
// silently ignored by the decoders, so a stale or invented key in the docs
// gives users settings that do nothing. Every ```yaml block must therefore be
// a complete config document (rooted at the top level) that uses only real
// config fields.
func TestDocsConfigurationReference(t *testing.T) {
	data, err := os.ReadFile("../../docs/configuration.md")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile("(?s)```yaml\n(.*?)```")
	blocks := re.FindAllStringSubmatch(string(data), -1)
	if len(blocks) == 0 {
		t.Fatal("no ```yaml blocks found in docs/configuration.md")
	}
	for i, m := range blocks {
		var raw map[string]any
		if err := yaml.UnmarshalString(m[1], &raw); err != nil {
			t.Errorf("yaml block %d: does not parse as a config document: %v", i, err)
			continue
		}
		var unknown []string
		collectUnknownKeys(raw, reflect.TypeFor[Config](), "", &unknown)
		for _, k := range unknown {
			t.Errorf("yaml block %d: unknown key %q (not a config field; it would be silently ignored)", i, k)
		}
	}
}
