package hcl

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// AST
// ---------------------------------------------------------------------------

// NodeType represents an HCL AST node type.
type NodeType int

const (
	NodeBody      NodeType = iota // top-level body or block body
	NodeAttribute                 // key = value
	NodeBlock                     // blocktype "label" { ... }
	NodeLiteral                   // string, number, bool, heredoc
	NodeList                      // [ ... ]
	NodeObject                    // { key = val, ... } (inline object)
)

// String returns the node type name.
func (t NodeType) String() string {
	names := []string{"BODY", "ATTRIBUTE", "BLOCK", "LITERAL", "LIST", "OBJECT"}
	if int(t) < len(names) {
		return names[t]
	}
	return fmt.Sprintf("NODE(%d)", t)
}

// Node is an HCL AST node.
type Node struct {
	Type     NodeType
	Key      string   // attribute key or block type
	Value    string   // for literals
	Quoted   bool     // true if value came from a quoted string or heredoc
	Labels   []string // for blocks
	Children []*Node  // body items, list items, object entries
}

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

// Parser parses HCL tokens into an AST.
type Parser struct {
	tokens []Token
	pos    int
}

// NewParser creates a new HCL parser.
func NewParser(tokens []Token) *Parser {
	return &Parser{tokens: tokens}
}

// current returns the current token.
func (p *Parser) current() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos]
}

// peek returns the next token without consuming.
func (p *Parser) peek() Token {
	if p.pos+1 >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos+1]
}

// peekN returns the token N positions ahead.
func (p *Parser) peekN(n int) Token {
	idx := p.pos + n
	if idx >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[idx]
}

// advance moves to the next token.
func (p *Parser) advance() {
	if p.pos < len(p.tokens) {
		p.pos++
	}
}

// skipNewlines skips consecutive newline tokens.
func (p *Parser) skipNewlines() {
	for p.current().Type == TokenNewline {
		p.advance()
	}
}

// Parse parses the tokens into an AST body node.
func (p *Parser) Parse() (*Node, error) {
	return p.parseBody(false)
}

// parseBody parses a body (sequence of attributes and blocks).
// If inBlock is true, we stop at a closing brace.
func (p *Parser) parseBody(inBlock bool) (*Node, error) {
	body := &Node{Type: NodeBody}

	for {
		p.skipNewlines()

		tok := p.current()

		if tok.Type == TokenEOF {
			if inBlock {
				return nil, fmt.Errorf("unexpected EOF, expected '}' at %d:%d", tok.Line, tok.Col)
			}
			break
		}

		if tok.Type == TokenRBrace {
			if inBlock {
				break // the caller will consume '}'
			}
			return nil, fmt.Errorf("unexpected '}' at %d:%d", tok.Line, tok.Col)
		}

		// Each body item starts with an identifier
		if tok.Type != TokenIdent {
			return nil, fmt.Errorf("expected identifier, got %s (%q) at %d:%d", tok.Type, tok.Value, tok.Line, tok.Col)
		}

		item, err := p.parseBodyItem()
		if err != nil {
			return nil, err
		}
		body.Children = append(body.Children, item)
	}

	return body, nil
}

// parseBodyItem parses a single attribute or block.
func (p *Parser) parseBodyItem() (*Node, error) {
	ident := p.current()
	p.advance() // consume identifier

	p.skipNewlines()

	// Decide: attribute (ident = value) or block (ident [labels...] { ... })
	switch p.current().Type {
	case TokenEquals:
		return p.parseAttribute(ident.Value)
	default:
		return p.parseBlock(ident.Value)
	}
}

// parseAttribute parses: key = value
func (p *Parser) parseAttribute(key string) (*Node, error) {
	p.advance() // consume '='

	p.skipNewlines()

	val, err := p.parseExpression()
	if err != nil {
		return nil, fmt.Errorf("attribute %q: %w", key, err)
	}

	return &Node{
		Type:     NodeAttribute,
		Key:      key,
		Children: []*Node{val},
	}, nil
}

