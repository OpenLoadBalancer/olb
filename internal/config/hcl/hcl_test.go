package hcl

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test: Basic attributes (string, int, float, bool)
// ---------------------------------------------------------------------------

func TestParseBasicAttributes(t *testing.T) {
	input := `
name    = "myapp"
port    = 8080
rate    = 3.14
enabled = true
debug   = false
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if m["name"] != "myapp" {
		t.Errorf("name = %v, want %q", m["name"], "myapp")
	}
	if m["port"] != int64(8080) {
		t.Errorf("port = %v (%T), want 8080", m["port"], m["port"])
	}
	if m["rate"] != 3.14 {
		t.Errorf("rate = %v, want 3.14", m["rate"])
	}
	if m["enabled"] != true {
		t.Errorf("enabled = %v, want true", m["enabled"])
	}
	if m["debug"] != false {
		t.Errorf("debug = %v, want false", m["debug"])
	}
}

// ---------------------------------------------------------------------------
// Test: Simple block with body
// ---------------------------------------------------------------------------

func TestParseSimpleBlock(t *testing.T) {
	input := `
server {
  host = "localhost"
  port = 9090
}
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	servers, ok := m["server"].([]interface{})
	if !ok || len(servers) == 0 {
		t.Fatalf("server block not found or wrong type: %v", m["server"])
	}

	srv := servers[0].(map[string]interface{})
	if srv["host"] != "localhost" {
		t.Errorf("host = %v, want %q", srv["host"], "localhost")
	}
	if srv["port"] != int64(9090) {
		t.Errorf("port = %v, want 9090", srv["port"])
	}
}

// ---------------------------------------------------------------------------
// Test: Block with one label
// ---------------------------------------------------------------------------

func TestParseBlockWithLabel(t *testing.T) {
	input := `
resource "web" {
  instances = 3
}
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	resources, ok := m["resource"].([]interface{})
	if !ok || len(resources) == 0 {
		t.Fatalf("resource block not found")
	}

	res := resources[0].(map[string]interface{})
	labels := res["__labels__"].([]string)
	if len(labels) != 1 || labels[0] != "web" {
		t.Errorf("labels = %v, want [web]", labels)
	}
	if res["instances"] != int64(3) {
		t.Errorf("instances = %v, want 3", res["instances"])
	}
}

// ---------------------------------------------------------------------------
// Test: Block with multiple labels
// ---------------------------------------------------------------------------

func TestParseBlockWithMultipleLabels(t *testing.T) {
	input := `
resource "aws_instance" "web" {
  ami           = "ami-12345"
  instance_type = "t2.micro"
}
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	resources := m["resource"].([]interface{})
	res := resources[0].(map[string]interface{})
	labels := res["__labels__"].([]string)
	if len(labels) != 2 || labels[0] != "aws_instance" || labels[1] != "web" {
		t.Errorf("labels = %v, want [aws_instance web]", labels)
	}
	if res["ami"] != "ami-12345" {
		t.Errorf("ami = %v, want %q", res["ami"], "ami-12345")
	}
	if res["instance_type"] != "t2.micro" {
		t.Errorf("instance_type = %v, want %q", res["instance_type"], "t2.micro")
	}
}

// ---------------------------------------------------------------------------
// Test: Nested blocks
// ---------------------------------------------------------------------------

func TestParseNestedBlocks(t *testing.T) {
	input := `
server {
  listener {
    port = 443
    tls  = true
  }
}
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	servers := m["server"].([]interface{})
	srv := servers[0].(map[string]interface{})

	listeners := srv["listener"].([]interface{})
	lis := listeners[0].(map[string]interface{})
	if lis["port"] != int64(443) {
		t.Errorf("port = %v, want 443", lis["port"])
	}
	if lis["tls"] != true {
		t.Errorf("tls = %v, want true", lis["tls"])
	}
}

// ---------------------------------------------------------------------------
// Test: Lists
// ---------------------------------------------------------------------------

func TestParseLists(t *testing.T) {
	input := `
ports  = [80, 443, 8080]
hosts  = ["web1", "web2", "web3"]
mixed  = [1, "two", true]
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	ports := m["ports"].([]interface{})
	if len(ports) != 3 {
		t.Fatalf("ports has %d items, want 3", len(ports))
	}
	if ports[0] != int64(80) || ports[1] != int64(443) || ports[2] != int64(8080) {
		t.Errorf("ports = %v, want [80 443 8080]", ports)
	}

	hosts := m["hosts"].([]interface{})
	if len(hosts) != 3 {
		t.Fatalf("hosts has %d items, want 3", len(hosts))
	}
	if hosts[0] != "web1" || hosts[1] != "web2" || hosts[2] != "web3" {
		t.Errorf("hosts = %v", hosts)
	}

	mixed := m["mixed"].([]interface{})
	if mixed[0] != int64(1) || mixed[1] != "two" || mixed[2] != true {
		t.Errorf("mixed = %v", mixed)
	}
}

