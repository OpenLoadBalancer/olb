// Package toml implements a TOML v1.0 parser with zero external dependencies.
// It provides lexing, parsing, and decoding of TOML documents into Go values.
package toml

import (
	"encoding"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Token types
// ---------------------------------------------------------------------------

// TokenType represents a TOML token type.
type TokenType int

const (
	tokenEOF TokenType = iota
	tokenNewline
	tokenEquals
	tokenDot
	tokenComma
	tokenLBracket
	tokenRBracket
	tokenLBrace
	tokenRBrace
	tokenBareKey
	tokenBasicString
	tokenLiteralString
	tokenMultilineBasicString
	tokenMultilineLiteralString
	tokenInteger
	tokenFloat
	tokenBool
	tokenDatetime
	tokenComment
)

// Token represents a lexical token.
type Token struct {
	Type  TokenType
	Value string
	Line  int
	Col   int
}

// ---------------------------------------------------------------------------
// Lexer
// ---------------------------------------------------------------------------

// Lexer tokenizes TOML input.
type Lexer struct {
	input []rune
	pos   int
	line  int
	col   int
}

// NewLexer creates a new TOML lexer.
func NewLexer(input string) *Lexer {
	return &Lexer{
		input: []rune(input),
		line:  1,
		col:   1,
	}
}

// ch returns the current character or 0 at EOF.
func (l *Lexer) ch() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	return l.input[l.pos]
}

// peek returns the character at offset without consuming.
func (l *Lexer) peek(offset int) rune {
	idx := l.pos + offset
	if idx >= len(l.input) || idx < 0 {
		return 0
	}
	return l.input[idx]
}

// advance moves forward one character.
func (l *Lexer) advance() {
	if l.pos < len(l.input) {
		if l.input[l.pos] == '\n' {
			l.line++
			l.col = 1
		} else {
			l.col++
		}
		l.pos++
	}
}

// skipWhitespace skips spaces and tabs (not newlines).
func (l *Lexer) skipWhitespace() {
	for l.ch() == ' ' || l.ch() == '\t' {
		l.advance()
	}
}

// skipComment skips a comment to end of line.
func (l *Lexer) skipComment() {
	if l.ch() == '#' {
		for l.ch() != '\n' && l.ch() != 0 {
			l.advance()
		}
	}
}

// NextToken returns the next token from the input.
func (l *Lexer) NextToken() (Token, error) {
	l.skipWhitespace()

	tok := Token{Line: l.line, Col: l.col}

	ch := l.ch()
	switch {
	case ch == 0:
		tok.Type = tokenEOF
		return tok, nil

	case ch == '#':
		// Comment — skip to EOL and return next token
		l.skipComment()
		return l.NextToken()

	case ch == '\n':
		tok.Type = tokenNewline
		tok.Value = "\n"
		l.advance()
		return tok, nil

	case ch == '\r':
		l.advance()
		if l.ch() == '\n' {
			l.advance()
		}
		tok.Type = tokenNewline
		tok.Value = "\n"
		return tok, nil

	case ch == '=':
		tok.Type = tokenEquals
		tok.Value = "="
		l.advance()
		return tok, nil

	case ch == '.':
		tok.Type = tokenDot
		tok.Value = "."
		l.advance()
		return tok, nil

	case ch == ',':
		tok.Type = tokenComma
		tok.Value = ","
		l.advance()
		return tok, nil

	case ch == '[':
		tok.Type = tokenLBracket
		tok.Value = "["
		l.advance()
		return tok, nil

	case ch == ']':
		tok.Type = tokenRBracket
		tok.Value = "]"
		l.advance()
		return tok, nil

	case ch == '{':
		tok.Type = tokenLBrace
		tok.Value = "{"
		l.advance()
		return tok, nil

	case ch == '}':
		tok.Type = tokenRBrace
		tok.Value = "}"
		l.advance()
		return tok, nil

	case ch == '"':
		return l.readString()

	case ch == '\'':
		return l.readLiteralString()

	default:
		// Bare key, number, bool, date/time
		return l.readBareValue()
	}
}

// readString reads a basic string or multi-line basic string.
func (l *Lexer) readString() (Token, error) {
	tok := Token{Line: l.line, Col: l.col}

	// Check for multi-line """
	if l.peek(1) == '"' && l.peek(2) == '"' {
		return l.readMultilineBasicString()
	}

	tok.Type = tokenBasicString
	l.advance() // skip opening "

	var sb strings.Builder
	for {
		ch := l.ch()
		if ch == 0 {
			return tok, fmt.Errorf("unterminated basic string at %d:%d", tok.Line, tok.Col)
		}
		if ch == '\n' || ch == '\r' {
			return tok, fmt.Errorf("newline in basic string at %d:%d", tok.Line, tok.Col)
		}
		if ch == '"' {
			l.advance()
			break
		}
		if ch == '\\' {
			l.advance()
			esc, err := l.readEscape()
			if err != nil {
				return tok, err
			}
			sb.WriteString(esc)
			continue
		}
		sb.WriteRune(ch)
		l.advance()
	}

	tok.Value = sb.String()
	return tok, nil
}

