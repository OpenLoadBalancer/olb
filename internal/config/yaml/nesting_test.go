package yaml

import (
	"fmt"
	"reflect"
	"testing"
)

// A key that dedents to an intermediate level (not all the way to the root)
// after a deeper block must stay a sibling at that level. The parser used to
// close the mapping on any DEDENT, so such keys leaked to the document root —
// e.g. middleware.rate_limit after middleware.ip_filter.allow_list became a
// top-level key and was silently ignored.
func TestUnmarshal_DedentToIntermediateLevel(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want map[string]any
	}{
		{
			name: "sibling after nested sequence",
			src:  "a:\n  b:\n    list:\n      - x\n  c:\n    k: v\nd: 1\n",
			want: map[string]any{
				"a": map[string]any{
					"b": map[string]any{"list": []any{"x"}},
					"c": map[string]any{"k": "v"},
				},
				"d": 1,
			},
		},
		{
			name: "sibling after three-deep mapping",
			src:  "a:\n  b:\n    c:\n      d: 1\n  e: 2\nf: 3\n",
			want: map[string]any{
				"a": map[string]any{
					"b": map[string]any{"c": map[string]any{"d": 1}},
					"e": 2,
				},
				"f": 3,
			},
		},
		{
			name: "middleware blocks",
			src: "middleware:\n  ip_filter:\n    enabled: true\n    allow_list:\n      - 10.0.0.0/8\n" +
				"  rate_limit:\n    enabled: true\n    requests_per_second: 100\nadmin:\n  address: x\n",
			want: map[string]any{
				"middleware": map[string]any{
					"ip_filter":  map[string]any{"enabled": true, "allow_list": []any{"10.0.0.0/8"}},
					"rate_limit": map[string]any{"enabled": true, "requests_per_second": 100},
				},
				"admin": map[string]any{"address": "x"},
			},
		},
		{
			name: "key after list inside a sequence item",
			src: "listeners:\n  - name: http\n    routes:\n      - path: /\n        pool: p\n" +
				"    address: \":80\"\npools: []\n",
			want: map[string]any{
				"listeners": []any{map[string]any{
					"name":    "http",
					"routes":  []any{map[string]any{"path": "/", "pool": "p"}},
					"address": ":80",
				}},
				"pools": []any{},
			},
		},
		{
			name: "trailing comment on a key whose value is a list",
			src:  "a:\n  b:\n    list:     # note\n      - x\n  c: 1\n",
			want: map[string]any{
				"a": map[string]any{"b": map[string]any{"list": []any{"x"}}, "c": 1},
			},
		},
		{
			name: "comment-only value then sibling (nested)",
			src:  "a:\n  users:   # c\n    # admin: x\n  realm: r\n",
			want: map[string]any{"a": map[string]any{"users": "", "realm": "r"}},
		},
		{
			name: "comment-only value then sibling (root, first key)",
			src:  "users:   # c\n  # admin: x\nrealm: r\n",
			want: map[string]any{"users": "", "realm": "r"},
		},
		{
			name: "same-level sequence value",
			src:  "a:\n- x\n- y\nb: 1\n",
			want: map[string]any{"a": []any{"x", "y"}, "b": 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]any
			if err := Unmarshal([]byte(tt.src), &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			// Compare structure; scalar typing into interface{} is not what
			// this test is about.
			if !reflect.DeepEqual(scalarsAsStrings(got), scalarsAsStrings(tt.want)) {
				t.Errorf("got  %#v\nwant %#v", got, tt.want)
			}
		})
	}
}

func scalarsAsStrings(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = scalarsAsStrings(e)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = scalarsAsStrings(e)
		}
		return out
	default:
		return fmt.Sprint(v)
	}
}

func TestUnmarshal_EmptyValueLeavesCollectionsZero(t *testing.T) {
	var out struct {
		Users map[string]string `yaml:"users"`
		Paths []string          `yaml:"paths"`
		Inner *struct {
			X int `yaml:"x"`
		} `yaml:"inner"`
		Realm string `yaml:"realm"`
	}
	src := "users:   # none yet\n  # admin: x\npaths:\ninner: ~\nrealm: r\n"
	if err := Unmarshal([]byte(src), &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Users != nil || out.Paths != nil || out.Inner != nil {
		t.Errorf("empty values should leave zero values, got %+v", out)
	}
	if out.Realm != "r" {
		t.Errorf("realm = %q, want r", out.Realm)
	}
}
