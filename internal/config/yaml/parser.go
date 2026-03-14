package yaml

import (
	"fmt"
	"strings"
)

// NodeType represents an AST node type.
type NodeType int

const (
	NodeDocument NodeType = iota
	NodeMapping
	NodeSequence
	NodeScalar
	NodeAlias
)

// String returns the node type name.
func (t NodeType) String() string {
	names := []string{"DOCUMENT", "MAPPING", "SEQUENCE", "SCALAR", "ALIAS"}
	if int(t) < len(names) {
		return names[t]
	}
	return fmt.Sprintf("NODE(%d)", t)
}

// Node is an AST node.
type Node struct {
	Type     NodeType
	Value    string      // For scalars
	Tag      string      // Type tag (e.g., !!str)
	Anchor   string      // Anchor name
	Children []*Node     // For mappings and sequences
	Key      string      // For mapping entries
}

// Parser parses YAML tokens into an AST.
type Parser struct {
	tokens []Token
	pos    int
}

// NewParser creates a new YAML parser.
func NewParser(tokens []Token) *Parser {
	return &Parser{tokens: tokens}
}

// Parse parses the tokens into an AST.
func (p *Parser) Parse() (*Node, error) {
	return p.parseDocument()
}

// current returns the current token.
func (p *Parser) current() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos]
}

// peek returns the next token.
func (p *Parser) peek() Token {
	if p.pos+1 >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos+1]
}

// advance moves to the next token.
func (p *Parser) advance() {
	if p.pos < len(p.tokens) {
		p.pos++
	}
}

// skip skips specific token types.
func (p *Parser) skip(types ...TokenType) {
	for _, t := range types {
		if p.current().Type == t {
			p.advance()
		}
	}
}

// expect checks that the current token is of the expected type.
func (p *Parser) expect(t TokenType) (Token, error) {
	curr := p.current()
	if curr.Type != t {
		return Token{}, fmt.Errorf("expected %s, got %s at %d:%d", t, curr.Type, curr.Line, curr.Col)
	}
	p.advance()
	return curr, nil
}

// parseDocument parses the root document.
func (p *Parser) parseDocument() (*Node, error) {
	doc := &Node{Type: NodeDocument}

	// Skip leading newlines, comments, and indents
	for {
		tok := p.current()
		if tok.Type == TokenNewline || tok.Type == TokenComment || tok.Type == TokenIndent {
			p.advance()
			continue
		}
		break
	}

	// Parse the document content
	node, err := p.parseValue(0)
	if err != nil {
		return nil, err
	}

	if node != nil {
		doc.Children = append(doc.Children, node)

		// If it's a mapping, continue parsing more top-level entries
		if node.Type == NodeMapping {
			for {
				// Skip newlines, comments, indents, and dedents
				for p.current().Type == TokenNewline || p.current().Type == TokenComment ||
					p.current().Type == TokenIndent || p.current().Type == TokenDedent {
					p.advance()
				}

				// Stop at EOF
				if p.current().Type == TokenEOF {
					break
				}

				// Try to parse another key-value pair
				if p.current().Type == TokenString && p.peek().Type == TokenColon {
					nextNode, err := p.parseValue(0)
					if err != nil {
						return nil, err
					}
					if nextNode != nil && nextNode.Type == NodeMapping {
						// Merge the children into the existing mapping
						node.Children = append(node.Children, nextNode.Children...)
					}
				} else {
					break
				}
			}
		}
	}

	return doc, nil
}

// parseValue parses a value (scalar, sequence, or mapping).
func (p *Parser) parseValue(minIndent int) (*Node, error) {
	tok := p.current()

	switch tok.Type {
	case TokenEOF, TokenDedent:
		return nil, nil

	case TokenDash:
		// Sequence item
		return p.parseSequence(minIndent)

	case TokenLBracket:
		// Inline sequence
		return p.parseInlineSequence()

	case TokenLBrace:
		// Inline mapping
		return p.parseInlineMapping()

	case TokenString, TokenNumber, TokenBool, TokenNull:
		// Could be scalar or start of mapping
		return p.parseScalarOrMapping(minIndent)

	case TokenPipe, TokenGreater:
		// Multi-line string
		return p.parseMultiLineString()

	case TokenAlias:
		p.advance()
		return &Node{Type: NodeAlias, Value: tok.Value}, nil

	case TokenAnchor:
		return p.parseAnchoredValue()

	case TokenIndent:
		// Indented block
		p.advance()
		return p.parseValue(minIndent)

	default:
		// Try to skip and continue
		p.advance()
		return p.parseValue(minIndent)
	}
}