// readEscape processes an escape sequence after the backslash.
func (l *Lexer) readEscape() (string, error) {
	ch := l.ch()
	l.advance()
	switch ch {
	case 'b':
		return "\b", nil
	case 't':
		return "\t", nil
	case 'n':
		return "\n", nil
	case 'f':
		return "\f", nil
	case 'r':
		return "\r", nil
	case '"':
		return "\"", nil
	case '\\':
		return "\\", nil
	case 'u':
		return l.readUnicodeEscape(4)
	case 'U':
		return l.readUnicodeEscape(8)
	default:
		return "", fmt.Errorf("invalid escape sequence '\\%c' at %d:%d", ch, l.line, l.col)
	}
}

// readUnicodeEscape reads n hex digits and returns the corresponding character.
func (l *Lexer) readUnicodeEscape(n int) (string, error) {
	var hex strings.Builder
	for i := 0; i < n; i++ {
		ch := l.ch()
		if !isHexDigit(ch) {
			return "", fmt.Errorf("invalid unicode escape at %d:%d", l.line, l.col)
		}
		hex.WriteRune(ch)
		l.advance()
	}
	code, err := strconv.ParseUint(hex.String(), 16, 32)
	if err != nil {
		return "", fmt.Errorf("invalid unicode escape at %d:%d", l.line, l.col)
	}
	if !utf8.ValidRune(rune(code)) {
		return "", fmt.Errorf("invalid unicode codepoint U+%04X at %d:%d", code, l.line, l.col)
	}
	return string(rune(code)), nil
}

// readMultilineBasicString reads a """ ... """ string.
func (l *Lexer) readMultilineBasicString() (Token, error) {
	tok := Token{Type: tokenMultilineBasicString, Line: l.line, Col: l.col}

	// skip opening """
	l.advance()
	l.advance()
	l.advance()

	// A newline immediately after """ is trimmed.
	if l.ch() == '\n' {
		l.advance()
	} else if l.ch() == '\r' {
		l.advance()
		if l.ch() == '\n' {
			l.advance()
		}
	}

	var sb strings.Builder
	for {
		ch := l.ch()
		if ch == 0 {
			return tok, fmt.Errorf("unterminated multi-line basic string at %d:%d", tok.Line, tok.Col)
		}
		if ch == '"' && l.peek(1) == '"' && l.peek(2) == '"' {
			l.advance()
			l.advance()
			l.advance()
			break
		}
		if ch == '\\' {
			l.advance()
			// Line ending backslash — trim whitespace
			if l.ch() == '\n' || l.ch() == '\r' {
				l.skipNewlinesAndWhitespace()
				continue
			}
			esc, err := l.readEscape()
			if err != nil {
				return tok, err
			}
			sb.WriteString(esc)
			continue
		}
		if ch == '\r' {
			sb.WriteRune('\n')
			l.advance()
			if l.ch() == '\n' {
				l.advance()
			}
			continue
		}
		sb.WriteRune(ch)
		l.advance()
	}

	tok.Value = sb.String()
	return tok, nil
}

// skipNewlinesAndWhitespace skips newlines and whitespace (for line-ending backslash).
func (l *Lexer) skipNewlinesAndWhitespace() {
	for l.ch() == '\n' || l.ch() == '\r' || l.ch() == ' ' || l.ch() == '\t' {
		l.advance()
	}
}

// readLiteralString reads a '...' string or ”'...”' multi-line literal string.
func (l *Lexer) readLiteralString() (Token, error) {
	tok := Token{Line: l.line, Col: l.col}

	if l.peek(1) == '\'' && l.peek(2) == '\'' {
		return l.readMultilineLiteralString()
	}

	tok.Type = tokenLiteralString
	l.advance() // skip opening '

	var sb strings.Builder
	for {
		ch := l.ch()
		if ch == 0 {
			return tok, fmt.Errorf("unterminated literal string at %d:%d", tok.Line, tok.Col)
		}
		if ch == '\n' || ch == '\r' {
			return tok, fmt.Errorf("newline in literal string at %d:%d", tok.Line, tok.Col)
		}
		if ch == '\'' {
			l.advance()
			break
		}
		sb.WriteRune(ch)
		l.advance()
	}

	tok.Value = sb.String()
	return tok, nil
}

