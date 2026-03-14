package yaml

import (
	"testing"
)

func TestLexer_Tokenize(t *testing.T) {
	input := `
name: test
value: 42
enabled: true
`

	tokens, err := Tokenize(input)
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	// Should have at least: indent, string, colon, string, newline, ...
	if len(tokens) < 5 {
		t.Fatalf("Expected at least 5 tokens, got %d", len(tokens))
	}

	// Check for expected tokens
	foundString := false
	foundNumber := false
	foundBool := false

	for _, tok := range tokens {
		switch tok.Type {
		case TokenString:
			foundString = true
		case TokenNumber:
			foundNumber = true
		case TokenBool:
			foundBool = true
		}
	}

	if !foundString {
		t.Error("No string token found")
	}
	if !foundNumber {
		t.Error("No number token found")
	}
	if !foundBool {
		t.Error("No bool token found")
	}
}

func TestLexer_SimpleMapping(t *testing.T) {
	input := "key: value"

	tokens, err := Tokenize(input)
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	// Filter out EOF
	var relevant []Token
	for _, tok := range tokens {
		if tok.Type != TokenEOF {
			relevant = append(relevant, tok)
		}
	}

	if len(relevant) < 3 {
		t.Fatalf("Expected at least 3 tokens, got %d: %v", len(relevant), relevant)
	}
}

func TestLexer_List(t *testing.T) {
	input := `
items:
  - one
  - two
  - three
`

	tokens, err := Tokenize(input)
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	// Look for dash tokens
	dashCount := 0
	for _, tok := range tokens {
		if tok.Type == TokenDash {
			dashCount++
		}
	}

	if dashCount != 3 {
		t.Errorf("Expected 3 dash tokens, got %d", dashCount)
	}
}

func TestLexer_QuotedString(t *testing.T) {
	input := `key: "quoted value"`

	tokens, err := Tokenize(input)
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	// Find string token with quoted value
	var found bool
	for _, tok := range tokens {
		if tok.Type == TokenString && tok.Value == "quoted value" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Quoted string not tokenized correctly")
	}
}

func TestLexer_Numbers(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"value: 42", "42"},
		{"value: -123", "-123"},
		{"value: 3.14", "3.14"},
		{"value: -1.5e10", "-1.5e10"},
	}

	for _, tt := range tests {
		tokens, err := Tokenize(tt.input)
		if err != nil {
			t.Fatalf("Tokenize(%s) failed: %v", tt.input, err)
		}

		var found bool
		for _, tok := range tokens {
			if tok.Type == TokenNumber && tok.Value == tt.expected {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("Expected number %q not found in %s", tt.expected, tt.input)
		}
	}
}

func TestParser_SimpleMapping(t *testing.T) {
	input := "key: value"

	node, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if node.Type != NodeDocument {
		t.Errorf("Expected NodeDocument, got %v", node.Type)
	}

	if len(node.Children) == 0 {
		t.Fatal("Document has no children")
	}
}

func TestParser_List(t *testing.T) {
	input := `
items:
  - one
  - two
  - three
`

	node, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if node.Type != NodeDocument {
		t.Errorf("Expected NodeDocument, got %v", node.Type)
	}
}

func TestParser_NestedMapping(t *testing.T) {
	input := `
server:
  host: localhost
  port: 8080
`

	node, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if node.Type != NodeDocument {
		t.Errorf("Expected NodeDocument, got %v", node.Type)
	}
}

func TestDecoder_Simple(t *testing.T) {
	type Config struct {
		Name  string `yaml:"name"`
		Value int    `yaml:"value"`
	}

	input := "name: test\nvalue: 42"

	node, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	decoder := NewDecoder(node)

	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if cfg.Name != "test" {
		t.Errorf("Name = %q, want %q", cfg.Name, "test")
	}

	if cfg.Value != 42 {
		t.Errorf("Value = %d, want %d", cfg.Value, 42)
	}
}

func TestDecoder_List(t *testing.T) {
	type Config struct {
		Items []string `yaml:"items"`
	}

	input := `
items:
  - one
  - two
  - three
`

	var cfg Config
	if err := UnmarshalString(input, &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(cfg.Items) != 3 {
		t.Errorf("len(Items) = %d, want 3", len(cfg.Items))
	}
}

func TestDecoder_Nested(t *testing.T) {
	type Server struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	}

	type Config struct {
		Server Server `yaml:"server"`
	}

	input := `
server:
  host: localhost
  port: 8080
`

	var cfg Config
	if err := UnmarshalString(input, &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.Server.Host != "localhost" {
		t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "localhost")
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 8080)
	}
}

func TestDecoder_Types(t *testing.T) {
	type Config struct {
		String  string  `yaml:"string"`
		Int     int     `yaml:"int"`
		Float   float64 `yaml:"float"`
		Bool    bool    `yaml:"bool"`
	}

	input := `
string: hello
int: 123
float: 3.14
bool: true
`

	var cfg Config
	if err := UnmarshalString(input, &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cfg.String != "hello" {
		t.Errorf("String = %q, want %q", cfg.String, "hello")
	}
	if cfg.Int != 123 {
		t.Errorf("Int = %d, want %d", cfg.Int, 123)
	}
	if cfg.Float != 3.14 {
		t.Errorf("Float = %f, want %f", cfg.Float, 3.14)
	}
	if !cfg.Bool {
		t.Errorf("Bool = %v, want true", cfg.Bool)
	}
}
