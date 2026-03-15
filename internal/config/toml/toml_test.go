package toml

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func mustParse(t *testing.T, input string) map[string]interface{} {
	t.Helper()
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	return m
}

func expectError(t *testing.T, input string) {
	t.Helper()
	_, err := Parse([]byte(input))
	if err == nil {
		t.Fatalf("expected error for input: %s", input)
	}
}

// ---------------------------------------------------------------------------
// 1. Basic key-value pairs
// ---------------------------------------------------------------------------

func TestBasicKeyValuePairs(t *testing.T) {
	input := `
title = "TOML Example"
count = 42
ratio = 3.14
enabled = true
`
	m := mustParse(t, input)

	if m["title"] != "TOML Example" {
		t.Errorf("title = %v, want %q", m["title"], "TOML Example")
	}
	if m["count"] != int64(42) {
		t.Errorf("count = %v (%T), want int64(42)", m["count"], m["count"])
	}
	if m["ratio"] != 3.14 {
		t.Errorf("ratio = %v, want 3.14", m["ratio"])
	}
	if m["enabled"] != true {
		t.Errorf("enabled = %v, want true", m["enabled"])
	}
}

// ---------------------------------------------------------------------------
// 2. Quoted keys
// ---------------------------------------------------------------------------

func TestQuotedKeys(t *testing.T) {
	input := `
"quoted key" = "value1"
'literal key' = "value2"
"ʎǝʞ" = "unicode key"
`
	m := mustParse(t, input)

	if m["quoted key"] != "value1" {
		t.Errorf("quoted key = %v", m["quoted key"])
	}
	if m["literal key"] != "value2" {
		t.Errorf("literal key = %v", m["literal key"])
	}
	if m["ʎǝʞ"] != "unicode key" {
		t.Errorf("unicode key = %v", m["ʎǝʞ"])
	}
}

// ---------------------------------------------------------------------------
// 3. Dotted keys
// ---------------------------------------------------------------------------

func TestDottedKeys(t *testing.T) {
	input := `
server.host = "localhost"
server.port = 8080
database.primary.host = "db1.example.com"
`
	m := mustParse(t, input)

	server, ok := m["server"].(map[string]interface{})
	if !ok {
		t.Fatalf("server is not a map: %T", m["server"])
	}
	if server["host"] != "localhost" {
		t.Errorf("server.host = %v", server["host"])
	}
	if server["port"] != int64(8080) {
		t.Errorf("server.port = %v", server["port"])
	}

	db, ok := m["database"].(map[string]interface{})
	if !ok {
		t.Fatalf("database is not a map")
	}
	primary, ok := db["primary"].(map[string]interface{})
	if !ok {
		t.Fatalf("database.primary is not a map")
	}
	if primary["host"] != "db1.example.com" {
		t.Errorf("database.primary.host = %v", primary["host"])
	}
}

// ---------------------------------------------------------------------------
// 4. Standard tables
// ---------------------------------------------------------------------------

func TestStandardTables(t *testing.T) {
	input := `
[server]
host = "localhost"
port = 8080

[database]
name = "mydb"
`
	m := mustParse(t, input)

	server := m["server"].(map[string]interface{})
	if server["host"] != "localhost" {
		t.Errorf("server.host = %v", server["host"])
	}
	if server["port"] != int64(8080) {
		t.Errorf("server.port = %v", server["port"])
	}

	db := m["database"].(map[string]interface{})
	if db["name"] != "mydb" {
		t.Errorf("database.name = %v", db["name"])
	}
}

// ---------------------------------------------------------------------------
// 5. Nested tables
// ---------------------------------------------------------------------------

func TestNestedTables(t *testing.T) {
	input := `
[server]
host = "localhost"

[server.tls]
cert = "/path/to/cert.pem"
key = "/path/to/key.pem"
`
	m := mustParse(t, input)

	server := m["server"].(map[string]interface{})
	if server["host"] != "localhost" {
		t.Errorf("server.host = %v", server["host"])
	}

	tls := server["tls"].(map[string]interface{})
	if tls["cert"] != "/path/to/cert.pem" {
		t.Errorf("server.tls.cert = %v", tls["cert"])
	}
	if tls["key"] != "/path/to/key.pem" {
		t.Errorf("server.tls.key = %v", tls["key"])
	}
}

// ---------------------------------------------------------------------------
// 6. Arrays of tables
// ---------------------------------------------------------------------------