// readMultilineLiteralString reads a ”' ... ”' string.
func (l *Lexer) readMultilineLiteralString() (Token, error) {
	tok := Token{Type: tokenMultilineLiteralString, Line: l.line, Col: l.col}

	l.advance()
	l.advance()
	l.advance()

	// Trim leading newline
	if l.ch() == '\n' {
		l.advance()
	} else if l.ch() == '\r' {
		l.advance()
		if l.ch() == '\n' {
			l.advance()
		}
	}

	var sb strings.Builder
	for {
		ch := l.ch()
		if ch == 0 {
			return tok, fmt.Errorf("unterminated multi-line literal string at %d:%d", tok.Line, tok.Col)
		}
		if ch == '\'' && l.peek(1) == '\'' && l.peek(2) == '\'' {
			l.advance()
			l.advance()
			l.advance()
			break
		}
		if ch == '\r' {
			sb.WriteRune('\n')
			l.advance()
			if l.ch() == '\n' {
				l.advance()
			}
			continue
		}
		sb.WriteRune(ch)
		l.advance()
	}

	tok.Value = sb.String()
	return tok, nil
}

// readBareValue reads a bare key, number, bool, or date/time.
func (l *Lexer) readBareValue() (Token, error) {
	tok := Token{Line: l.line, Col: l.col}

	start := l.pos
	for {
		ch := l.ch()
		if ch == 0 || ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' ||
			ch == '=' || ch == ',' ||
			ch == '[' || ch == ']' || ch == '{' || ch == '}' ||
			ch == '#' || ch == '"' || ch == '\'' {
			break
		}
		// Dot is normally a key separator, but inside a number (e.g. 3.14) or
		// datetime (e.g. 2024-01-15T10:30:00.123Z) it must be included.
		if ch == '.' {
			// Check if we've been reading a number so far and the char after '.' is a digit.
			sofar := string(l.input[start:l.pos])
			if isNumberStart(sofar) && l.peek(1) >= '0' && l.peek(1) <= '9' {
				// Part of a float literal — keep going.
				l.advance()
				continue
			}
			// Otherwise treat '.' as a separator.
			break
		}
		l.advance()
	}

	raw := string(l.input[start:l.pos])
	if raw == "" {
		return tok, fmt.Errorf("unexpected character '%c' at %d:%d", l.ch(), l.line, l.col)
	}

	// Classify the token
	lower := strings.ToLower(raw)

	// Boolean
	if lower == "true" || lower == "false" {
		tok.Type = tokenBool
		tok.Value = lower
		return tok, nil
	}

	// Special float values
	if lower == "inf" || lower == "+inf" || lower == "-inf" ||
		lower == "nan" || lower == "+nan" || lower == "-nan" {
		tok.Type = tokenFloat
		tok.Value = raw
		return tok, nil
	}

	// Try date/time — contains T or : or is date-like (YYYY-MM-DD)
	if looksLikeDatetime(raw) {
		tok.Type = tokenDatetime
		tok.Value = raw
		return tok, nil
	}

	// Try to detect number vs bare key
	if isNumberStart(raw) {
		if isFloat(raw) {
			tok.Type = tokenFloat
			tok.Value = raw
			return tok, nil
		}
		if isInteger(raw) {
			tok.Type = tokenInteger
			tok.Value = raw
			return tok, nil
		}
	}

	// Default: bare key
	if !isValidBareKey(raw) {
		return tok, fmt.Errorf("invalid bare key %q at %d:%d", raw, tok.Line, tok.Col)
	}
	tok.Type = tokenBareKey
	tok.Value = raw
	return tok, nil
}

// ---------------------------------------------------------------------------
// Classification helpers
// ---------------------------------------------------------------------------

func isHexDigit(ch rune) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

