package config

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// UnknownKeys walks a raw parsed config (as produced by the YAML, TOML or
// HCL parsers) against the Config struct and returns a message for every key
// that is not a config field. The decoders ignore unknown keys, so without
// this a typo — "lest_connections", "rate_limt:" — silently does nothing.
//
// Each message names the dotted path and, when a real field is a close
// match, suggests it.
func UnknownKeys(raw map[string]any) []string {
	var out []string
	walkUnknownKeys(raw, reflect.TypeFor[Config](), "", &out)
	sort.Strings(out)
	return out
}

func walkUnknownKeys(raw any, t reflect.Type, path string, out *[]string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch v := raw.(type) {
	case []any:
		// A list of items or blocks: check each against the element type (or,
		// for a struct fed by a single HCL block, the struct itself).
		elem := t
		if t.Kind() == reflect.Slice {
			elem = t.Elem()
		}
		for _, item := range v {
			walkUnknownKeys(item, elem, path, out)
		}
	case map[string]any:
		if t.Kind() != reflect.Struct {
			return // free-form maps (users, response_add, ...) take any key
		}
		fields := yamlFields(t)
		for k, sub := range v {
			ft, ok := fields[k]
			if !ok {
				msg := fmt.Sprintf("unknown config key %q", strings.TrimPrefix(path+"."+k, "."))
				if s := closestKey(k, fields); s != "" {
					msg += fmt.Sprintf(" (did you mean %q?)", s)
				}
				*out = append(*out, msg)
				continue
			}
			walkUnknownKeys(sub, ft, path+"."+k, out)
		}
	}
}

// yamlFields maps a struct's yaml tag names to their field types.
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

// closestKey returns the field name within edit distance 2 of k, if any.
func closestKey(k string, fields map[string]reflect.Type) string {
	best, bestDist := "", 3
	for name := range fields {
		if d := editDistance(k, name); d < bestDist || (d == bestDist && name < best) {
			best, bestDist = name, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