// parseBlock parses: blocktype "label1" "label2" { body }
func (p *Parser) parseBlock(blockType string) (*Node, error) {
	node := &Node{
		Type: NodeBlock,
		Key:  blockType,
	}

	// Collect labels (strings or identifiers before '{')
	for {
		p.skipNewlines()
		tok := p.current()
		if tok.Type == TokenLBrace {
			break
		}
		if tok.Type == TokenString || tok.Type == TokenIdent {
			node.Labels = append(node.Labels, tok.Value)
			p.advance()
			continue
		}
		return nil, fmt.Errorf("expected block label or '{', got %s (%q) at %d:%d", tok.Type, tok.Value, tok.Line, tok.Col)
	}

	// Consume '{'
	p.advance()

	// Parse block body
	body, err := p.parseBody(true)
	if err != nil {
		return nil, fmt.Errorf("block %q: %w", blockType, err)
	}
	node.Children = body.Children

	// Consume '}'
	p.skipNewlines()
	if p.current().Type != TokenRBrace {
		return nil, fmt.Errorf("expected '}' to close block %q, got %s at %d:%d", blockType, p.current().Type, p.current().Line, p.current().Col)
	}
	p.advance()

	return node, nil
}

// parseExpression parses a value expression.
func (p *Parser) parseExpression() (*Node, error) {
	tok := p.current()

	switch tok.Type {
	case TokenString, TokenHeredoc:
		p.advance()
		return &Node{Type: NodeLiteral, Value: tok.Value, Quoted: true}, nil

	case TokenNumber:
		p.advance()
		return &Node{Type: NodeLiteral, Value: tok.Value}, nil

	case TokenBool:
		p.advance()
		return &Node{Type: NodeLiteral, Value: tok.Value}, nil

	case TokenIdent:
		// Unquoted identifier used as value
		p.advance()
		return &Node{Type: NodeLiteral, Value: tok.Value}, nil

	case TokenLBracket:
		return p.parseList()

	case TokenLBrace:
		return p.parseObject()

	default:
		return nil, fmt.Errorf("expected expression, got %s (%q) at %d:%d", tok.Type, tok.Value, tok.Line, tok.Col)
	}
}

// parseList parses: [val1, val2, val3]
func (p *Parser) parseList() (*Node, error) {
	node := &Node{Type: NodeList}
	p.advance() // consume '['

	for {
		p.skipNewlines()

		if p.current().Type == TokenRBracket {
			p.advance()
			return node, nil
		}

		item, err := p.parseExpression()
		if err != nil {
			return nil, fmt.Errorf("list item: %w", err)
		}
		node.Children = append(node.Children, item)

		p.skipNewlines()

		// Optional comma
		if p.current().Type == TokenComma {
			p.advance()
		}
	}
}

// parseObject parses inline object: { key = val, key2 = val2 }
func (p *Parser) parseObject() (*Node, error) {
	node := &Node{Type: NodeObject}
	p.advance() // consume '{'

	for {
		p.skipNewlines()

		if p.current().Type == TokenRBrace {
			p.advance()
			return node, nil
		}

		// Expect key
		keyTok := p.current()
		if keyTok.Type != TokenIdent && keyTok.Type != TokenString {
			return nil, fmt.Errorf("expected key in object, got %s (%q) at %d:%d", keyTok.Type, keyTok.Value, keyTok.Line, keyTok.Col)
		}
		p.advance()

		p.skipNewlines()

		// Expect '='
		if p.current().Type != TokenEquals {
			return nil, fmt.Errorf("expected '=' after key %q in object, got %s at %d:%d", keyTok.Value, p.current().Type, p.current().Line, p.current().Col)
		}
		p.advance()

		p.skipNewlines()

		// Parse value
		val, err := p.parseExpression()
		if err != nil {
			return nil, fmt.Errorf("object key %q: %w", keyTok.Value, err)
		}

		node.Children = append(node.Children, &Node{
			Type:     NodeAttribute,
			Key:      keyTok.Value,
			Children: []*Node{val},
		})

		p.skipNewlines()

		// Optional comma
		if p.current().Type == TokenComma {
			p.advance()
		}
	}
}

// ---------------------------------------------------------------------------
// Interpreter: AST → map[string]any
// ---------------------------------------------------------------------------

// interpret converts a body AST node into map[string]any.
func interpret(node *Node) (map[string]any, error) {
	result := make(map[string]any)

	for _, child := range node.Children {
		switch child.Type {
		case NodeAttribute:
			val := evalNode(child.Children[0])
			// Perform string interpolation on string values
			if s, ok := val.(string); ok {
				val = interpolate(s)
			}
			result[child.Key] = val

		case NodeBlock:
			block := interpretBlock(child)
			key := child.Key

			// Blocks of the same type are collected into a slice.
			if existing, ok := result[key]; ok {
				if arr, ok := existing.([]any); ok {
					result[key] = append(arr, block)
				} else {
					result[key] = []any{existing, block}
				}
			} else {
				result[key] = []any{block}
			}

		default:
			// Ignore unexpected node types at body level.
		}
	}

	return result, nil
}

