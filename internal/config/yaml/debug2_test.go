package yaml

import (
	"testing"
)

func TestDebug_FullConfig(t *testing.T) {
	input := `
version: "1"
listeners:
  - name: http
    address: ":80"

pools:
  - name: backend
    backends:
      - address: "10.0.1.10:8080"
`

	node, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	printNode(t, node, 0)
}
