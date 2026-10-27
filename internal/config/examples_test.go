package config

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
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

			var unknown []string
			collectUnknownKeys(raw, reflect.TypeFor[Config](), "", &unknown)
			sort.Strings(unknown)
			for _, k := range unknown {
				t.Errorf("unknown key %q (not a config field; it would be silently ignored)", k)
			}
		})
	}
}

// collectUnknownKeys walks raw against the struct type t by yaml tag.
func collectUnknownKeys(raw any, t reflect.Type, path string, unknown *[]string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch v := raw.(type) {
	case []any:
		// A list of blocks/items: each element is checked against the element
		// type (or, for a struct field fed by a single HCL block, the struct).
		elem := t
		if t.Kind() == reflect.Slice {
			elem = t.Elem()
		}
		for _, item := range v {
			collectUnknownKeys(item, elem, path, unknown)
		}
	case map[string]any:
		if t.Kind() != reflect.Struct {
			return // free-form maps (users, response_add, ...) take any key
		}
		fields := yamlFields(t)
		for k, sub := range v {
			ft, ok := fields[k]
			if !ok {
				*unknown = append(*unknown, strings.TrimPrefix(path+"."+k, "."))
				continue
			}
			collectUnknownKeys(sub, ft, path+"."+k, unknown)
		}
	}
}

func yamlFields(t reflect.Type) map[string]reflect.Type {
	out := make(map[string]reflect.Type)
	for i := range t.NumField() {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		out[name] = f.Type
	}
	return out
}
