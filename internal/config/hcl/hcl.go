// Package hcl provides an HCL (HashiCorp Configuration Language) parser
// for OpenLoadBalancer. It implements a lexer, parser, and decoder with
// zero external dependencies, producing map[string]any or decoding
// directly into Go structs via reflection.
package hcl

import (
	"encoding"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ---------------------------------------------------------------------------
// Token types
// ---------------------------------------------------------------------------

// TokenType represents an HCL token type.
type TokenType int

const (
	// Special tokens
	TokenEOF TokenType = iota
	TokenNewline

	// Literals
	TokenIdent   // unquoted identifier (key names, block types)
	TokenString  // quoted string "..."
	TokenHeredoc // heredoc string <<EOF ... EOF
	TokenNumber  // int, float, hex, octal, scientific
	TokenBool    // true, false

	// Punctuation
	TokenEquals   // =
	TokenLBrace   // {
	TokenRBrace   // }
	TokenLBracket // [
	TokenRBracket // ]
	TokenComma    // ,
	TokenDot      // .

	// Interpolation
	TokenInterpolation // ${...}

	// Comments (skipped by lexer, but defined for completeness)
	TokenComment
)

// String returns the token type name.
func (t TokenType) String() string {
	names := []string{
		"EOF", "NEWLINE",
		"IDENT", "STRING", "HEREDOC", "NUMBER", "BOOL",
		"EQUALS", "LBRACE", "RBRACE", "LBRACKET", "RBRACKET", "COMMA", "DOT",
		"INTERPOLATION", "COMMENT",
	}
	if int(t) < len(names) {
		return names[t]
	}
	return fmt.Sprintf("TOKEN(%d)", t)
}

// Token represents a lexical token.
type Token struct {
	Type  TokenType
	Value string
	Line  int
	Col   int
}

// String returns a string representation of the token.
func (t Token) String() string {
	return fmt.Sprintf("%s:%q@%d:%d", t.Type, t.Value, t.Line, t.Col)
}

// ---------------------------------------------------------------------------
// Lexer
// ---------------------------------------------------------------------------

// Lexer tokenizes HCL input.
type Lexer struct {
	input   string
	pos     int
	readPos int
	ch      byte
	line    int
	col     int
}

// NewLexer creates a new HCL lexer.
func NewLexer(input string) *Lexer {
	l := &Lexer{
		input: input,
		line:  1,
		col:   0,
	}
	l.readChar()
	return l
}

// readChar reads the next character.
func (l *Lexer) readChar() {
	if l.readPos >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPos]
	}
	l.pos = l.readPos
	l.readPos++
	l.col++
}

// peek returns the next character without consuming it.
func (l *Lexer) peek() byte {
	if l.readPos >= len(l.input) {
		return 0
	}
	return l.input[l.readPos]
}

// peekAt returns the character at offset positions ahead without consuming.
func (l *Lexer) peekAt(offset int) byte {
	idx := l.readPos + offset - 1
	if idx >= len(l.input) {
		return 0
	}
	return l.input[idx]
}

// skipWhitespace skips spaces and tabs (but not newlines).
func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' {
		l.readChar()
	}
}

// skipSingleLineComment skips a comment to end of line (# or //).
func (l *Lexer) skipSingleLineComment() {
	for l.ch != '\n' && l.ch != '\r' && l.ch != 0 {
		l.readChar()
	}
}

// skipMultiLineComment skips a /* ... */ comment.
func (l *Lexer) skipMultiLineComment() {
	l.readChar() // skip *
	for {
		if l.ch == 0 {
			return
		}
		if l.ch == '*' && l.peek() == '/' {
			l.readChar() // skip *
			l.readChar() // skip /
			return
		}
		if l.ch == '\n' {
			l.line++
			l.col = 0
		}
		l.readChar()
	}
}

