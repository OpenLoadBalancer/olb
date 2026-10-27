package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openloadbalancer/olb/internal/config/hcl"
	"github.com/openloadbalancer/olb/internal/config/toml"
	"github.com/openloadbalancer/olb/internal/config/yaml"
)

// The shipped examples are what users copy first. All four failed to load
// at one point (YAML parser, TOML sub-tables, HCL blocks, stale schema), and
// unknown keys are ignored silently, so also check that every key in each
// example is a real config field.
func TestExampleConfigs(t *testing.T) {
	files, err := filepath.Glob("../../configs/*")
	if err != nil || len(files) == 0 {
		t.Fatalf("no example configs found: %v", err)
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if _, err := Load(path); err != nil {
				t.Fatalf("Load: %v", err)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var raw map[string]any
			switch filepath.Ext(path) {
			case ".toml":
				raw, err = toml.Parse(data)
			case ".hcl":
				raw, err = hcl.Parse(data)
			default:
				err = yaml.UnmarshalString(string(data), &raw)
			}
			if err != nil {
				t.Fatalf("raw parse: %v", err)
			}

			for _, msg := range UnknownKeys(raw) {
				t.Errorf("%s (it would be silently ignored)", msg)
			}
		})
	}
}
