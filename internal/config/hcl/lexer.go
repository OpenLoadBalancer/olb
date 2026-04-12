// Package hcl provides an HCL (HashiCorp Configuration Language) parser
// for OpenLoadBalancer. It implements a lexer, parser, and decoder with
// zero external dependencies, producing map[string]any or decoding
// directly into Go structs via reflection.
package hcl

import (
	"fmt"
	"strings"
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