// NextToken returns the next token.
func (l *Lexer) NextToken() Token {
	// Skip whitespace and comments in a loop
	for {
		l.skipWhitespace()

		if l.ch == '#' {
			l.skipSingleLineComment()
			continue
		}
		if l.ch == '/' && l.peek() == '/' {
			l.skipSingleLineComment()
			continue
		}
		if l.ch == '/' && l.peek() == '*' {
			l.readChar() // skip /
			l.skipMultiLineComment()
			continue
		}
		break
	}

	tok := Token{Line: l.line, Col: l.col}

	switch l.ch {
	case 0:
		tok.Type = TokenEOF
		tok.Value = ""

	case '\n':
		tok.Type = TokenNewline
		tok.Value = "\n"
		l.readChar()
		l.line++
		l.col = 0

	case '\r':
		tok.Type = TokenNewline
		tok.Value = "\n"
		l.readChar()
		if l.ch == '\n' {
			l.readChar()
		}
		l.line++
		l.col = 0

	case '=':
		tok.Type = TokenEquals
		tok.Value = "="
		l.readChar()

	case '{':
		tok.Type = TokenLBrace
		tok.Value = "{"
		l.readChar()

	case '}':
		tok.Type = TokenRBrace
		tok.Value = "}"
		l.readChar()

	case '[':
		tok.Type = TokenLBracket
		tok.Value = "["
		l.readChar()

	case ']':
		tok.Type = TokenRBracket
		tok.Value = "]"
		l.readChar()

	case ',':
		tok.Type = TokenComma
		tok.Value = ","
		l.readChar()

	case '.':
		tok.Type = TokenDot
		tok.Value = "."
		l.readChar()

	case '"':
		return l.readQuotedString()

	case '<':
		if l.peek() == '<' {
			return l.readHeredoc()
		}
		// Fall through to identifier
		return l.readIdentifier()

	default:
		if l.ch == '-' && isDigit(l.peek()) {
			return l.readNumber()
		}
		if isDigit(l.ch) {
			return l.readNumber()
		}
		if isIdentStart(l.ch) {
			return l.readIdentifier()
		}
		// Unknown character, skip
		l.readChar()
		return l.NextToken()
	}

	return tok
}

// readQuotedString reads a double-quoted string, handling escape sequences and interpolation.
func (l *Lexer) readQuotedString() Token {
	tok := Token{Type: TokenString, Line: l.line, Col: l.col}
	l.readChar() // skip opening "

	var result strings.Builder
	for l.ch != '"' && l.ch != 0 {
		if l.ch == '\\' {
			l.readChar()
			switch l.ch {
			case 'n':
				result.WriteByte('\n')
			case 'r':
				result.WriteByte('\r')
			case 't':
				result.WriteByte('\t')
			case '\\':
				result.WriteByte('\\')
			case '"':
				result.WriteByte('"')
			case '$':
				result.WriteByte('$')
			default:
				result.WriteByte('\\')
				result.WriteByte(l.ch)
			}
			l.readChar()
			continue
		}
		if l.ch == '\n' {
			l.line++
			l.col = 0
		}
		result.WriteByte(l.ch)
		l.readChar()
	}

	if l.ch == '"' {
		l.readChar() // skip closing "
	}

	tok.Value = result.String()
	return tok
}

// readHeredoc reads a heredoc string (<<EOF ... EOF or <<-EOF ... EOF).
func (l *Lexer) readHeredoc() Token {
	tok := Token{Type: TokenHeredoc, Line: l.line, Col: l.col}

	l.readChar() // skip first <
	l.readChar() // skip second <

	// Check for indented heredoc (<<-)
	indented := false
	if l.ch == '-' {
		indented = true
		l.readChar()
	}

	// Read the delimiter identifier
	var delim strings.Builder
	for l.ch != '\n' && l.ch != '\r' && l.ch != 0 && l.ch != ' ' && l.ch != '\t' {
		delim.WriteByte(l.ch)
		l.readChar()
	}
	delimStr := delim.String()

	// Skip to end of line
	for l.ch == ' ' || l.ch == '\t' {
		l.readChar()
	}
	if l.ch == '\r' {
		l.readChar()
	}
	if l.ch == '\n' {
		l.readChar()
		l.line++
		l.col = 0
	}

	// Read content lines until we find the closing delimiter
	var content strings.Builder
	var lines []string
	for l.ch != 0 {
		// Read a full line
		var line strings.Builder
		for l.ch != '\n' && l.ch != '\r' && l.ch != 0 {
			line.WriteByte(l.ch)
			l.readChar()
		}
		lineStr := line.String()

		// Check if this line (trimmed) matches the delimiter
		trimmed := strings.TrimSpace(lineStr)
		if trimmed == delimStr {
			// Consume the newline if present
			if l.ch == '\r' {
				l.readChar()
			}
			if l.ch == '\n' {
				l.readChar()
				l.line++
				l.col = 0
			}
			break
		}

		lines = append(lines, lineStr)

		// Consume newline
		if l.ch == '\r' {
			l.readChar()
		}
		if l.ch == '\n' {
			l.readChar()
			l.line++
			l.col = 0
		}
	}

	// If indented heredoc, strip common leading whitespace
	if indented && len(lines) > 0 {
		minIndent := -1
		for _, line := range lines {
			if line == "" {
				continue
			}
			indent := 0
			for _, ch := range line {
				if ch == ' ' || ch == '\t' {
					indent++
				} else {
					break
				}
			}
			if minIndent < 0 || indent < minIndent {
				minIndent = indent
			}
		}
		if minIndent > 0 {
			for i, line := range lines {
				if len(line) >= minIndent {
					lines[i] = line[minIndent:]
				}
			}
		}
	}

	content.WriteString(strings.Join(lines, "\n"))
	tok.Value = content.String()
	return tok
}