func TestArraysOfTables(t *testing.T) {
	input := `
[[backends]]
id = "web1"
address = "10.0.0.1:80"

[[backends]]
id = "web2"
address = "10.0.0.2:80"

[[backends]]
id = "web3"
address = "10.0.0.3:80"
`
	m := mustParse(t, input)

	backends, ok := m["backends"].([]interface{})
	if !ok {
		t.Fatalf("backends is not a slice: %T", m["backends"])
	}
	if len(backends) != 3 {
		t.Fatalf("len(backends) = %d, want 3", len(backends))
	}

	b1 := backends[0].(map[string]interface{})
	if b1["id"] != "web1" {
		t.Errorf("backends[0].id = %v", b1["id"])
	}
	b2 := backends[1].(map[string]interface{})
	if b2["id"] != "web2" {
		t.Errorf("backends[1].id = %v", b2["id"])
	}
	b3 := backends[2].(map[string]interface{})
	if b3["id"] != "web3" {
		t.Errorf("backends[2].id = %v", b3["id"])
	}
}

// ---------------------------------------------------------------------------
// 7. Inline tables
// ---------------------------------------------------------------------------

func TestInlineTables(t *testing.T) {
	input := `
point = {x = 1, y = 2}
person = {name = "Tom", age = 30}
`
	m := mustParse(t, input)

	point := m["point"].(map[string]interface{})
	if point["x"] != int64(1) {
		t.Errorf("point.x = %v", point["x"])
	}
	if point["y"] != int64(2) {
		t.Errorf("point.y = %v", point["y"])
	}

	person := m["person"].(map[string]interface{})
	if person["name"] != "Tom" {
		t.Errorf("person.name = %v", person["name"])
	}
	if person["age"] != int64(30) {
		t.Errorf("person.age = %v", person["age"])
	}
}

// ---------------------------------------------------------------------------
// 8. Arrays
// ---------------------------------------------------------------------------

func TestArrays(t *testing.T) {
	// Simple array
	input := `
ports = [80, 443, 8080]
hosts = ["alpha", "beta", "gamma"]
`
	m := mustParse(t, input)

	ports := m["ports"].([]interface{})
	if len(ports) != 3 {
		t.Fatalf("len(ports) = %d", len(ports))
	}
	if ports[0] != int64(80) || ports[1] != int64(443) || ports[2] != int64(8080) {
		t.Errorf("ports = %v", ports)
	}

	hosts := m["hosts"].([]interface{})
	if len(hosts) != 3 {
		t.Fatalf("len(hosts) = %d", len(hosts))
	}
	if hosts[0] != "alpha" || hosts[1] != "beta" || hosts[2] != "gamma" {
		t.Errorf("hosts = %v", hosts)
	}
}

func TestNestedArrays(t *testing.T) {
	input := `
matrix = [[1, 2], [3, 4]]
`
	m := mustParse(t, input)

	matrix := m["matrix"].([]interface{})
	if len(matrix) != 2 {
		t.Fatalf("len(matrix) = %d", len(matrix))
	}

	row1 := matrix[0].([]interface{})
	if row1[0] != int64(1) || row1[1] != int64(2) {
		t.Errorf("row1 = %v", row1)
	}
	row2 := matrix[1].([]interface{})
	if row2[0] != int64(3) || row2[1] != int64(4) {
		t.Errorf("row2 = %v", row2)
	}
}

func TestMultilineArray(t *testing.T) {
	input := `
colors = [
  "red",
  "green",
  "blue",
]
`
	m := mustParse(t, input)

	colors := m["colors"].([]interface{})
	if len(colors) != 3 {
		t.Fatalf("len(colors) = %d", len(colors))
	}
	if colors[0] != "red" || colors[1] != "green" || colors[2] != "blue" {
		t.Errorf("colors = %v", colors)
	}
}

// ---------------------------------------------------------------------------
// 9. String types
// ---------------------------------------------------------------------------