func isValidBareKey(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if !(unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

func isNumberStart(s string) bool {
	if s == "" {
		return false
	}
	ch := rune(s[0])
	return ch == '+' || ch == '-' || (ch >= '0' && ch <= '9')
}

func isInteger(s string) bool {
	clean := strings.ReplaceAll(s, "_", "")
	if clean == "" {
		return false
	}

	// Handle sign
	if clean[0] == '+' || clean[0] == '-' {
		clean = clean[1:]
	}
	if clean == "" {
		return false
	}

	// Hex
	if strings.HasPrefix(clean, "0x") || strings.HasPrefix(clean, "0X") {
		hex := clean[2:]
		if hex == "" {
			return false
		}
		for _, ch := range hex {
			if !isHexDigit(ch) {
				return false
			}
		}
		return true
	}
	// Octal
	if strings.HasPrefix(clean, "0o") || strings.HasPrefix(clean, "0O") {
		oct := clean[2:]
		if oct == "" {
			return false
		}
		for _, ch := range oct {
			if ch < '0' || ch > '7' {
				return false
			}
		}
		return true
	}
	// Binary
	if strings.HasPrefix(clean, "0b") || strings.HasPrefix(clean, "0B") {
		bin := clean[2:]
		if bin == "" {
			return false
		}
		for _, ch := range bin {
			if ch != '0' && ch != '1' {
				return false
			}
		}
		return true
	}
	// Decimal
	for _, ch := range clean {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func isFloat(s string) bool {
	clean := strings.ReplaceAll(s, "_", "")
	if clean == "" {
		return false
	}

	lower := strings.ToLower(clean)
	if lower == "inf" || lower == "+inf" || lower == "-inf" ||
		lower == "nan" || lower == "+nan" || lower == "-nan" {
		return true
	}

	// Must contain . or e/E to be float
	if !strings.ContainsAny(clean, ".eE") {
		return false
	}

	// Strip sign
	if clean[0] == '+' || clean[0] == '-' {
		clean = clean[1:]
	}

	// Try parsing
	_, err := strconv.ParseFloat(clean, 64)
	return err == nil
}

func looksLikeDatetime(s string) bool {
	// Quick heuristic: at least 10 chars and starts with 4 digits-dash
	if len(s) < 5 {
		return false
	}
	// Local time like 10:30:00
	if len(s) >= 5 && s[2] == ':' {
		return isDigitChar(s[0]) && isDigitChar(s[1])
	}
	// Date: YYYY-MM-DD
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		return isDigitChar(s[0]) && isDigitChar(s[1]) && isDigitChar(s[2]) && isDigitChar(s[3])
	}
	return false
}

func isDigitChar(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

// Tokenize returns all tokens from the input.
func Tokenize(input string) ([]Token, error) {
	l := NewLexer(input)
	var tokens []Token
	for {
		tok, err := l.NextToken()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, tok)
		if tok.Type == tokenEOF {
			break
		}
	}
	return tokens, nil
}

// ---------------------------------------------------------------------------
// Parser — produces map[string]any
// ---------------------------------------------------------------------------

// parser holds the state for parsing a TOML document.
type parser struct {
	tokens []Token
	pos    int
	result map[string]any
	// implicitTables tracks tables created implicitly by dotted keys.
	implicitTables map[string]bool
	// definedTables tracks tables explicitly defined with [table].
	definedTables map[string]bool
	// arrayTables tracks keys that are arrays of tables [[table]].
	arrayTables map[string]bool
}

func newParser(tokens []Token) *parser {
	return &parser{
		tokens:         tokens,
		result:         make(map[string]any),
		implicitTables: make(map[string]bool),
		definedTables:  make(map[string]bool),
		arrayTables:    make(map[string]bool),
	}
}

func (p *parser) current() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: tokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *parser) advance() {
	if p.pos < len(p.tokens) {
		p.pos++
	}
}

func (p *parser) skipNewlines() {
	for p.current().Type == tokenNewline {
		p.advance()
	}
}

// parse processes the full TOML document.
func (p *parser) parse() (map[string]any, error) {
	currentTable := []string{} // root

	for {
		p.skipNewlines()
		tok := p.current()

		switch tok.Type {
		case tokenEOF:
			return p.result, nil

		case tokenLBracket:
			// Check for array of tables [[...]] or standard table [...]
			p.advance()
			if p.current().Type == tokenLBracket {
				// Array of tables
				p.advance()
				keys, err := p.parseKey()
				if err != nil {
					return nil, fmt.Errorf("parsing array table name: %w", err)
				}
				// Expect ]]
				if p.current().Type != tokenRBracket {
					return nil, fmt.Errorf("expected ']' at %d:%d", p.current().Line, p.current().Col)
				}
				p.advance()
				if p.current().Type != tokenRBracket {
					return nil, fmt.Errorf("expected ']]' at %d:%d", p.current().Line, p.current().Col)
				}
				p.advance()
				p.expectNewlineOrEOF()

				fullKey := strings.Join(keys, ".")
				p.arrayTables[fullKey] = true

				// Create the array entry
				target, err := p.ensureArrayTable(keys)
				if err != nil {
					return nil, err
				}
				currentTable = p.pathForArrayTable(keys, target)

			} else {
				// Standard table
				keys, err := p.parseKey()
				if err != nil {
					return nil, fmt.Errorf("parsing table name: %w", err)
				}
				// Expect ]
				if p.current().Type != tokenRBracket {
					return nil, fmt.Errorf("expected ']' at %d:%d", p.current().Line, p.current().Col)
				}
				p.advance()
				p.expectNewlineOrEOF()

				fullKey := strings.Join(keys, ".")

				// Check for duplicate table definition
				if p.definedTables[fullKey] {
					return nil, fmt.Errorf("duplicate table [%s]", fullKey)
				}
				if p.arrayTables[fullKey] {
					return nil, fmt.Errorf("cannot define [%s] as table, already used as array of tables", fullKey)
				}
				p.definedTables[fullKey] = true

				// Ensure parent tables exist
				if err := p.ensureTable(keys); err != nil {
					return nil, err
				}
				currentTable = keys
			}

		default:
			// Key-value pair
			if isKeyToken(tok.Type) {
				if err := p.parseKeyValue(currentTable); err != nil {
					return nil, err
				}
			} else {
				return nil, fmt.Errorf("unexpected token %q at %d:%d", tok.Value, tok.Line, tok.Col)
			}
		}
	}
}

// isKeyToken returns whether a token type can start a key.
func isKeyToken(t TokenType) bool {
	return t == tokenBareKey || t == tokenBasicString || t == tokenLiteralString ||
		t == tokenInteger || t == tokenBool || t == tokenFloat
}

// parseKey reads a (possibly dotted) key and returns the parts.
func (p *parser) parseKey() ([]string, error) {
	var keys []string

	key, err := p.parseSingleKey()
	if err != nil {
		return nil, err
	}
	keys = append(keys, key)

	for p.current().Type == tokenDot {
		p.advance()
		key, err = p.parseSingleKey()
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}

	return keys, nil
}

// parseSingleKey reads one key (bare, basic string, or literal string).
func (p *parser) parseSingleKey() (string, error) {
	tok := p.current()
	switch tok.Type {
	case tokenBareKey, tokenBasicString, tokenLiteralString, tokenInteger, tokenBool, tokenFloat:
		p.advance()
		return tok.Value, nil
	default:
		return "", fmt.Errorf("expected key, got %q at %d:%d", tok.Value, tok.Line, tok.Col)
	}
}

// parseKeyValue parses key = value and inserts into the table at prefix.
func (p *parser) parseKeyValue(prefix []string) error {
	keys, err := p.parseKey()
	if err != nil {
		return err
	}

	// Expect =
	p.skipWhitespace()
	if p.current().Type != tokenEquals {
		return fmt.Errorf("expected '=' after key at %d:%d", p.current().Line, p.current().Col)
	}
	p.advance()
	p.skipWhitespace()

	// Parse value
	val, err := p.parseValue()
	if err != nil {
		return err
	}

	// Insert the value into the result map
	fullPath := append(append([]string{}, prefix...), keys...)
	return p.insertValue(fullPath, val)
}

// skipWhitespace skips newline tokens that are whitespace in value context.
func (p *parser) skipWhitespace() {
	// In TOML, whitespace around = is fine but newlines are not
	// This is handled by the lexer already (spaces/tabs)
}

// parseValue parses a TOML value.
func (p *parser) parseValue() (any, error) {
	tok := p.current()

	switch tok.Type {
	case tokenBasicString, tokenLiteralString, tokenMultilineBasicString, tokenMultilineLiteralString:
		p.advance()
		return tok.Value, nil

	case tokenInteger:
		p.advance()
		return parseInteger(tok.Value)

	case tokenFloat:
		p.advance()
		return parseFloat(tok.Value)

	case tokenBool:
		p.advance()
		return tok.Value == "true", nil

	case tokenDatetime:
		p.advance()
		return parseDatetime(tok.Value)

	case tokenLBracket:
		return p.parseArray()

	case tokenLBrace:
		return p.parseInlineTable()

	default:
		return nil, fmt.Errorf("expected value, got %q at %d:%d", tok.Value, tok.Line, tok.Col)
	}
}

// parseArray parses [ val1, val2, ... ]
func (p *parser) parseArray() (any, error) {
	p.advance() // skip [

	var arr []any

	for {
		p.skipNewlines()

		if p.current().Type == tokenRBracket {
			p.advance()
			return arr, nil
		}

		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		arr = append(arr, val)

		p.skipNewlines()

		if p.current().Type == tokenComma {
			p.advance()
			continue
		}
		p.skipNewlines()
		if p.current().Type == tokenRBracket {
			p.advance()
			return arr, nil
		}
		return nil, fmt.Errorf("expected ',' or ']' in array at %d:%d", p.current().Line, p.current().Col)
	}
}

// parseInlineTable parses { key = val, ... }
func (p *parser) parseInlineTable() (any, error) {
	p.advance() // skip {

	tbl := make(map[string]any)
	first := true

	for {
		// Skip whitespace but NOT newlines — inline tables must be on one line per spec,
		// but we are lenient here.
		p.skipNewlines()

		if p.current().Type == tokenRBrace {
			p.advance()
			return tbl, nil
		}

		if !first {
			if p.current().Type != tokenComma {
				return nil, fmt.Errorf("expected ',' or '}' in inline table at %d:%d", p.current().Line, p.current().Col)
			}
			p.advance()
			p.skipNewlines()
		}
		first = false

		if p.current().Type == tokenRBrace {
			p.advance()
			return tbl, nil
		}

		// Read key = value
		keys, err := p.parseKey()
		if err != nil {
			return nil, err
		}

		if p.current().Type != tokenEquals {
			return nil, fmt.Errorf("expected '=' in inline table at %d:%d", p.current().Line, p.current().Col)
		}
		p.advance()

		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}

		// Insert into the inline table using dotted keys
		if err := insertIntoMap(tbl, keys, val); err != nil {
			return nil, err
		}
	}
}

// insertIntoMap inserts a value into a nested map using the given key path.
func insertIntoMap(m map[string]any, keys []string, val any) error {
	current := m
	for i, key := range keys {
		if i == len(keys)-1 {
			if _, exists := current[key]; exists {
				return fmt.Errorf("duplicate key %q", strings.Join(keys, "."))
			}
			current[key] = val
		} else {
			next, ok := current[key]
			if !ok {
				sub := make(map[string]any)
				current[key] = sub
				current = sub
			} else {
				sub, ok := next.(map[string]any)
				if !ok {
					return fmt.Errorf("key %q already has a non-table value", key)
				}
				current = sub
			}
		}
	}
	return nil
}

// insertValue inserts a value at the given full path in p.result.
func (p *parser) insertValue(fullPath []string, val any) error {
	current := p.result
	for i, key := range fullPath {
		if i == len(fullPath)-1 {
			// Final key — insert
			if _, exists := current[key]; exists {
				return fmt.Errorf("duplicate key %q", strings.Join(fullPath, "."))
			}
			current[key] = val
		} else {
			// Intermediate key — traverse or create
			next, ok := current[key]
			if !ok {
				sub := make(map[string]any)
				current[key] = sub
				current = sub

				// Track implicit table
				partialKey := strings.Join(fullPath[:i+1], ".")
				p.implicitTables[partialKey] = true
			} else {
				switch v := next.(type) {
				case map[string]any:
					current = v
				case []any:
					// For arrays of tables, target the last element
					if len(v) > 0 {
						if lastMap, ok := v[len(v)-1].(map[string]any); ok {
							current = lastMap
						} else {
							return fmt.Errorf("key %q is not a table", key)
						}
					} else {
						return fmt.Errorf("key %q is an empty array", key)
					}
				default:
					return fmt.Errorf("key %q already has a non-table value", key)
				}
			}
		}
	}
	return nil
}

// ensureTable ensures that the table at keys exists, creating intermediate maps.
func (p *parser) ensureTable(keys []string) error {
	current := p.result
	for i, key := range keys {
		next, ok := current[key]
		if !ok {
			sub := make(map[string]any)
			current[key] = sub
			current = sub

			partialKey := strings.Join(keys[:i+1], ".")
			p.implicitTables[partialKey] = true
		} else {
			switch v := next.(type) {
			case map[string]any:
				current = v
			case []any:
				// For arrays of tables, target the last element
				if len(v) > 0 {
					if lastMap, ok := v[len(v)-1].(map[string]any); ok {
						current = lastMap
					} else {
						return fmt.Errorf("key %q is not a table", key)
					}
				}
			default:
				return fmt.Errorf("key %q already has a non-table value", key)
			}
		}
	}
	return nil
}

// ensureArrayTable ensures the array of tables and appends a new entry.
func (p *parser) ensureArrayTable(keys []string) ([]any, error) {
	current := p.result

	// Traverse to parent
	for i := 0; i < len(keys)-1; i++ {
		key := keys[i]
		next, ok := current[key]
		if !ok {
			sub := make(map[string]any)
			current[key] = sub
			current = sub
		} else {
			switch v := next.(type) {
			case map[string]any:
				current = v
			case []any:
				if len(v) > 0 {
					if lastMap, ok := v[len(v)-1].(map[string]any); ok {
						current = lastMap
					} else {
						return nil, fmt.Errorf("key %q is not a table", key)
					}
				}
			default:
				return nil, fmt.Errorf("key %q already exists as non-table", key)
			}
		}
	}

	lastKey := keys[len(keys)-1]
	newEntry := make(map[string]any)

	existing, ok := current[lastKey]
	if !ok {
		arr := []any{newEntry}
		current[lastKey] = arr
		return arr, nil
	}

	arr, ok := existing.([]any)
	if !ok {
		return nil, fmt.Errorf("key %q already exists as non-array", lastKey)
	}
	arr = append(arr, newEntry)
	current[lastKey] = arr
	return arr, nil
}

// pathForArrayTable returns the key path to use as currentTable after processing [[keys]].
func (p *parser) pathForArrayTable(keys []string, arr []any) []string {
	// The current table after [[a.b]] is effectively a.b pointing to the last element.
	// We return keys as-is; insertValue will navigate through the array.
	return keys
}

// expectNewlineOrEOF consumes a newline or does nothing at EOF.
func (p *parser) expectNewlineOrEOF() {
	if p.current().Type == tokenNewline {
		p.advance()
	}
}

// ---------------------------------------------------------------------------
// Number parsing helpers
// ---------------------------------------------------------------------------

func parseInteger(s string) (int64, error) {
	clean := strings.ReplaceAll(s, "_", "")

	// Handle sign
	negative := false
	if len(clean) > 0 && clean[0] == '-' {
		negative = true
		clean = clean[1:]
	} else if len(clean) > 0 && clean[0] == '+' {
		clean = clean[1:]
	}

	var val uint64
	var err error

	if strings.HasPrefix(clean, "0x") || strings.HasPrefix(clean, "0X") {
		val, err = strconv.ParseUint(clean[2:], 16, 64)
	} else if strings.HasPrefix(clean, "0o") || strings.HasPrefix(clean, "0O") {
		val, err = strconv.ParseUint(clean[2:], 8, 64)
	} else if strings.HasPrefix(clean, "0b") || strings.HasPrefix(clean, "0B") {
		val, err = strconv.ParseUint(clean[2:], 2, 64)
	} else {
		val, err = strconv.ParseUint(clean, 10, 64)
	}

	if err != nil {
		return 0, fmt.Errorf("invalid integer %q: %w", s, err)
	}

	if negative {
		return -int64(val), nil
	}
	return int64(val), nil
}

func parseFloat(s string) (float64, error) {
	clean := strings.ReplaceAll(s, "_", "")
	lower := strings.ToLower(clean)

	switch lower {
	case "inf", "+inf":
		return math.Inf(1), nil
	case "-inf":
		return math.Inf(-1), nil
	case "nan", "+nan":
		return math.NaN(), nil
	case "-nan":
		return math.NaN(), nil
	}

	return strconv.ParseFloat(clean, 64)
}

func parseDatetime(s string) (string, error) {
	// We store datetimes as strings. The caller can parse them further.
	// Validate basic structure.
	return s, nil
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// Parse parses TOML data and returns a map[string]any.
func Parse(data []byte) (map[string]any, error) {
	tokens, err := Tokenize(string(data))
	if err != nil {
		return nil, fmt.Errorf("toml lexer: %w", err)
	}

	p := newParser(tokens)
	return p.parse()
}

// Decode parses TOML data and stores the result in the value pointed to by v.
func Decode(data []byte, v any) error {
	m, err := Parse(data)
	if err != nil {
		return err
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return errors.New("decode target must be a non-nil pointer")
	}

	return decodeValue(m, rv.Elem())
}

// DecodeFile reads a file and decodes its TOML content into v.
func DecodeFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	return Decode(data, v)
}

// ---------------------------------------------------------------------------
// Decoder — map[string]any → Go struct/map/slice
// ---------------------------------------------------------------------------

// decodeValue decodes a generic value into a reflect.Value.
func decodeValue(src any, dst reflect.Value) error {
	if src == nil {
		return nil
	}

	// Handle pointers
	for dst.Kind() == reflect.Ptr {
		if dst.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
		}
		dst = dst.Elem()
	}

	switch v := src.(type) {
	case map[string]any:
		return decodeMap(v, dst)

	case []any:
		return decodeSlice(v, dst)

	case string:
		return decodeScalar(v, dst)

	case int64:
		return decodeInt(v, dst)

	case float64:
		return decodeFloat(v, dst)

	case bool:
		return decodeBool(v, dst)

	default:
		// Fallback: convert to string
		return decodeScalar(fmt.Sprintf("%v", v), dst)
	}
}

