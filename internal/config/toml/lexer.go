package toml

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

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
	for range n {
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