// parseSequence parses a sequence (list).
func (p *Parser) parseSequence(minIndent int) (*Node, error) {
	seq := &Node{Type: NodeSequence}

	for {
		// Skip newlines and comments
		for {
			tok := p.current()
			if tok.Type == TokenNewline || tok.Type == TokenComment {
				p.advance()
				continue
			}
			break
		}

		// Check for sequence end
		tok := p.current()
		if tok.Type != TokenDash {
			break
		}

		// Consume dash
		p.advance()

		// Skip whitespace after dash
		for p.current().Type == TokenIndent {
			p.advance()
		}

		// Parse item value
		item, err := p.parseValue(minIndent)
		if err != nil {
			return nil, err
		}

		if item != nil {
			seq.Children = append(seq.Children, item)
		}
	}

	return seq, nil
}

// parseMapping parses a mapping (dictionary).
func (p *Parser) parseMapping(minIndent int) (*Node, error) {
	mapping := &Node{Type: NodeMapping}

	for {
		// Skip newlines and comments
		for {
			tok := p.current()
			if tok.Type == TokenNewline || tok.Type == TokenComment {
				p.advance()
				continue
			}
			break
		}

		tok := p.current()

		// Check for mapping end (dedent or EOF)
		if tok.Type == TokenEOF || tok.Type == TokenDedent {
			break
		}

		// Expect a key (string)
		if tok.Type != TokenString {
			break
		}

		key := tok.Value
		p.advance()

		// Expect colon
		if p.current().Type != TokenColon {
			// Not a mapping, might be a plain scalar
			return &Node{Type: NodeScalar, Value: key}, nil
		}
		p.advance() // consume colon

		// Skip whitespace after colon
		for p.current().Type == TokenIndent || p.current().Type == TokenNewline {
			p.advance()
		}

		// Parse value
		var value *Node
		var err error

		if p.current().Type == TokenEOF || p.current().Type == TokenNewline || p.current().Type == TokenDedent {
			// Empty value
			value = &Node{Type: NodeScalar, Value: ""}
		} else {
			value, err = p.parseValue(minIndent)
			if err != nil {
				return nil, err
			}
		}

		if value == nil {
			value = &Node{Type: NodeScalar, Value: ""}
		}

		value.Key = key
		mapping.Children = append(mapping.Children, value)
	}

	return mapping, nil
}

// parseScalarOrMapping parses either a scalar or a mapping.
func (p *Parser) parseScalarOrMapping(minIndent int) (*Node, error) {
	tok := p.current()
	value := tok.Value

	p.advance()

	// Check if this is a mapping key
	if p.current().Type == TokenColon {
		// It's a mapping key
		p.advance() // consume colon

		// Skip whitespace but preserve the value if present on same line
		if p.current().Type == TokenIndent {
			p.advance()
		}

		// Parse value
		var valNode *Node
		var err error

		if p.current().Type == TokenEOF || p.current().Type == TokenDedent {
			valNode = &Node{Type: NodeScalar, Value: ""}
		} else if p.current().Type == TokenString || p.current().Type == TokenNumber || p.current().Type == TokenBool || p.current().Type == TokenNull {
			// Simple value on same line
			valNode = &Node{Type: NodeScalar, Value: p.current().Value}
			p.advance()
		} else if p.current().Type == TokenNewline {
			// Value might be on next line - check for sequence or nested mapping
			p.advance() // consume newline
			for p.current().Type == TokenNewline || p.current().Type == TokenIndent {
				p.advance()
			}
			if p.current().Type == TokenDash {
				// It's a sequence
				valNode, err = p.parseSequence(minIndent)
				if err != nil {
					return nil, err
				}
			} else if p.current().Type == TokenString && p.peek().Type == TokenColon {
				// It's a nested mapping
				valNode, err = p.parseMapping(minIndent)
				if err != nil {
					return nil, err
				}
			} else {
				// Empty or unknown
				valNode = &Node{Type: NodeScalar, Value: ""}
			}
		} else {
			valNode, err = p.parseValue(minIndent)
			if err != nil {
				return nil, err
			}
		}

		if valNode == nil {
			valNode = &Node{Type: NodeScalar, Value: ""}
		}

		valNode.Key = value

		// Check for more mapping entries
		mapping := &Node{Type: NodeMapping}
		mapping.Children = append(mapping.Children, valNode)

		for {
			// Skip newlines and indents
			for p.current().Type == TokenNewline || p.current().Type == TokenComment || p.current().Type == TokenIndent {
				p.advance()
			}

			// Check for dedent (end of mapping)
			if p.current().Type == TokenDedent || p.current().Type == TokenEOF {
				p.advance()
				break
			}

			// Check for another mapping entry at same indent
			if p.current().Type == TokenString && p.peek().Type == TokenColon {
				key := p.current().Value
				p.advance()
				p.advance() // consume colon

				for p.current().Type == TokenIndent || p.current().Type == TokenNewline {
					p.advance()
				}

				var nextVal *Node
				if p.current().Type == TokenEOF || p.current().Type == TokenNewline || p.current().Type == TokenDedent {
					nextVal = &Node{Type: NodeScalar, Value: ""}
				} else {
					nextVal, err = p.parseValue(minIndent)
					if err != nil {
						return nil, err
					}
				}

				if nextVal == nil {
					nextVal = &Node{Type: NodeScalar, Value: ""}
				}
				nextVal.Key = key
				mapping.Children = append(mapping.Children, nextVal)
			} else {
				break
			}
		}

		return mapping, nil
	}

	// Just a scalar
	return &Node{Type: NodeScalar, Value: value}, nil
}