// ---------------------------------------------------------------------------
// Test: Inline objects
// ---------------------------------------------------------------------------

func TestParseObjects(t *testing.T) {
	input := `
tags = { env = "prod", region = "us-east-1" }
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	tags, ok := m["tags"].(map[string]interface{})
	if !ok {
		t.Fatalf("tags is not a map: %T", m["tags"])
	}
	if tags["env"] != "prod" {
		t.Errorf("env = %v, want %q", tags["env"], "prod")
	}
	if tags["region"] != "us-east-1" {
		t.Errorf("region = %v, want %q", tags["region"], "us-east-1")
	}
}

// ---------------------------------------------------------------------------
// Test: String interpolation
// ---------------------------------------------------------------------------

func TestParseStringInterpolation(t *testing.T) {
	os.Setenv("OLB_TEST_HOST", "10.0.0.1")
	defer os.Unsetenv("OLB_TEST_HOST")

	input := `
address = "${OLB_TEST_HOST}"
label   = "prefix-${OLB_TEST_HOST}-suffix"
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if m["address"] != "10.0.0.1" {
		t.Errorf("address = %v, want %q", m["address"], "10.0.0.1")
	}
	if m["label"] != "prefix-10.0.0.1-suffix" {
		t.Errorf("label = %v, want %q", m["label"], "prefix-10.0.0.1-suffix")
	}
}

// ---------------------------------------------------------------------------
// Test: Heredoc strings (<<EOF)
// ---------------------------------------------------------------------------

func TestParseHeredoc(t *testing.T) {
	input := `description = <<EOF
Hello
World
EOF
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	desc, ok := m["description"].(string)
	if !ok {
		t.Fatalf("description is not a string: %T", m["description"])
	}
	if desc != "Hello\nWorld" {
		t.Errorf("description = %q, want %q", desc, "Hello\nWorld")
	}
}

// ---------------------------------------------------------------------------
// Test: Indented heredoc strings (<<-EOF)
// ---------------------------------------------------------------------------

func TestParseIndentedHeredoc(t *testing.T) {
	input := `description = <<-EOF
    Hello
    World
    EOF
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	desc, ok := m["description"].(string)
	if !ok {
		t.Fatalf("description is not a string: %T", m["description"])
	}
	if desc != "Hello\nWorld" {
		t.Errorf("description = %q, want %q", desc, "Hello\nWorld")
	}
}

// ---------------------------------------------------------------------------
// Test: Comments (# and // and /* */)
// ---------------------------------------------------------------------------

func TestParseComments(t *testing.T) {
	input := `
# This is a hash comment
name = "test" # inline comment

// This is a double-slash comment
port = 8080

/* This is a
   multi-line comment */
enabled = true
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if m["name"] != "test" {
		t.Errorf("name = %v, want %q", m["name"], "test")
	}
	if m["port"] != int64(8080) {
		t.Errorf("port = %v, want 8080", m["port"])
	}
	if m["enabled"] != true {
		t.Errorf("enabled = %v, want true", m["enabled"])
	}
}

// ---------------------------------------------------------------------------
// Test: Quoted strings with escapes
// ---------------------------------------------------------------------------

func TestParseQuotedStringEscapes(t *testing.T) {
	input := `
path   = "C:\\Users\\test"
msg    = "Hello\nWorld"
quoted = "say \"hello\""
tab    = "col1\tcol2"
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if m["path"] != "C:\\Users\\test" {
		t.Errorf("path = %q, want %q", m["path"], "C:\\Users\\test")
	}
	if m["msg"] != "Hello\nWorld" {
		t.Errorf("msg = %q, want %q", m["msg"], "Hello\nWorld")
	}
	if m["quoted"] != "say \"hello\"" {
		t.Errorf("quoted = %q, want %q", m["quoted"], "say \"hello\"")
	}
	if m["tab"] != "col1\tcol2" {
		t.Errorf("tab = %q, want %q", m["tab"], "col1\tcol2")
	}
}