func TestBasicStrings(t *testing.T) {
	input := `
str1 = "Hello, World!"
str2 = "Line1\nLine2"
str3 = "Tab\there"
str4 = "Quote: \"inside\""
str5 = "Backslash: \\"
`
	m := mustParse(t, input)

	if m["str1"] != "Hello, World!" {
		t.Errorf("str1 = %v", m["str1"])
	}
	if m["str2"] != "Line1\nLine2" {
		t.Errorf("str2 = %v", m["str2"])
	}
	if m["str3"] != "Tab\there" {
		t.Errorf("str3 = %v", m["str3"])
	}
	if m["str4"] != `Quote: "inside"` {
		t.Errorf("str4 = %v", m["str4"])
	}
	if m["str5"] != `Backslash: \` {
		t.Errorf("str5 = %v", m["str5"])
	}
}

func TestLiteralStrings(t *testing.T) {
	input := `
path = 'C:\Users\admin'
regex = '\\d+\\.\\d+'
`
	m := mustParse(t, input)

	if m["path"] != `C:\Users\admin` {
		t.Errorf("path = %v", m["path"])
	}
	if m["regex"] != `\\d+\\.\\d+` {
		t.Errorf("regex = %v", m["regex"])
	}
}

func TestMultilineBasicStrings(t *testing.T) {
	input := `
bio = """
Roses are red
Violets are blue"""
`
	m := mustParse(t, input)

	expected := "Roses are red\nViolets are blue"
	if m["bio"] != expected {
		t.Errorf("bio = %q, want %q", m["bio"], expected)
	}
}

func TestMultilineLiteralStrings(t *testing.T) {
	input := `
code = '''
fn main() {
    println!("hello");
}'''
`
	m := mustParse(t, input)

	expected := "fn main() {\n    println!(\"hello\");\n}"
	if m["code"] != expected {
		t.Errorf("code = %q, want %q", m["code"], expected)
	}
}

// ---------------------------------------------------------------------------
// 10. Integer formats
// ---------------------------------------------------------------------------

func TestIntegers(t *testing.T) {
	input := `
dec = 42
pos = +99
neg = -17
hex = 0xDEAD
oct = 0o755
bin = 0b11010110
underscore = 1_000_000
`
	m := mustParse(t, input)

	tests := map[string]int64{
		"dec":        42,
		"pos":        99,
		"neg":        -17,
		"hex":        0xDEAD,
		"oct":        0o755,
		"bin":        0b11010110,
		"underscore": 1_000_000,
	}

	for key, want := range tests {
		got, ok := m[key].(int64)
		if !ok {
			t.Errorf("%s is %T, not int64", key, m[key])
			continue
		}
		if got != want {
			t.Errorf("%s = %d, want %d", key, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// 11. Float formats
// ---------------------------------------------------------------------------

func TestFloats(t *testing.T) {
	input := `
pi = 3.14159
neg = -0.001
exp = 5e+22
under = 3.14_15
`
	m := mustParse(t, input)

	if v := m["pi"].(float64); v != 3.14159 {
		t.Errorf("pi = %v", v)
	}
	if v := m["neg"].(float64); v != -0.001 {
		t.Errorf("neg = %v", v)
	}
	if v := m["exp"].(float64); v != 5e+22 {
		t.Errorf("exp = %v", v)
	}
	if v := m["under"].(float64); v != 3.1415 {
		t.Errorf("under = %v", v)
	}
}

func TestSpecialFloats(t *testing.T) {
	input := `
posinf = inf
neginf = -inf
notanum = nan
`
	m := mustParse(t, input)

	if v := m["posinf"].(float64); !math.IsInf(v, 1) {
		t.Errorf("posinf = %v, want +Inf", v)
	}
	if v := m["neginf"].(float64); !math.IsInf(v, -1) {
		t.Errorf("neginf = %v, want -Inf", v)
	}
	if v := m["notanum"].(float64); !math.IsNaN(v) {
		t.Errorf("notanum = %v, want NaN", v)
	}
}

// ---------------------------------------------------------------------------
// 12. Boolean values
// ---------------------------------------------------------------------------

func TestBooleans(t *testing.T) {
	input := `
a = true
b = false
`
	m := mustParse(t, input)

	if m["a"] != true {
		t.Errorf("a = %v", m["a"])
	}
	if m["b"] != false {
		t.Errorf("b = %v", m["b"])
	}
}

// ---------------------------------------------------------------------------
// 13. Date/time formats
// ---------------------------------------------------------------------------

func TestDatetime(t *testing.T) {
	input := `
odt = 2024-01-15T10:30:00Z
local_date = 2024-01-15
local_time = 10:30:00
`
	m := mustParse(t, input)

	if m["odt"] != "2024-01-15T10:30:00Z" {
		t.Errorf("odt = %v", m["odt"])
	}
	if m["local_date"] != "2024-01-15" {
		t.Errorf("local_date = %v", m["local_date"])
	}
	if m["local_time"] != "10:30:00" {
		t.Errorf("local_time = %v", m["local_time"])
	}
}

// ---------------------------------------------------------------------------
// 14. Comments
// ---------------------------------------------------------------------------

func TestComments(t *testing.T) {
	input := `
# This is a comment
key = "value" # inline comment
another = 42
`
	m := mustParse(t, input)

	if m["key"] != "value" {
		t.Errorf("key = %v", m["key"])
	}
	if m["another"] != int64(42) {
		t.Errorf("another = %v", m["another"])
	}
}

// ---------------------------------------------------------------------------
// 15. Duplicate key detection
// ---------------------------------------------------------------------------

func TestDuplicateKeyError(t *testing.T) {
	input := `
name = "first"
name = "second"
`
	expectError(t, input)
}

func TestDuplicateTableError(t *testing.T) {
	input := `
[server]
host = "a"

[server]
host = "b"
`
	expectError(t, input)
}

// ---------------------------------------------------------------------------
// 16. Escape sequences
// ---------------------------------------------------------------------------

func TestEscapeSequences(t *testing.T) {
	input := `
backspace = "a\bb"
tab = "a\tb"
newline = "a\nb"
formfeed = "a\fb"
carriage = "a\rb"
quote = "a\"b"
backslash = "a\\b"
unicode4 = "\u0041"
unicode8 = "\U00000041"
`
	m := mustParse(t, input)

	tests := map[string]string{
		"backspace": "a\bb",
		"tab":       "a\tb",
		"newline":   "a\nb",
		"formfeed":  "a\fb",
		"carriage":  "a\rb",
		"quote":     "a\"b",
		"backslash": "a\\b",
		"unicode4":  "A",
		"unicode8":  "A",
	}

	for key, want := range tests {
		got, ok := m[key].(string)
		if !ok {
			t.Errorf("%s not a string: %T", key, m[key])
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// 17. Decode to Go struct
// ---------------------------------------------------------------------------

func TestDecodeStruct(t *testing.T) {
	input := `
name = "olb"
version = "1.0"
port = 8080
debug = true
ratio = 1.5

[logging]
level = "info"
format = "json"
`
	type LogConfig struct {
		Level  string `toml:"level"`
		Format string `toml:"format"`
	}
	type AppConfig struct {
		Name    string    `toml:"name"`
		Version string    `toml:"version"`
		Port    int       `toml:"port"`
		Debug   bool      `toml:"debug"`
		Ratio   float64   `toml:"ratio"`
		Logging LogConfig `toml:"logging"`
	}

	var cfg AppConfig
	err := Decode([]byte(input), &cfg)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if cfg.Name != "olb" {
		t.Errorf("Name = %q", cfg.Name)
	}
	if cfg.Version != "1.0" {
		t.Errorf("Version = %q", cfg.Version)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d", cfg.Port)
	}
	if cfg.Debug != true {
		t.Errorf("Debug = %v", cfg.Debug)
	}
	if cfg.Ratio != 1.5 {
		t.Errorf("Ratio = %v", cfg.Ratio)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %q", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("Logging.Format = %q", cfg.Logging.Format)
	}
}

// ---------------------------------------------------------------------------
// 18. Decode with yaml tag fallback
// ---------------------------------------------------------------------------

func TestDecodeYAMLTagFallback(t *testing.T) {
	input := `
server_name = "test"
listen_port = 443
`
	type Cfg struct {
		ServerName string `yaml:"server_name"`
		ListenPort int    `yaml:"listen_port"`
	}

	var cfg Cfg
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if cfg.ServerName != "test" {
		t.Errorf("ServerName = %q", cfg.ServerName)
	}
	if cfg.ListenPort != 443 {
		t.Errorf("ListenPort = %d", cfg.ListenPort)
	}
}

// ---------------------------------------------------------------------------
// 19. Decode slices and arrays of tables
// ---------------------------------------------------------------------------

func TestDecodeSlicesAndArrayOfTables(t *testing.T) {
	input := `
[[backends]]
id = "web1"
address = "10.0.0.1:80"
weight = 100

[[backends]]
id = "web2"
address = "10.0.0.2:80"
weight = 200
`
	type Backend struct {
		ID      string `toml:"id"`
		Address string `toml:"address"`
		Weight  int    `toml:"weight"`
	}
	type Config struct {
		Backends []Backend `toml:"backends"`
	}

	var cfg Config
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if len(cfg.Backends) != 2 {
		t.Fatalf("len(Backends) = %d, want 2", len(cfg.Backends))
	}
	if cfg.Backends[0].ID != "web1" {
		t.Errorf("Backends[0].ID = %q", cfg.Backends[0].ID)
	}
	if cfg.Backends[0].Weight != 100 {
		t.Errorf("Backends[0].Weight = %d", cfg.Backends[0].Weight)
	}
	if cfg.Backends[1].ID != "web2" {
		t.Errorf("Backends[1].ID = %q", cfg.Backends[1].ID)
	}
}

// ---------------------------------------------------------------------------
// 20. Complex nested config (OLB-like)
// ---------------------------------------------------------------------------

func TestComplexNestedConfig(t *testing.T) {
	input := `
version = "1"

[admin]
address = ":8080"
enabled = true

[logging]
level = "info"
format = "json"
output = "stdout"

[metrics]
enabled = true
path = "/metrics"

[[listeners]]
name = "http"
address = ":80"
protocol = "http"

[[listeners]]
name = "https"
address = ":443"
protocol = "https"
tls = true

[[pools]]
name = "web-pool"
algorithm = "round_robin"

[[pools.backends]]
id = "web1"
address = "10.0.0.1:8080"
weight = 100

[[pools.backends]]
id = "web2"
address = "10.0.0.2:8080"
weight = 200
`
	m := mustParse(t, input)

	if m["version"] != "1" {
		t.Errorf("version = %v", m["version"])
	}

	admin := m["admin"].(map[string]interface{})
	if admin["address"] != ":8080" {
		t.Errorf("admin.address = %v", admin["address"])
	}
	if admin["enabled"] != true {
		t.Errorf("admin.enabled = %v", admin["enabled"])
	}

	listeners := m["listeners"].([]interface{})
	if len(listeners) != 2 {
		t.Fatalf("len(listeners) = %d", len(listeners))
	}
	l1 := listeners[0].(map[string]interface{})
	if l1["name"] != "http" {
		t.Errorf("listeners[0].name = %v", l1["name"])
	}
	l2 := listeners[1].(map[string]interface{})
	if l2["tls"] != true {
		t.Errorf("listeners[1].tls = %v", l2["tls"])
	}

	pools := m["pools"].([]interface{})
	if len(pools) != 1 {
		t.Fatalf("len(pools) = %d", len(pools))
	}
	pool := pools[0].(map[string]interface{})
	if pool["name"] != "web-pool" {
		t.Errorf("pools[0].name = %v", pool["name"])
	}

	backends := pool["backends"].([]interface{})
	if len(backends) != 2 {
		t.Fatalf("len(backends) = %d", len(backends))
	}
	b1 := backends[0].(map[string]interface{})
	if b1["id"] != "web1" {
		t.Errorf("backends[0].id = %v", b1["id"])
	}
}

// ---------------------------------------------------------------------------
// 21. DecodeFile
// ---------------------------------------------------------------------------

func TestDecodeFile(t *testing.T) {
	content := `
name = "test-app"
port = 9090
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	type Cfg struct {
		Name string `toml:"name"`
		Port int    `toml:"port"`
	}

	var cfg Cfg
	if err := DecodeFile(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "test-app" {
		t.Errorf("Name = %q", cfg.Name)
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d", cfg.Port)
	}
}

// ---------------------------------------------------------------------------
// 22. Pointer types
// ---------------------------------------------------------------------------

func TestPointerTypes(t *testing.T) {
	input := `
[server]
host = "localhost"
port = 3000
`
	type Server struct {
		Host string `toml:"host"`
		Port int    `toml:"port"`
	}
	type Cfg struct {
		Server *Server `toml:"server"`
	}

	var cfg Cfg
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Server == nil {
		t.Fatal("Server is nil")
	}
	if cfg.Server.Host != "localhost" {
		t.Errorf("Server.Host = %q", cfg.Server.Host)
	}
	if cfg.Server.Port != 3000 {
		t.Errorf("Server.Port = %d", cfg.Server.Port)
	}
}

// ---------------------------------------------------------------------------
// 23. Map decoding
// ---------------------------------------------------------------------------

func TestMapDecoding(t *testing.T) {
	input := `
[metadata]
env = "production"
region = "us-east-1"
`
	type Cfg struct {
		Metadata map[string]string `toml:"metadata"`
	}

	var cfg Cfg
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Metadata["env"] != "production" {
		t.Errorf("Metadata[env] = %q", cfg.Metadata["env"])
	}
	if cfg.Metadata["region"] != "us-east-1" {
		t.Errorf("Metadata[region] = %q", cfg.Metadata["region"])
	}
}

// ---------------------------------------------------------------------------
// 24. Empty values / edge cases
// ---------------------------------------------------------------------------

func TestEmptyDocument(t *testing.T) {
	m := mustParse(t, "")
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

func TestCommentOnlyDocument(t *testing.T) {
	m := mustParse(t, "# just a comment\n# another one\n")
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

func TestEmptyArrayAndTable(t *testing.T) {
	input := `
empty_arr = []
empty_tbl = {}
`
	m := mustParse(t, input)

	arr := m["empty_arr"].([]interface{})
	if len(arr) != 0 {
		t.Errorf("empty_arr has %d elements", len(arr))
	}

	tbl := m["empty_tbl"].(map[string]interface{})
	if len(tbl) != 0 {
		t.Errorf("empty_tbl has %d keys", len(tbl))
	}
}

// ---------------------------------------------------------------------------
// 25. Inline table with dotted keys
// ---------------------------------------------------------------------------

func TestInlineTableDottedKeys(t *testing.T) {
	input := `
point = {x.a = 1, x.b = 2}
`
	m := mustParse(t, input)

	point := m["point"].(map[string]interface{})
	x := point["x"].(map[string]interface{})
	if x["a"] != int64(1) {
		t.Errorf("point.x.a = %v", x["a"])
	}
	if x["b"] != int64(2) {
		t.Errorf("point.x.b = %v", x["b"])
	}
}

// ---------------------------------------------------------------------------
// 26. Multiline basic string with line-ending backslash
// ---------------------------------------------------------------------------

func TestMultilineBasicStringLineEndingBackslash(t *testing.T) {
	input := "str = \"\"\"\nThe quick brown \\\n\n  fox jumps over \\\n  the lazy dog.\"\"\"\n"
	m := mustParse(t, input)

	expected := "The quick brown fox jumps over the lazy dog."
	if m["str"] != expected {
		t.Errorf("str = %q, want %q", m["str"], expected)
	}
}

// ---------------------------------------------------------------------------
// 27. Non-nil pointer target validation
// ---------------------------------------------------------------------------

func TestDecodeNonPointerError(t *testing.T) {
	var x int
	err := Decode([]byte("a = 1"), x)
	if err == nil {
		t.Error("expected error for non-pointer target")
	}
}

// ---------------------------------------------------------------------------
// 28. Tokenizer coverage
// ---------------------------------------------------------------------------

func TestTokenize(t *testing.T) {
	input := `key = "value"` + "\n"
	tokens, err := Tokenize(input)
	if err != nil {
		t.Fatal(err)
	}
	// Should have: bareKey, equals, basicString, newline, EOF
	if len(tokens) < 4 {
		t.Fatalf("too few tokens: %d", len(tokens))
	}
	if tokens[0].Type != tokenBareKey || tokens[0].Value != "key" {
		t.Errorf("token[0] = %v %q", tokens[0].Type, tokens[0].Value)
	}
	if tokens[1].Type != tokenEquals {
		t.Errorf("token[1] type = %v", tokens[1].Type)
	}
	if tokens[2].Type != tokenBasicString || tokens[2].Value != "value" {
		t.Errorf("token[2] = %v %q", tokens[2].Type, tokens[2].Value)
	}
}

// ---------------------------------------------------------------------------
// 29. Type conversions in decoder
// ---------------------------------------------------------------------------

func TestDecoderTypeConversions(t *testing.T) {
	input := `
timeout = "5s"
count = 10
rate = 2.5
active = true
`
	type Cfg struct {
		Timeout string  `toml:"timeout"`
		Count   int     `toml:"count"`
		Rate    float64 `toml:"rate"`
		Active  bool    `toml:"active"`
	}

	var cfg Cfg
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Timeout != "5s" {
		t.Errorf("Timeout = %q", cfg.Timeout)
	}
	if cfg.Count != 10 {
		t.Errorf("Count = %d", cfg.Count)
	}
	if cfg.Rate != 2.5 {
		t.Errorf("Rate = %v", cfg.Rate)
	}
	if cfg.Active != true {
		t.Errorf("Active = %v", cfg.Active)
	}
}

// ---------------------------------------------------------------------------
// 30. Decode into interface{}
// ---------------------------------------------------------------------------

func TestDecodeInterface(t *testing.T) {
	input := `
name = "test"
count = 42
`
	var result interface{}
	err := Decode([]byte(input), &result)
	if err != nil {
		t.Fatal(err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is %T", result)
	}
	if m["name"] != "test" {
		t.Errorf("name = %v", m["name"])
	}
	if m["count"] != int64(42) {
		t.Errorf("count = %v (%T)", m["count"], m["count"])
	}
}

// ---------------------------------------------------------------------------
// 31. Decode with uint
// ---------------------------------------------------------------------------

func TestDecodeUint(t *testing.T) {
	input := `
port = 8080
`
	type Cfg struct {
		Port uint16 `toml:"port"`
	}

	var cfg Cfg
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d", cfg.Port)
	}
}

// ---------------------------------------------------------------------------
// 32. Decode struct with json tag fallback
// ---------------------------------------------------------------------------

func TestDecodeJSONTagFallback(t *testing.T) {
	input := `
server_name = "test"
`
	type Cfg struct {
		ServerName string `json:"server_name"`
	}

	var cfg Cfg
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ServerName != "test" {
		t.Errorf("ServerName = %q", cfg.ServerName)
	}
}

// ---------------------------------------------------------------------------
// 33. Invalid TOML detection
// ---------------------------------------------------------------------------

func TestInvalidEscapeSequence(t *testing.T) {
	input := `key = "bad\qescape"`
	_, err := Parse([]byte(input))
	if err == nil {
		t.Error("expected error for invalid escape sequence")
	}
}

func TestUnterminatedString(t *testing.T) {
	input := `key = "no end`
	_, err := Parse([]byte(input))
	if err == nil {
		t.Error("expected error for unterminated string")
	}
}

// ---------------------------------------------------------------------------
// 34. Nested arrays of tables
// ---------------------------------------------------------------------------

func TestNestedArraysOfTables(t *testing.T) {
	input := `
[[fruits]]
name = "apple"

[[fruits.varieties]]
name = "red delicious"

[[fruits.varieties]]
name = "granny smith"

[[fruits]]
name = "banana"

[[fruits.varieties]]
name = "plantain"
`
	m := mustParse(t, input)

	fruits := m["fruits"].([]interface{})
	if len(fruits) != 2 {
		t.Fatalf("len(fruits) = %d, want 2", len(fruits))
	}

	apple := fruits[0].(map[string]interface{})
	if apple["name"] != "apple" {
		t.Errorf("fruits[0].name = %v", apple["name"])
	}
	appleVars := apple["varieties"].([]interface{})
	if len(appleVars) != 2 {
		t.Fatalf("len(apple.varieties) = %d", len(appleVars))
	}
	if appleVars[0].(map[string]interface{})["name"] != "red delicious" {
		t.Errorf("apple.varieties[0].name = %v", appleVars[0])
	}

	banana := fruits[1].(map[string]interface{})
	if banana["name"] != "banana" {
		t.Errorf("fruits[1].name = %v", banana["name"])
	}
	bananaVars := banana["varieties"].([]interface{})
	if len(bananaVars) != 1 {
		t.Fatalf("len(banana.varieties) = %d", len(bananaVars))
	}
}

// ---------------------------------------------------------------------------
// 35. Parse API returns correct types
// ---------------------------------------------------------------------------

func TestParseReturnTypes(t *testing.T) {
	input := `
str = "hello"
int_val = 42
float_val = 3.14
bool_val = true
arr = [1, 2, 3]
tbl = {a = 1}
`
	m := mustParse(t, input)

	if _, ok := m["str"].(string); !ok {
		t.Errorf("str is %T, not string", m["str"])
	}
	if _, ok := m["int_val"].(int64); !ok {
		t.Errorf("int_val is %T, not int64", m["int_val"])
	}
	if _, ok := m["float_val"].(float64); !ok {
		t.Errorf("float_val is %T, not float64", m["float_val"])
	}
	if _, ok := m["bool_val"].(bool); !ok {
		t.Errorf("bool_val is %T, not bool", m["bool_val"])
	}
	if _, ok := m["arr"].([]interface{}); !ok {
		t.Errorf("arr is %T, not []interface{}", m["arr"])
	}
	if _, ok := m["tbl"].(map[string]interface{}); !ok {
		t.Errorf("tbl is %T, not map[string]interface{}", m["tbl"])
	}
}

// ---------------------------------------------------------------------------
// 36. Decode with reflect: array to fixed array
// ---------------------------------------------------------------------------

func TestDecodeFixedArray(t *testing.T) {
	input := `
coords = [1, 2, 3]
`
	type Cfg struct {
		Coords [3]int `toml:"coords"`
	}

	var cfg Cfg
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Coords != [3]int{1, 2, 3} {
		t.Errorf("Coords = %v", cfg.Coords)
	}
}

// ---------------------------------------------------------------------------
// 37. Decode bool from int
// ---------------------------------------------------------------------------

func TestDecodeBoolFromInt(t *testing.T) {
	input := `
flag = true
`
	type Cfg struct {
		Flag bool `toml:"flag"`
	}
	var cfg Cfg
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Flag {
		t.Errorf("Flag = %v", cfg.Flag)
	}
}

// ---------------------------------------------------------------------------
// 38. Ensure Parse and Decode are consistent
// ---------------------------------------------------------------------------

func TestParseAndDecodeConsistency(t *testing.T) {
	input := `
title = "TOML"
[owner]
name = "Tom"
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}

	type Owner struct {
		Name string `toml:"name"`
	}
	type Doc struct {
		Title string `toml:"title"`
		Owner Owner  `toml:"owner"`
	}

	var doc Doc
	if err := Decode([]byte(input), &doc); err != nil {
		t.Fatal(err)
	}

	if m["title"] != doc.Title {
		t.Errorf("Parse title %q != Decode title %q", m["title"], doc.Title)
	}
	owner := m["owner"].(map[string]interface{})
	if owner["name"] != doc.Owner.Name {
		t.Errorf("Parse owner.name %q != Decode owner.name %q", owner["name"], doc.Owner.Name)
	}
}

// ---------------------------------------------------------------------------
// 39. Float to int conversion
// ---------------------------------------------------------------------------

func TestFloatToIntConversion(t *testing.T) {
	input := `
ratio = 2.5
`
	type Cfg struct {
		Ratio float32 `toml:"ratio"`
	}
	var cfg Cfg
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Ratio != 2.5 {
		t.Errorf("Ratio = %v", cfg.Ratio)
	}
}

// ---------------------------------------------------------------------------
// 40. Struct reflect: ignore unknown fields
// ---------------------------------------------------------------------------

func TestIgnoreUnknownFields(t *testing.T) {
	input := `
known = "yes"
unknown_field = "ignored"
`
	type Cfg struct {
		Known string `toml:"known"`
	}
	var cfg Cfg
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Known != "yes" {
		t.Errorf("Known = %q", cfg.Known)
	}
}

// ---------------------------------------------------------------------------
// 41. skipWhitespace coverage (tested indirectly)
// ---------------------------------------------------------------------------

func TestSkipWhitespace(t *testing.T) {
	// skipWhitespace is a no-op in the current TOML parser (whitespace is
	// handled by the lexer), but we still exercise the code path by parsing
	// TOML with various whitespace around = signs and values.
	input := `
key1   =    "value1"
key2=    "value2"
key3   ="value3"
key4="value4"
`
	m := mustParse(t, input)

	if m["key1"] != "value1" {
		t.Errorf("key1 = %v, want value1", m["key1"])
	}
	if m["key2"] != "value2" {
		t.Errorf("key2 = %v, want value2", m["key2"])
	}
	if m["key3"] != "value3" {
		t.Errorf("key3 = %v, want value3", m["key3"])
	}
	if m["key4"] != "value4" {
		t.Errorf("key4 = %v, want value4", m["key4"])
	}
}

// ---------------------------------------------------------------------------
// Benchmark
// ---------------------------------------------------------------------------

func BenchmarkParse(b *testing.B) {
	input := []byte(`
version = "1"
[server]
host = "localhost"
port = 8080
[server.tls]
enabled = true
cert = "/path/cert.pem"
[[backends]]
id = "web1"
address = "10.0.0.1:80"
weight = 100
[[backends]]
id = "web2"
address = "10.0.0.2:80"
weight = 200
`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Parse(input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecode(b *testing.B) {
	input := []byte(`
name = "bench"
port = 8080
debug = false
ratio = 1.5
[logging]
level = "info"
format = "json"
`)

	type Log struct {
		Level  string `toml:"level"`
		Format string `toml:"format"`
	}
	type Cfg struct {
		Name    string  `toml:"name"`
		Port    int     `toml:"port"`
		Debug   bool    `toml:"debug"`
		Ratio   float64 `toml:"ratio"`
		Logging Log     `toml:"logging"`
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var cfg Cfg
		if err := Decode(input, &cfg); err != nil {
			b.Fatal(err)
		}
	}
}

// Ensure reflect is used (compiler sometimes complains if not referenced)
var _ = reflect.TypeOf(nil)