// readNumber reads a number: int, float, hex (0x), octal (0o), scientific notation.
func (l *Lexer) readNumber() Token {
	tok := Token{Type: TokenNumber, Line: l.line, Col: l.col}
	pos := l.pos

	// Optional sign
	if l.ch == '-' {
		l.readChar()
	}

	// Hex: 0x...
	if l.ch == '0' && (l.peek() == 'x' || l.peek() == 'X') {
		l.readChar() // 0
		l.readChar() // x
		for isHexDigit(l.ch) {
			l.readChar()
		}
		tok.Value = l.input[pos:l.pos]
		return tok
	}

	// Octal: 0o...
	if l.ch == '0' && (l.peek() == 'o' || l.peek() == 'O') {
		l.readChar() // 0
		l.readChar() // o
		for l.ch >= '0' && l.ch <= '7' {
			l.readChar()
		}
		tok.Value = l.input[pos:l.pos]
		return tok
	}

	// Integer part
	for isDigit(l.ch) {
		l.readChar()
	}

	// Decimal part
	if l.ch == '.' && isDigit(l.peek()) {
		l.readChar()
		for isDigit(l.ch) {
			l.readChar()
		}
	}

	// Exponent
	if l.ch == 'e' || l.ch == 'E' {
		l.readChar()
		if l.ch == '-' || l.ch == '+' {
			l.readChar()
		}
		for isDigit(l.ch) {
			l.readChar()
		}
	}

	tok.Value = l.input[pos:l.pos]
	return tok
}

// readIdentifier reads an unquoted identifier or keyword (true/false).
func (l *Lexer) readIdentifier() Token {
	tok := Token{Line: l.line, Col: l.col}
	pos := l.pos

	for isIdentChar(l.ch) {
		l.readChar()
	}

	value := l.input[pos:l.pos]

	switch value {
	case "true", "false":
		tok.Type = TokenBool
		tok.Value = value
	default:
		tok.Type = TokenIdent
		tok.Value = value
	}

	return tok
}

// Tokenize tokenizes the entire input and returns all tokens.
func Tokenize(input string) ([]Token, error) {
	l := NewLexer(input)
	var tokens []Token

	for {
		tok := l.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == TokenEOF {
			break
		}
	}

	return tokens, nil
}

func isDigit(ch byte) bool { return ch >= '0' && ch <= '9' }
func isHexDigit(ch byte) bool {
	return isDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}
func isIdentStart(ch byte) bool {
	return unicode.IsLetter(rune(ch)) || ch == '_'
}
func isIdentChar(ch byte) bool {
	return unicode.IsLetter(rune(ch)) || unicode.IsDigit(rune(ch)) || ch == '_' || ch == '-'
}

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

// ---------------------------------------------------------------------------
// Decoder: map[string]any → Go struct
// ---------------------------------------------------------------------------

// decodeMap decodes a map[string]any into a Go value via reflection.
func decodeMap(m map[string]any, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return errors.New("decode target must be a non-nil pointer")
	}
	return decodeValue(reflect.ValueOf(m), rv.Elem())
}

