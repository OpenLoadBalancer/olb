package yaml

import (
	"testing"
)

func FuzzYAMLParse(f *testing.F) {
	f.Add("key: value\n")
	f.Add("list:\n  - item1\n  - item2\n")
	f.Add("nested:\n  inner:\n    deep: true\n")
	f.Add("")
	f.Add("num: 42\nfloat: 3.14\n")
	f.Add("str: \"quoted string\"\n")
	f.Add("str2: 'single quoted'\n")
	f.Add("- a\n- b\n- c\n")
	f.Add("bool1: true\nbool2: false\n")
	f.Add("null_val: null\n")

	f.Fuzz(func(t *testing.T, input string) {
		tokens, err := Tokenize(input)
		if err != nil {
			return // Tokenize errors are expected for malformed input
		}
		p := NewParser(tokens)
		_, parseErr := p.Parse()
		_ = parseErr // Parse errors are expected for malformed input
	})
}

func FuzzYAMLLexer(f *testing.F) {
	f.Add("key: value\n")
	f.Add("")
	f.Add("\"string with\\nescape\"")
	f.Add("'single quoted'")
	f.Add("# comment\nkey: val")
	f.Add("  indented: true")
	f.Add("---\n")
	f.Add("...\n")
	f.Add(":")
	f.Add("- - - nested")

	f.Fuzz(func(t *testing.T, input string) {
		l := NewLexer(input)
		for {
			tok := l.NextToken()
			if tok.Type == TokenEOF {
				break
			}
		}
	})
}