// interpretBlock converts a block node into map[string]any.
func interpretBlock(node *Node) map[string]any {
	m := make(map[string]any)

	// Attach labels
	if len(node.Labels) > 0 {
		m["__labels__"] = node.Labels
	}

	// Interpret child body items (attributes and nested blocks)
	for _, child := range node.Children {
		switch child.Type {
		case NodeAttribute:
			val := evalNode(child.Children[0])
			if s, ok := val.(string); ok {
				val = interpolate(s)
			}
			m[child.Key] = val

		case NodeBlock:
			block := interpretBlock(child)
			key := child.Key
			if existing, ok := m[key]; ok {
				if arr, ok := existing.([]any); ok {
					m[key] = append(arr, block)
				} else {
					m[key] = []any{existing, block}
				}
			} else {
				m[key] = []any{block}
			}
		}
	}

	return m
}

// evalNode evaluates an expression node to a Go value.
func evalNode(node *Node) any {
	switch node.Type {
	case NodeLiteral:
		if node.Quoted {
			// Quoted strings stay as strings — no type coercion.
			return node.Value
		}
		return parseLiteralValue(node.Value)

	case NodeList:
		items := make([]any, 0, len(node.Children))
		for _, child := range node.Children {
			items = append(items, evalNode(child))
		}
		return items

	case NodeObject:
		m := make(map[string]any)
		for _, child := range node.Children {
			if child.Type == NodeAttribute && len(child.Children) > 0 {
				m[child.Key] = evalNode(child.Children[0])
			}
		}
		return m

	default:
		return node.Value
	}
}

// parseLiteralValue converts a literal string to a typed Go value.
func parseLiteralValue(s string) any {
	// Booleans
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}

	// Hex integer
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		if i, err := strconv.ParseInt(s, 0, 64); err == nil {
			return i
		}
	}

	// Octal integer
	if strings.HasPrefix(s, "0o") || strings.HasPrefix(s, "0O") {
		cleaned := strings.Replace(strings.Replace(s, "0o", "0", 1), "0O", "0", 1)
		if i, err := strconv.ParseInt(cleaned, 0, 64); err == nil {
			return i
		}
	}

	// Integer
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}

	// Float
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}

	// Otherwise it's a string
	return s
}

// interpolate processes ${...} in a string value.
// Supports ${ENV_VAR} as environment variable lookup.
func interpolate(s string) any {
	if !strings.Contains(s, "${") {
		return s
	}

	var result strings.Builder
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '$' && s[i+1] == '{' {
			// Find closing brace
			end := strings.Index(s[i:], "}")
			if end < 0 {
				result.WriteByte(s[i])
				i++
				continue
			}
			expr := s[i+2 : i+end]
			resolved := resolveInterpolation(expr)
			result.WriteString(resolved)
			i = i + end + 1
		} else {
			result.WriteByte(s[i])
			i++
		}
	}

	return result.String()
}

// resolveInterpolation resolves a single interpolation expression.
// Supports:
//   - var.name → looks up "var.name" literally (returned as-is for now)
//   - simple names → environment variable lookup
func resolveInterpolation(expr string) string {
	expr = strings.TrimSpace(expr)

	// Try environment variable
	if val := os.Getenv(expr); val != "" {
		return val
	}

	// Return the expression unchanged (variable references are resolved at decode time)
	return "${" + expr + "}"
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// Parse parses HCL data and returns a generic map[string]any.
func Parse(data []byte) (map[string]any, error) {
	tokens, err := Tokenize(string(data))
	if err != nil {
		return nil, fmt.Errorf("tokenize: %w", err)
	}

	parser := NewParser(tokens)
	ast, err := parser.Parse()
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	return interpret(ast)
}

// Decode parses HCL data and decodes it into the value pointed to by v.
func Decode(data []byte, v any) error {
	m, err := Parse(data)
	if err != nil {
		return err
	}

	return decodeMap(m, v)
}

// DecodeFile reads an HCL file and decodes it into v.
func DecodeFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	return Decode(data, v)
}