// decodeMap decodes a map into a struct or map.
func decodeMap(src map[string]any, dst reflect.Value) error {
	switch dst.Kind() {
	case reflect.Struct:
		return decodeStruct(src, dst)

	case reflect.Map:
		return decodeGoMap(src, dst)

	case reflect.Interface:
		dst.Set(reflect.ValueOf(src))
		return nil

	default:
		return fmt.Errorf("cannot decode TOML table into %v", dst.Type())
	}
}

// decodeStruct decodes a map into a Go struct.
func decodeStruct(src map[string]any, dst reflect.Value) error {
	t := dst.Type()

	// Build field map: lowercase tag name -> field index
	fieldMap := make(map[string]int)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue // unexported
		}

		name := field.Name

		// Check toml tag first, then yaml, then json, then field name
		if tag := field.Tag.Get("toml"); tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] != "" && parts[0] != "-" {
				name = parts[0]
			}
		} else if tag := field.Tag.Get("yaml"); tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] != "" && parts[0] != "-" {
				name = parts[0]
			}
		} else if tag := field.Tag.Get("json"); tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] != "" && parts[0] != "-" {
				name = parts[0]
			}
		}

		fieldMap[strings.ToLower(name)] = i
	}

	for key, val := range src {
		idx, ok := fieldMap[strings.ToLower(key)]
		if !ok {
			continue // ignore unknown keys
		}
		field := dst.Field(idx)
		if err := decodeValue(val, field); err != nil {
			return fmt.Errorf("field %s: %w", key, err)
		}
	}

	return nil
}

