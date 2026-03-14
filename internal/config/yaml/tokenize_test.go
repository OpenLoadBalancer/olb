package yaml

import (
	"testing"
)

func TestDebug_Tokenize(t *testing.T) {
	input := `
routes:
  - path: /
    pool: backend
`

	tokens, err := Tokenize(input)
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}

	for i, tok := range tokens {
		t.Logf("%d: %s value=%q line=%d col=%d", i, tok.Type, tok.Value, tok.Line, tok.Col)
	}
}