// ---------------------------------------------------------------------------
// Test: Unquoted identifiers as values
// ---------------------------------------------------------------------------

func TestParseUnquotedIdentifiers(t *testing.T) {
	input := `
algorithm = round_robin
mode      = http
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if m["algorithm"] != "round_robin" {
		t.Errorf("algorithm = %v, want %q", m["algorithm"], "round_robin")
	}
	if m["mode"] != "http" {
		t.Errorf("mode = %v, want %q", m["mode"], "http")
	}
}

// ---------------------------------------------------------------------------
// Test: Multiple blocks of same type
// ---------------------------------------------------------------------------

func TestParseMultipleBlocksSameType(t *testing.T) {
	input := `
backend "web1" {
  address = "10.0.0.1:8080"
}

backend "web2" {
  address = "10.0.0.2:8080"
}

backend "web3" {
  address = "10.0.0.3:8080"
}
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	backends, ok := m["backend"].([]interface{})
	if !ok {
		t.Fatalf("backend is not a slice: %T", m["backend"])
	}
	if len(backends) != 3 {
		t.Fatalf("got %d backends, want 3", len(backends))
	}

	b1 := backends[0].(map[string]interface{})
	b2 := backends[1].(map[string]interface{})
	b3 := backends[2].(map[string]interface{})

	if b1["address"] != "10.0.0.1:8080" {
		t.Errorf("b1 address = %v", b1["address"])
	}
	if b2["address"] != "10.0.0.2:8080" {
		t.Errorf("b2 address = %v", b2["address"])
	}
	if b3["address"] != "10.0.0.3:8080" {
		t.Errorf("b3 address = %v", b3["address"])
	}
}

// ---------------------------------------------------------------------------
// Test: Decoding to Go struct
// ---------------------------------------------------------------------------