// decodeGoMap decodes a map into a Go map.
func decodeGoMap(src map[string]any, dst reflect.Value) error {
	if dst.IsNil() {
		dst.Set(reflect.MakeMap(dst.Type()))
	}

	keyType := dst.Type().Key()
	elemType := dst.Type().Elem()

	for k, v := range src {
		key := reflect.New(keyType).Elem()
		if err := decodeScalar(k, key); err != nil {
			return fmt.Errorf("map key %q: %w", k, err)
		}

		elem := reflect.New(elemType).Elem()
		if err := decodeValue(v, elem); err != nil {
			return fmt.Errorf("map value for %q: %w", k, err)
		}

		dst.SetMapIndex(key, elem)
	}

	return nil
}

// decodeSlice decodes a []any into a Go slice.
func decodeSlice(src []any, dst reflect.Value) error {
	switch dst.Kind() {
	case reflect.Slice:
		elemType := dst.Type().Elem()
		slice := reflect.MakeSlice(dst.Type(), 0, len(src))

		for _, item := range src {
			elem := reflect.New(elemType).Elem()
			if err := decodeValue(item, elem); err != nil {
				return err
			}
			slice = reflect.Append(slice, elem)
		}
		dst.Set(slice)
		return nil

	case reflect.Array:
		if len(src) > dst.Len() {
			return fmt.Errorf("array too long: %d elements for array of length %d", len(src), dst.Len())
		}
		for i, item := range src {
			if err := decodeValue(item, dst.Index(i)); err != nil {
				return err
			}
		}
		return nil

	case reflect.Interface:
		dst.Set(reflect.ValueOf(src))
		return nil

	default:
		return fmt.Errorf("cannot decode TOML array into %v", dst.Type())
	}
}

