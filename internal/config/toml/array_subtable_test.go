package toml

import "testing"

// Each [[pools]] element may have its own [pools.health_check] sub-table.
// The parser tracked defined tables globally, so the second element's
// sub-table was rejected as a duplicate.
func TestParse_SubTablePerArrayElement(t *testing.T) {
	src := `
[[pools]]
name = "a"
[pools.health_check]
path = "/a"

[[pools]]
name = "b"
[pools.health_check]
path = "/b"
`
	var out struct {
		Pools []struct {
			Name        string `toml:"name"`
			HealthCheck struct {
				Path string `toml:"path"`
			} `toml:"health_check"`
		} `toml:"pools"`
	}
	if err := Decode([]byte(src), &out); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(out.Pools) != 2 || out.Pools[0].HealthCheck.Path != "/a" || out.Pools[1].HealthCheck.Path != "/b" {
		t.Errorf("got %+v, want two pools with /a and /b health checks", out.Pools)
	}
}

func TestParse_DuplicateSubTableInSameElementStillRejected(t *testing.T) {
	src := "[[pools]]\n[pools.health_check]\npath = \"/a\"\n[pools.health_check]\npath = \"/b\"\n"
	var out map[string]any
	if err := Decode([]byte(src), &out); err == nil {
		t.Error("expected a duplicate-table error within one array element")
	}
}