func TestDecodeToStruct(t *testing.T) {
	input := `
version = "1"
name    = "test-lb"
port    = 8080
weight  = 1.5
enabled = true
`
	type AppConfig struct {
		Version string  `hcl:"version"`
		Name    string  `hcl:"name"`
		Port    int     `hcl:"port"`
		Weight  float64 `hcl:"weight"`
		Enabled bool    `hcl:"enabled"`
	}

	var cfg AppConfig
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if cfg.Version != "1" {
		t.Errorf("Version = %q, want %q", cfg.Version, "1")
	}
	if cfg.Name != "test-lb" {
		t.Errorf("Name = %q, want %q", cfg.Name, "test-lb")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.Weight != 1.5 {
		t.Errorf("Weight = %f, want 1.5", cfg.Weight)
	}
	if cfg.Enabled != true {
		t.Errorf("Enabled = %v, want true", cfg.Enabled)
	}
}

// ---------------------------------------------------------------------------
// Test: Complex nested config
// ---------------------------------------------------------------------------

func TestDecodeComplexNestedConfig(t *testing.T) {
	input := `
version = "1"

listener "http" {
  address  = ":80"
  protocol = "http"

  route {
    path = "/"
    pool = "web-pool"
  }
}

pool "web-pool" {
  algorithm = "round-robin"

  health_check {
    type     = "http"
    path     = "/health"
    interval = "10s"
    timeout  = "2s"
  }

  backend {
    id      = "web1"
    address = "10.0.0.1:8080"
    weight  = 1
  }

  backend {
    id      = "web2"
    address = "10.0.0.2:8080"
    weight  = 2
  }
}
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if m["version"] != "1" {
		t.Errorf("version = %v, want %q", m["version"], "1")
	}

	// Listener
	listeners := m["listener"].([]interface{})
	if len(listeners) != 1 {
		t.Fatalf("got %d listeners, want 1", len(listeners))
	}
	lis := listeners[0].(map[string]interface{})
	if lis["address"] != ":80" {
		t.Errorf("listener address = %v", lis["address"])
	}
	if lis["protocol"] != "http" {
		t.Errorf("listener protocol = %v", lis["protocol"])
	}

	// Route inside listener
	routes := lis["route"].([]interface{})
	route := routes[0].(map[string]interface{})
	if route["path"] != "/" {
		t.Errorf("route path = %v", route["path"])
	}
	if route["pool"] != "web-pool" {
		t.Errorf("route pool = %v", route["pool"])
	}

	// Pool
	pools := m["pool"].([]interface{})
	pool := pools[0].(map[string]interface{})
	if pool["algorithm"] != "round-robin" {
		t.Errorf("pool algorithm = %v", pool["algorithm"])
	}

	// Health check in pool
	hcs := pool["health_check"].([]interface{})
	hc := hcs[0].(map[string]interface{})
	if hc["type"] != "http" {
		t.Errorf("health_check type = %v", hc["type"])
	}
	if hc["interval"] != "10s" {
		t.Errorf("health_check interval = %v", hc["interval"])
	}

	// Backends in pool
	backends := pool["backend"].([]interface{})
	if len(backends) != 2 {
		t.Fatalf("got %d backends, want 2", len(backends))
	}
	b1 := backends[0].(map[string]interface{})
	if b1["id"] != "web1" {
		t.Errorf("backend 1 id = %v", b1["id"])
	}
	if b1["weight"] != int64(1) {
		t.Errorf("backend 1 weight = %v (%T)", b1["weight"], b1["weight"])
	}
}

// ---------------------------------------------------------------------------
// Test: Error on invalid syntax
// ---------------------------------------------------------------------------

func TestParseInvalidSyntax(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "missing closing brace",
			input: "server {\n  port = 80\n",
		},
		{
			name:  "unexpected closing brace",
			input: "port = 80\n}\n",
		},
		{
			name:  "missing value after equals",
			input: "port = \n}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.input))
			if err == nil {
				t.Errorf("expected error for input %q, got nil", tt.input)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test: Number formats (hex, octal, scientific)
// ---------------------------------------------------------------------------

func TestParseNumberFormats(t *testing.T) {
	input := `
dec  = 42
neg  = -17
hex  = 0xFF
oct  = 0o77
sci  = 1.5e3
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if m["dec"] != int64(42) {
		t.Errorf("dec = %v (%T), want 42", m["dec"], m["dec"])
	}
	if m["neg"] != int64(-17) {
		t.Errorf("neg = %v (%T), want -17", m["neg"], m["neg"])
	}
	if m["hex"] != int64(0xFF) {
		t.Errorf("hex = %v (%T), want 255", m["hex"], m["hex"])
	}
	if m["oct"] != int64(077) {
		t.Errorf("oct = %v (%T), want 63", m["oct"], m["oct"])
	}
	if m["sci"] != float64(1.5e3) {
		t.Errorf("sci = %v (%T), want 1500", m["sci"], m["sci"])
	}
}

// ---------------------------------------------------------------------------
// Test: Decode struct with yaml tag fallback
// ---------------------------------------------------------------------------

func TestDecodeYAMLTagFallback(t *testing.T) {
	input := `
server_name = "myserver"
max_conns   = 100
`
	type Config struct {
		ServerName string `yaml:"server_name"`
		MaxConns   int    `yaml:"max_conns"`
	}

	var cfg Config
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if cfg.ServerName != "myserver" {
		t.Errorf("ServerName = %q, want %q", cfg.ServerName, "myserver")
	}
	if cfg.MaxConns != 100 {
		t.Errorf("MaxConns = %d, want 100", cfg.MaxConns)
	}
}

// ---------------------------------------------------------------------------
// Test: Decode struct with duration field
// ---------------------------------------------------------------------------

func TestDecodeDuration(t *testing.T) {
	input := `
timeout  = "5s"
interval = "100ms"
`
	type Config struct {
		Timeout  time.Duration `hcl:"timeout"`
		Interval time.Duration `hcl:"interval"`
	}

	var cfg Config
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", cfg.Timeout)
	}
	if cfg.Interval != 100*time.Millisecond {
		t.Errorf("Interval = %v, want 100ms", cfg.Interval)
	}
}

// ---------------------------------------------------------------------------
// Test: Decode slices of strings
// ---------------------------------------------------------------------------

func TestDecodeSlice(t *testing.T) {
	input := `
domains = ["example.com", "test.com", "dev.local"]
`
	type Config struct {
		Domains []string `hcl:"domains"`
	}

	var cfg Config
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	expected := []string{"example.com", "test.com", "dev.local"}
	if !reflect.DeepEqual(cfg.Domains, expected) {
		t.Errorf("Domains = %v, want %v", cfg.Domains, expected)
	}
}