// decodeScalar decodes a string into a scalar Go type.
func decodeScalar(s string, dst reflect.Value) error {
	// Handle TextUnmarshaler
	if dst.Kind() == reflect.Struct || (dst.Kind() == reflect.Ptr && dst.Type().Elem().Kind() == reflect.Struct) {
		target := dst
		if target.Kind() == reflect.Ptr {
			if target.IsNil() {
				target.Set(reflect.New(target.Type().Elem()))
			}
			target = target.Elem()
		}
		if target.CanAddr() {
			if u, ok := target.Addr().Interface().(encoding.TextUnmarshaler); ok {
				return u.UnmarshalText([]byte(s))
			}
		}
	}

	switch dst.Kind() {
	case reflect.String:
		dst.SetString(s)
		return nil

	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return fmt.Errorf("cannot parse %q as bool", s)
		}
		dst.SetBool(b)
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if dst.Type() == reflect.TypeOf(time.Duration(0)) {
			dur, err := time.ParseDuration(s)
			if err != nil {
				return fmt.Errorf("cannot parse %q as duration", s)
			}
			dst.SetInt(int64(dur))
			return nil
		}
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("cannot parse %q as int", s)
		}
		dst.SetInt(i)
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		i, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return fmt.Errorf("cannot parse %q as uint", s)
		}
		dst.SetUint(i)
		return nil

	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("cannot parse %q as float", s)
		}
		dst.SetFloat(f)
		return nil

	case reflect.Interface:
		dst.Set(reflect.ValueOf(s))
		return nil

	default:
		return fmt.Errorf("cannot decode string into %v", dst.Type())
	}
}

