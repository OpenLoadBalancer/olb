package hcl

import (
	"strings"
	"testing"
)

// A singleton block such as `admin { ... }` must decode into a struct (or a
// pointer to one). Blocks are collected as lists so repeated blocks can fill
// slices, and a one-element list was rejected for struct targets — so the
// example olb.hcl could not be loaded at all.
func TestDecode_SingletonBlockIntoStruct(t *testing.T) {
	src := `
admin {
  address = ":9090"
  limits {
    max = 3
  }
}
listeners {
  name = "a"
}
listeners {
  name = "b"
}
`
	var out struct {
		Admin *struct {
			Address string `hcl:"address"`
			Limits  struct {
				Max int `hcl:"max"`
			} `hcl:"limits"`
		} `hcl:"admin"`
		Listeners []struct {
			Name string `hcl:"name"`
		} `hcl:"listeners"`
	}
	if err := Decode([]byte(src), &out); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if out.Admin == nil || out.Admin.Address != ":9090" || out.Admin.Limits.Max != 3 {
		t.Errorf("admin = %+v", out.Admin)
	}
	if len(out.Listeners) != 2 || out.Listeners[1].Name != "b" {
		t.Errorf("listeners = %+v", out.Listeners)
	}
}

func TestDecode_RepeatedBlockIntoStructRejected(t *testing.T) {
	var out struct {
		Admin struct {
			Address string `hcl:"address"`
		} `hcl:"admin"`
	}
	err := Decode([]byte("admin {\n address = \"a\"\n}\nadmin {\n address = \"b\"\n}\n"), &out)
	if err == nil || !strings.Contains(err.Error(), "admin") {
		t.Errorf("Decode error = %v, want a repeated-block error naming admin", err)
	}
}