// decodeValue decodes a reflect.Value (from the parsed map) into a target reflect.Value.
func decodeValue(src reflect.Value, dst reflect.Value) error {
	// Unwrap interface
	if src.Kind() == reflect.Interface {
		src = src.Elem()
	}

	// Handle nil source
	if !src.IsValid() {
		return nil
	}

	// Handle pointer destination
	if dst.Kind() == reflect.Ptr {
		if dst.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
		}
		return decodeValue(src, dst.Elem())
	}

	switch dst.Kind() {
	case reflect.Struct:
		return decodeStruct(src, dst)
	case reflect.Map:
		return decodeMapField(src, dst)
	case reflect.Slice:
		return decodeSlice(src, dst)
	case reflect.Interface:
		dst.Set(src)
		return nil
	default:
		return decodeScalar(src, dst)
	}
}

// decodeStruct decodes a map into a struct.
func decodeStruct(src reflect.Value, dst reflect.Value) error {
	if src.Kind() != reflect.Map {
		return fmt.Errorf("cannot decode %v into struct %v", src.Type(), dst.Type())
	}

	t := dst.Type()

	// Build field map: lowercase tag/name → field index
	type fieldInfo struct {
		index int
	}
	fieldMap := make(map[string]fieldInfo)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" { // skip unexported
			continue
		}

		name := ""
		// Try hcl tag first
		if tag := field.Tag.Get("hcl"); tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] != "" && parts[0] != "-" {
				name = parts[0]
			}
		}
		// Fall back to yaml tag
		if name == "" {
			if tag := field.Tag.Get("yaml"); tag != "" {
				parts := strings.Split(tag, ",")
				if parts[0] != "" && parts[0] != "-" {
					name = parts[0]
				}
			}
		}
		// Fall back to json tag
		if name == "" {
			if tag := field.Tag.Get("json"); tag != "" {
				parts := strings.Split(tag, ",")
				if parts[0] != "" && parts[0] != "-" {
					name = parts[0]
				}
			}
		}
		// Fall back to field name
		if name == "" {
			name = field.Name
		}

		fieldMap[strings.ToLower(name)] = fieldInfo{index: i}
	}

	// Iterate map keys
	for _, key := range src.MapKeys() {
		keyStr := fmt.Sprintf("%v", key.Interface())
		fi, ok := fieldMap[strings.ToLower(keyStr)]
		if !ok {
			continue
		}

		fieldVal := dst.Field(fi.index)
		srcVal := src.MapIndex(key)

		if err := decodeValue(srcVal, fieldVal); err != nil {
			return fmt.Errorf("field %s: %w", keyStr, err)
		}
	}

	return nil
}

// decodeMapField decodes a source value into a map destination.
func decodeMapField(src reflect.Value, dst reflect.Value) error {
	if src.Kind() == reflect.Interface {
		src = src.Elem()
	}
	if src.Kind() != reflect.Map {
		return fmt.Errorf("cannot decode %v into map", src.Type())
	}

	if dst.IsNil() {
		dst.Set(reflect.MakeMap(dst.Type()))
	}

	keyType := dst.Type().Key()
	elemType := dst.Type().Elem()

	for _, key := range src.MapKeys() {
		// Create map key
		newKey := reflect.New(keyType).Elem()
		if err := decodeScalar(key, newKey); err != nil {
			return fmt.Errorf("map key: %w", err)
		}

		// Create map element
		newElem := reflect.New(elemType).Elem()
		if err := decodeValue(src.MapIndex(key), newElem); err != nil {
			return fmt.Errorf("map value: %w", err)
		}

		dst.SetMapIndex(newKey, newElem)
	}

	return nil
}

// decodeSlice decodes a source value into a slice destination.
func decodeSlice(src reflect.Value, dst reflect.Value) error {
	if src.Kind() == reflect.Interface {
		src = src.Elem()
	}

	// If source is a slice/array
	if src.Kind() == reflect.Slice || src.Kind() == reflect.Array {
		elemType := dst.Type().Elem()
		slice := reflect.MakeSlice(dst.Type(), 0, src.Len())

		for i := 0; i < src.Len(); i++ {
			elem := reflect.New(elemType).Elem()
			if err := decodeValue(src.Index(i), elem); err != nil {
				return fmt.Errorf("slice index %d: %w", i, err)
			}
			slice = reflect.Append(slice, elem)
		}

		dst.Set(slice)
		return nil
	}

	// Single value → one-element slice
	elemType := dst.Type().Elem()
	elem := reflect.New(elemType).Elem()
	if err := decodeValue(src, elem); err != nil {
		return err
	}
	dst.Set(reflect.Append(reflect.MakeSlice(dst.Type(), 0, 1), elem))
	return nil
}