// parseInlineSequence parses an inline sequence [a, b, c].
func (p *Parser) parseInlineSequence() (*Node, error) {
	seq := &Node{Type: NodeSequence}

	p.advance() // consume [

	for {
		// Skip whitespace
		for p.current().Type == TokenIndent {
			p.advance()
		}

		if p.current().Type == TokenRBracket {
			p.advance()
			break
		}

		// Parse item
		item, err := p.parseInlineValue()
		if err != nil {
			return nil, err
		}

		if item != nil {
			seq.Children = append(seq.Children, item)
		}

		// Skip comma
		if p.current().Type == TokenComma {
			p.advance()
		} else if p.current().Type != TokenRBracket {
			return nil, fmt.Errorf("expected ',' or ']' in sequence at %d:%d", p.current().Line, p.current().Col)
		}
	}

	return seq, nil
}

// parseInlineMapping parses an inline mapping {a: 1, b: 2}.
func (p *Parser) parseInlineMapping() (*Node, error) {
	mapping := &Node{Type: NodeMapping}

	p.advance() // consume {

	for {
		// Skip whitespace
		for p.current().Type == TokenIndent {
			p.advance()
		}

		if p.current().Type == TokenRBrace {
			p.advance()
			break
		}

		// Expect key
		keyTok := p.current()
		if keyTok.Type != TokenString && keyTok.Type != TokenNumber {
			return nil, fmt.Errorf("expected key in mapping at %d:%d", keyTok.Line, keyTok.Col)
		}
		p.advance()

		// Expect colon
		if p.current().Type != TokenColon {
			return nil, fmt.Errorf("expected ':' after key at %d:%d", p.current().Line, p.current().Col)
		}
		p.advance()

		// Parse value
		val, err := p.parseInlineValue()
		if err != nil {
			return nil, err
		}

		if val == nil {
			val = &Node{Type: NodeScalar, Value: ""}
		}

		val.Key = keyTok.Value
		mapping.Children = append(mapping.Children, val)

		// Skip comma
		if p.current().Type == TokenComma {
			p.advance()
		} else if p.current().Type != TokenRBrace {
			return nil, fmt.Errorf("expected ',' or '}' in mapping at %d:%d", p.current().Line, p.current().Col)
		}
	}

	return mapping, nil
}

// parseInlineValue parses a value in inline context.
func (p *Parser) parseInlineValue() (*Node, error) {
	tok := p.current()

	switch tok.Type {
	case TokenString, TokenNumber, TokenBool, TokenNull:
		p.advance()
		return &Node{Type: NodeScalar, Value: tok.Value}, nil

	case TokenLBracket:
		return p.parseInlineSequence()

	case TokenLBrace:
		return p.parseInlineMapping()

	default:
		return nil, nil
	}
}

// parseMultiLineString parses a multi-line string.
func (p *Parser) parseMultiLineString() (*Node, error) {
	style := p.current().Type
	p.advance()

	// Read all indented lines
	var lines []string

	// Skip to end of line
	for p.current().Type != TokenNewline && p.current().Type != TokenEOF {
		p.advance()
	}
	p.advance() // consume newline

	// Collect indented lines
	for {
		// Skip empty lines
		for p.current().Type == TokenNewline {
			lines = append(lines, "")
			p.advance()
		}

		// Check for dedent (end of block)
		if p.current().Type == TokenDedent || p.current().Type == TokenEOF {
			break
		}

		if p.current().Type == TokenIndent {
			p.advance()
		}

		// Read line content
		var line strings.Builder
		for p.current().Type != TokenNewline && p.current().Type != TokenEOF {
			if p.current().Type == TokenIndent {
				p.advance()
				continue
			}
			line.WriteString(p.current().Value)
			p.advance()
		}

		lines = append(lines, line.String())

		if p.current().Type == TokenNewline {
			p.advance()
		}
	}

	var result string
	if style == TokenPipe {
		// Literal style - preserve newlines
		result = strings.Join(lines, "\n")
	} else {
		// Folded style - fold newlines to spaces
		result = strings.Join(lines, " ")
	}

	return &Node{Type: NodeScalar, Value: result}, nil
}

// parseAnchoredValue parses a value with an anchor.
func (p *Parser) parseAnchoredValue() (*Node, error) {
	anchor := p.current().Value
	p.advance()

	node, err := p.parseValue(0)
	if err != nil {
		return nil, err
	}

	if node != nil {
		node.Anchor = anchor
	}

	return node, nil
}

// Parse parses YAML input string into an AST.
func Parse(input string) (*Node, error) {
	tokens, err := Tokenize(input)
	if err != nil {
		return nil, err
	}

	parser := NewParser(tokens)
	return parser.Parse()
}