// ---------------------------------------------------------------------------
// Test: Decode nested struct via blocks
// ---------------------------------------------------------------------------

func TestDecodeNestedStructBlocks(t *testing.T) {
	input := `
version = "2"

admin {
  address = ":9090"
  enabled = true
}
`
	type Admin struct {
		Address string `hcl:"address"`
		Enabled bool   `hcl:"enabled"`
	}
	type Config struct {
		Version string        `hcl:"version"`
		Admin   []interface{} `hcl:"admin"`
	}

	var cfg Config
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if cfg.Version != "2" {
		t.Errorf("Version = %q, want %q", cfg.Version, "2")
	}
	if len(cfg.Admin) != 1 {
		t.Fatalf("Admin has %d items, want 1", len(cfg.Admin))
	}
}

// ---------------------------------------------------------------------------
// Test: Decode map[string]string
// ---------------------------------------------------------------------------

func TestDecodeMapField(t *testing.T) {
	input := `
labels = { env = "prod", team = "infra" }
`
	type Config struct {
		Labels map[string]interface{} `hcl:"labels"`
	}

	var cfg Config
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if cfg.Labels["env"] != "prod" {
		t.Errorf("Labels[env] = %v, want %q", cfg.Labels["env"], "prod")
	}
	if cfg.Labels["team"] != "infra" {
		t.Errorf("Labels[team] = %v, want %q", cfg.Labels["team"], "infra")
	}
}

// ---------------------------------------------------------------------------
// Test: Block without labels
// ---------------------------------------------------------------------------

func TestParseBlockWithoutLabels(t *testing.T) {
	input := `
logging {
  level  = "info"
  format = "json"
}
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	blocks := m["logging"].([]interface{})
	logging := blocks[0].(map[string]interface{})
	if logging["level"] != "info" {
		t.Errorf("level = %v, want %q", logging["level"], "info")
	}
	if logging["format"] != "json" {
		t.Errorf("format = %v, want %q", logging["format"], "json")
	}
	// No __labels__ key when no labels are present
	if _, hasLabels := logging["__labels__"]; hasLabels {
		t.Errorf("block without labels should not have __labels__")
	}
}

// ---------------------------------------------------------------------------
// Test: Tokenizer edge cases
// ---------------------------------------------------------------------------

func TestTokenizer(t *testing.T) {
	input := `name = "hello" # comment`
	tokens, err := Tokenize(input)
	if err != nil {
		t.Fatalf("tokenize error: %v", err)
	}

	// Should have: IDENT("name"), EQUALS, STRING("hello"), EOF
	// Comments are skipped.
	found := make([]TokenType, 0)
	for _, tok := range tokens {
		if tok.Type != TokenNewline {
			found = append(found, tok.Type)
		}
	}

	if len(found) < 4 {
		t.Fatalf("expected at least 4 tokens (ident, eq, str, eof), got %d: %v", len(found), found)
	}
	if found[0] != TokenIdent {
		t.Errorf("token 0 = %v, want IDENT", found[0])
	}
	if found[1] != TokenEquals {
		t.Errorf("token 1 = %v, want EQUALS", found[1])
	}
	if found[2] != TokenString {
		t.Errorf("token 2 = %v, want STRING", found[2])
	}
}

// ---------------------------------------------------------------------------
// Test: DecodeFile
// ---------------------------------------------------------------------------

func TestDecodeFile(t *testing.T) {
	// Create a temp file
	f, err := os.CreateTemp("", "olb-hcl-test-*.hcl")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(f.Name())

	content := `name = "from-file"
port = 3000
`
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()

	type Config struct {
		Name string `hcl:"name"`
		Port int    `hcl:"port"`
	}

	var cfg Config
	if err := DecodeFile(f.Name(), &cfg); err != nil {
		t.Fatalf("decode file error: %v", err)
	}

	if cfg.Name != "from-file" {
		t.Errorf("Name = %q, want %q", cfg.Name, "from-file")
	}
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want 3000", cfg.Port)
	}
}

// ---------------------------------------------------------------------------
// Test: Empty input
// ---------------------------------------------------------------------------