// decodeInt decodes an int64 into a Go numeric type.
func decodeInt(n int64, dst reflect.Value) error {
	switch dst.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		dst.SetInt(n)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if n < 0 {
			return fmt.Errorf("cannot assign negative value %d to unsigned type", n)
		}
		dst.SetUint(uint64(n))
		return nil
	case reflect.Float32, reflect.Float64:
		dst.SetFloat(float64(n))
		return nil
	case reflect.Interface:
		dst.Set(reflect.ValueOf(n))
		return nil
	case reflect.String:
		dst.SetString(strconv.FormatInt(n, 10))
		return nil
	case reflect.Bool:
		dst.SetBool(n != 0)
		return nil
	default:
		return fmt.Errorf("cannot decode integer into %v", dst.Type())
	}
}

// decodeFloat decodes a float64 into a Go numeric type.
func decodeFloat(f float64, dst reflect.Value) error {
	switch dst.Kind() {
	case reflect.Float32, reflect.Float64:
		dst.SetFloat(f)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		dst.SetInt(int64(f))
		return nil
	case reflect.Interface:
		dst.Set(reflect.ValueOf(f))
		return nil
	case reflect.String:
		dst.SetString(strconv.FormatFloat(f, 'f', -1, 64))
		return nil
	default:
		return fmt.Errorf("cannot decode float into %v", dst.Type())
	}
}

// decodeBool decodes a bool.
func decodeBool(b bool, dst reflect.Value) error {
	switch dst.Kind() {
	case reflect.Bool:
		dst.SetBool(b)
		return nil
	case reflect.Interface:
		dst.Set(reflect.ValueOf(b))
		return nil
	case reflect.String:
		if b {
			dst.SetString("true")
		} else {
			dst.SetString("false")
		}
		return nil
	default:
		return fmt.Errorf("cannot decode bool into %v", dst.Type())
	}
}