// decodeScalar decodes a scalar value (string, int, float, bool) into a destination.
func decodeScalar(src reflect.Value, dst reflect.Value) error {
	if src.Kind() == reflect.Interface {
		src = src.Elem()
	}

	if !src.IsValid() {
		return nil
	}

	switch dst.Kind() {
	case reflect.String:
		dst.SetString(fmt.Sprintf("%v", src.Interface()))
		return nil

	case reflect.Bool:
		switch src.Kind() {
		case reflect.Bool:
			dst.SetBool(src.Bool())
		case reflect.String:
			b, err := strconv.ParseBool(src.String())
			if err != nil {
				return fmt.Errorf("cannot parse %q as bool", src.String())
			}
			dst.SetBool(b)
		default:
			return fmt.Errorf("cannot convert %v to bool", src.Type())
		}
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// Duration special case
		if dst.Type() == reflect.TypeOf(time.Duration(0)) {
			s := fmt.Sprintf("%v", src.Interface())
			dur, err := time.ParseDuration(s)
			if err != nil {
				return fmt.Errorf("cannot parse %q as duration", s)
			}
			dst.SetInt(int64(dur))
			return nil
		}

		switch src.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			dst.SetInt(src.Int())
		case reflect.Float32, reflect.Float64:
			dst.SetInt(int64(src.Float()))
		case reflect.String:
			i, err := strconv.ParseInt(src.String(), 0, 64)
			if err != nil {
				return fmt.Errorf("cannot parse %q as int", src.String())
			}
			dst.SetInt(i)
		default:
			return fmt.Errorf("cannot convert %v to int", src.Type())
		}
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		switch src.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			dst.SetUint(uint64(src.Int()))
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			dst.SetUint(src.Uint())
		case reflect.Float32, reflect.Float64:
			dst.SetUint(uint64(src.Float()))
		case reflect.String:
			i, err := strconv.ParseUint(src.String(), 0, 64)
			if err != nil {
				return fmt.Errorf("cannot parse %q as uint", src.String())
			}
			dst.SetUint(i)
		default:
			return fmt.Errorf("cannot convert %v to uint", src.Type())
		}
		return nil

	case reflect.Float32, reflect.Float64:
		switch src.Kind() {
		case reflect.Float32, reflect.Float64:
			dst.SetFloat(src.Float())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			dst.SetFloat(float64(src.Int()))
		case reflect.String:
			f, err := strconv.ParseFloat(src.String(), 64)
			if err != nil {
				return fmt.Errorf("cannot parse %q as float", src.String())
			}
			dst.SetFloat(f)
		default:
			return fmt.Errorf("cannot convert %v to float", src.Type())
		}
		return nil

	case reflect.Interface:
		dst.Set(src)
		return nil

	case reflect.Ptr:
		if dst.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
		}
		return decodeScalar(src, dst.Elem())

	case reflect.Struct:
		// Check for TextUnmarshaler
		if dst.CanAddr() {
			if u, ok := dst.Addr().Interface().(encoding.TextUnmarshaler); ok {
				s := fmt.Sprintf("%v", src.Interface())
				return u.UnmarshalText([]byte(s))
			}
		}
		// Try map → struct
		if src.Kind() == reflect.Map {
			return decodeStruct(src, dst)
		}
		return fmt.Errorf("cannot decode scalar into struct %v", dst.Type())

	case reflect.Map:
		if src.Kind() == reflect.Map {
			return decodeMapField(src, dst)
		}
		return fmt.Errorf("cannot decode %v into map %v", src.Type(), dst.Type())

	case reflect.Slice:
		return decodeSlice(src, dst)

	default:
		return fmt.Errorf("cannot decode %v into %v", src.Type(), dst.Type())
	}
}