func TestParseEmptyInput(t *testing.T) {
	m, err := Parse([]byte(""))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

// ---------------------------------------------------------------------------
// Test: Multiline list
// ---------------------------------------------------------------------------

func TestParseMultilineList(t *testing.T) {
	input := `
ports = [
  80,
  443,
  8080,
]
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	ports := m["ports"].([]interface{})
	if len(ports) != 3 {
		t.Fatalf("ports has %d items, want 3", len(ports))
	}
	if ports[0] != int64(80) || ports[1] != int64(443) || ports[2] != int64(8080) {
		t.Errorf("ports = %v", ports)
	}
}

// ---------------------------------------------------------------------------
// Test: String type conversion (string→int, string→bool, string→float)
// ---------------------------------------------------------------------------

func TestDecodeTypeConversions(t *testing.T) {
	input := `
str_int   = "42"
str_bool  = "true"
str_float = "3.14"
`
	type Config struct {
		StrInt   int     `hcl:"str_int"`
		StrBool  bool    `hcl:"str_bool"`
		StrFloat float64 `hcl:"str_float"`
	}

	var cfg Config
	if err := Decode([]byte(input), &cfg); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if cfg.StrInt != 42 {
		t.Errorf("StrInt = %d, want 42", cfg.StrInt)
	}
	if cfg.StrBool != true {
		t.Errorf("StrBool = %v, want true", cfg.StrBool)
	}
	if cfg.StrFloat != 3.14 {
		t.Errorf("StrFloat = %f, want 3.14", cfg.StrFloat)
	}
}

// ---------------------------------------------------------------------------
// Test: Deeply nested blocks
// ---------------------------------------------------------------------------

func TestParseDeeplyNestedBlocks(t *testing.T) {
	input := `
level1 {
  level2 {
    level3 {
      value = "deep"
    }
  }
}
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	l1 := m["level1"].([]interface{})[0].(map[string]interface{})
	l2 := l1["level2"].([]interface{})[0].(map[string]interface{})
	l3 := l2["level3"].([]interface{})[0].(map[string]interface{})

	if l3["value"] != "deep" {
		t.Errorf("value = %v, want %q", l3["value"], "deep")
	}
}

// ---------------------------------------------------------------------------
// Test: Unresolved interpolation stays as-is
// ---------------------------------------------------------------------------

func TestInterpolationUnresolved(t *testing.T) {
	// Make sure the env var doesn't exist
	os.Unsetenv("OLB_NONEXISTENT_VAR_XYZ")

	input := `
ref = "${OLB_NONEXISTENT_VAR_XYZ}"
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Unresolved interpolation stays as ${...}
	if m["ref"] != "${OLB_NONEXISTENT_VAR_XYZ}" {
		t.Errorf("ref = %v, want %q", m["ref"], "${OLB_NONEXISTENT_VAR_XYZ}")
	}
}

// ---------------------------------------------------------------------------
// Test: Decode non-pointer target returns error
// ---------------------------------------------------------------------------

func TestDecodeNonPointerError(t *testing.T) {
	input := `name = "test"`
	var cfg struct{ Name string }
	err := Decode([]byte(input), cfg)
	if err == nil {
		t.Errorf("expected error for non-pointer target")
	}
}

// ---------------------------------------------------------------------------
// Test: Nil pointer target returns error
// ---------------------------------------------------------------------------

func TestDecodeNilPointerError(t *testing.T) {
	input := `name = "test"`
	var cfg *struct{ Name string }
	err := Decode([]byte(input), cfg)
	if err == nil {
		t.Errorf("expected error for nil pointer target")
	}
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Test: TokenType.String() coverage
// ---------------------------------------------------------------------------

func TestTokenTypeString(t *testing.T) {
	tests := []struct {
		tt   TokenType
		want string
	}{
		{TokenEOF, "EOF"},
		{TokenNewline, "NEWLINE"},
		{TokenIdent, "IDENT"},
		{TokenString, "STRING"},
		{TokenHeredoc, "HEREDOC"},
		{TokenNumber, "NUMBER"},
		{TokenBool, "BOOL"},
		{TokenEquals, "EQUALS"},
		{TokenLBrace, "LBRACE"},
		{TokenRBrace, "RBRACE"},
		{TokenLBracket, "LBRACKET"},
		{TokenRBracket, "RBRACKET"},
		{TokenComma, "COMMA"},
		{TokenDot, "DOT"},
		{TokenInterpolation, "INTERPOLATION"},
		{TokenComment, "COMMENT"},
		{TokenType(99), "TOKEN(99)"},
	}

	for _, tt := range tests {
		got := tt.tt.String()
		if got != tt.want {
			t.Errorf("TokenType(%d).String() = %q, want %q", tt.tt, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Token.String() coverage
// ---------------------------------------------------------------------------

func TestTokenString(t *testing.T) {
	tok := Token{
		Type:  TokenIdent,
		Value: "name",
		Line:  1,
		Col:   5,
	}

	s := tok.String()
	if s == "" {
		t.Error("Token.String() should not be empty")
	}
	// Should contain the type, value, and position
	if !strings.Contains(s, "IDENT") {
		t.Errorf("Token.String() = %q, should contain IDENT", s)
	}
	if !strings.Contains(s, "name") {
		t.Errorf("Token.String() = %q, should contain name", s)
	}
}

// ---------------------------------------------------------------------------
// Test: Lexer.peekAt (tested indirectly through parsing)
// ---------------------------------------------------------------------------

func TestLexerPeekAt(t *testing.T) {
	// peekAt is used internally by the lexer for multi-character lookahead
	// (e.g., detecting <<EOF heredocs, //, /* comments).
	// We test it indirectly by parsing content that requires multi-char lookahead.
	input := `
description = <<EOF
line1
line2
EOF
comment_test = "after_heredoc"
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if m["description"] != "line1\nline2" {
		t.Errorf("description = %q, want %q", m["description"], "line1\nline2")
	}
	if m["comment_test"] != "after_heredoc" {
		t.Errorf("comment_test = %v, want %q", m["comment_test"], "after_heredoc")
	}

	// Also test with // comments to exercise peekAt for / followed by /
	input2 := `
// double slash comment
value = 42
`
	m2, err := Parse([]byte(input2))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if m2["value"] != int64(42) {
		t.Errorf("value = %v, want 42", m2["value"])
	}
}

// ---------------------------------------------------------------------------
// Test: NodeType.String() coverage
// ---------------------------------------------------------------------------

func TestNodeTypeString(t *testing.T) {
	tests := []struct {
		nt   NodeType
		want string
	}{
		{NodeBody, "BODY"},
		{NodeAttribute, "ATTRIBUTE"},
		{NodeBlock, "BLOCK"},
		{NodeLiteral, "LITERAL"},
		{NodeList, "LIST"},
		{NodeObject, "OBJECT"},
		{NodeType(99), "NODE(99)"},
	}

	for _, tt := range tests {
		got := tt.nt.String()
		if got != tt.want {
			t.Errorf("NodeType(%d).String() = %q, want %q", tt.nt, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Parser.peek and Parser.peekN coverage
// ---------------------------------------------------------------------------

func TestParserPeekAndPeekN(t *testing.T) {
	// Test peek and peekN indirectly through parsing an attribute
	// which requires the parser to look ahead to determine if an
	// identifier is a key (followed by =) or a block type.
	input := `
block_label "label1" "label2" {
  key = "value"
}

simple_key = 123
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// The parser uses peek/peekN to distinguish blocks from attributes.
	if m["simple_key"] != int64(123) {
		t.Errorf("simple_key = %v, want 123", m["simple_key"])
	}

	blocks := m["block_label"].([]interface{})
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	block := blocks[0].(map[string]interface{})
	labels := block["__labels__"].([]string)
	if len(labels) != 2 || labels[0] != "label1" || labels[1] != "label2" {
		t.Errorf("labels = %v, want [label1 label2]", labels)
	}
}

// TestHCL_PeekFunctions tests peek/peekAt/peekN indirectly through parsing
// that requires lookahead (block with labels and a list value).
func TestHCL_PeekFunctions(t *testing.T) {
	input := `
resource "aws" "main" {
  count = 3
  tags = ["a", "b"]
}
`
	result, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
}

func BenchmarkParse(b *testing.B) {
	input := []byte(`
version = "1"

listener "http" {
  address  = ":80"
  protocol = "http"

  route {
    path = "/"
    pool = "web-pool"
  }
}

pool "web-pool" {
  algorithm = "round-robin"

  backend {
    id      = "web1"
    address = "10.0.0.1:8080"
    weight  = 1
  }
}
`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Parse(input)
	}
}

func BenchmarkTokenize(b *testing.B) {
	input := `name = "hello" port = 8080 enabled = true tags = ["a", "b", "c"]`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Tokenize(input)
	}
}
