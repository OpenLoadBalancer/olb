package yaml

import (
	"testing"
)

func printNode(t *testing.T, node *Node, indent int) {
	prefix := ""
	for i := 0; i < indent; i++ {
		prefix += "  "
	}

	t.Logf("%s%s key=%q value=%q", prefix, node.Type, node.Key, node.Value)

	for _, child := range node.Children {
		printNode(t, child, indent+1)
	}
}

func TestDebug_NestedList(t *testing.T) {
	input := `
routes:
  - path: /
    pool: backend
`

	node, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	printNode(t, node, 0)
}
