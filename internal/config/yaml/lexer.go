package yaml

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// TokenType represents a YAML token type.
type TokenType int

const (
	// Special tokens
	TokenEOF TokenType = iota
	TokenNewline
	TokenIndent
	TokenDedent

	// Literals
	TokenString
	TokenNumber
	TokenBool
	TokenNull

	// Punctuation
	TokenColon      // :
	TokenDash       // -
	TokenComma      // ,
	TokenLBrace     // {
	TokenRBrace     // }
	TokenLBracket   // [
	TokenRBracket   // ]
	TokenPipe       // |
	TokenGreater    // >
	TokenAmpersand  // & (anchor)
	TokenAsterisk   // * (alias)
	TokenExclaim    // ! (tag)
	TokenHash       // # (comment start)
	TokenQuestion   // ? (complex key)
	TokenAt         // @ (reserved)
	TokenBacktick   // ` (reserved)

	// Special
	TokenTag        // !tag
	TokenAnchor     // &anchor
	TokenAlias      // *alias
	TokenComment    // # comment
)

// String returns the token type name.
func (t TokenType) String() string {
	names := []string{
		"EOF", "NEWLINE", "INDENT", "DEDENT",
		"STRING", "NUMBER", "BOOL", "NULL",
		"COLON", "DASH", "COMMA",
		"LBRACE", "RBRACE", "LBRACKET", "RBRACKET",
		"PIPE", "GREATER", "AMPERSAND", "ASTERISK",
		"EXCLAIM", "HASH", "QUESTION", "AT", "BACKTICK",
		"TAG", "ANCHOR", "ALIAS", "COMMENT",
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

// Lexer tokenizes YAML input.
type Lexer struct {
	input  string
	pos    int
	readPos int
	ch     byte
	line   int
	col    int

	// Indentation tracking
	indentStack []int
	lastIndent  int
	pendingDedents int // Number of DEDENT tokens to emit
}

// NewLexer creates a new YAML lexer.
func NewLexer(input string) *Lexer {
	l := &Lexer{
		input:       input,
		line:        1,
		col:         0,
		indentStack: []int{0},
		pendingDedents: 0,
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

// skipWhitespace skips spaces and tabs (but not newlines).
func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' {
		l.readChar()
	}
}

// readIndent reads leading whitespace on a line and returns the indent level.
func (l *Lexer) readIndent() int {
	indent := 0
	for l.ch == ' ' {
		indent++
		l.readChar()
	}
	for l.ch == '\t' {
		indent += 4 // Treat tab as 4 spaces
		l.readChar()
	}
	return indent
}

// NextToken returns the next token.
func (l *Lexer) NextToken() Token {
	// First, emit any pending DEDENT tokens
	if l.pendingDedents > 0 {
		l.pendingDedents--
		// Pop from stack and return the new indent level
		if len(l.indentStack) > 1 {
			l.indentStack = l.indentStack[:len(l.indentStack)-1]
			l.lastIndent = l.indentStack[len(l.indentStack)-1]
		}
		return Token{Type: TokenDedent, Value: strconv.Itoa(l.lastIndent), Line: l.line, Col: 1}
	}

	// Handle newlines and indentation
	if l.ch == '\n' || l.ch == '\r' {
		return l.readNewline()
	}

	// Check if we're at the start of a line (after newline)
	if l.col == 0 || (l.pos > 0 && l.input[l.pos-1] == '\n') {
		// Check for empty/comment-only line first
		if l.ch == '\n' || l.ch == '\r' {
			return l.readNewline()
		}
		if l.ch == '#' {
			l.skipComment()
			return l.NextToken()
		}
		if l.ch == 0 {
			// EOF at start of line - fall through to EOF handling below
		} else {
			indent := l.readIndent()
			if l.ch == '\n' || l.ch == '\r' || l.ch == '#' || l.ch == 0 {
				// Empty line or comment-only line after indent
				if l.ch == '#' {
					l.skipComment()
				}
				return l.NextToken()
			}

			// Generate indent/dedent tokens
			tok := l.handleIndent(indent)
			if tok.Type != TokenEOF {
				return tok
			}
		}
	}

	l.skipWhitespace()

	tok := Token{Line: l.line, Col: l.col}

	switch l.ch {
	case 0:
		tok.Type = TokenEOF
		tok.Value = ""
		// Generate remaining dedents via pendingDedents
		if len(l.indentStack) > 1 {
			l.indentStack = l.indentStack[:len(l.indentStack)-1]
			l.lastIndent = l.indentStack[len(l.indentStack)-1]
			l.pendingDedents = len(l.indentStack) - 1
			tok.Type = TokenDedent
			tok.Value = strconv.Itoa(l.lastIndent)
		}

	case ':':
		tok.Type = TokenColon
		tok.Value = ":"
		l.readChar()

	case '-':
		// Check if it's a list item or part of a number
		if isDigit(l.peek()) {
			return l.readNumber()
		}
		tok.Type = TokenDash
		tok.Value = "-"
		l.readChar()

	case ',':
		tok.Type = TokenComma
		tok.Value = ","
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

	case '|':
		tok.Type = TokenPipe
		tok.Value = "|"
		l.readChar()

	case '>':
		tok.Type = TokenGreater
		tok.Value = ">"
		l.readChar()

	case '&':
		l.readChar()
		tok.Type = TokenAnchor
		tok.Value = l.readIdentifier()

	case '*':
		l.readChar()
		tok.Type = TokenAlias
		tok.Value = l.readIdentifier()

	case '!':
		tok.Type = TokenTag
		tok.Value = l.readTag()

	case '#':
		tok.Type = TokenComment
		tok.Value = l.readComment()

	case '?':
		tok.Type = TokenQuestion
		tok.Value = "?"
		l.readChar()

	case '"':
		return l.readDoubleQuotedString()

	case '\'':
		return l.readSingleQuotedString()

	default:
		if isLetter(l.ch) {
			return l.readIdentifierOrKeyword()
		} else if isDigit(l.ch) || l.ch == '-' {
			return l.readNumber()
		} else if l.ch == '/' || l.ch == '.' {
			// Path-like strings
			tok.Type = TokenString
			tok.Value = l.readIdentifier()
			return tok
		} else {
			// Unknown character, skip it
			l.readChar()
			return l.NextToken()
		}
	}

	return tok
}

// readNewline handles newline characters.
func (l *Lexer) readNewline() Token {
	tok := Token{Type: TokenNewline, Value: "\n", Line: l.line, Col: l.col}

	if l.ch == '\r' {
		l.readChar()
	}
	if l.ch == '\n' {
		l.readChar()
	}
	l.line++
	l.col = 0

	return tok
}

// handleIndent generates INDENT/DEDENT tokens.
func (l *Lexer) handleIndent(indent int) Token {
	if indent > l.lastIndent {
		// Indent
		l.indentStack = append(l.indentStack, indent)
		l.lastIndent = indent
		return Token{Type: TokenIndent, Value: strconv.Itoa(indent), Line: l.line, Col: 1}
	} else if indent < l.lastIndent {
		// Dedent - pop one level at a time
		if len(l.indentStack) > 1 {
			// Count how many levels we need to pop
			popCount := 0
			for i := len(l.indentStack) - 1; i > 0 && l.indentStack[i] > indent; i-- {
				popCount++
			}
			// Pop one level and return its DEDENT
			l.indentStack = l.indentStack[:len(l.indentStack)-1]
			l.lastIndent = l.indentStack[len(l.indentStack)-1]
			l.pendingDedents = popCount - 1 // Remaining dedents to emit
			return Token{Type: TokenDedent, Value: strconv.Itoa(l.lastIndent), Line: l.line, Col: 1}
		}
	}
	return Token{Type: TokenEOF, Value: ""}
}

// skipComment skips a comment to end of line.
func (l *Lexer) skipComment() {
	for l.ch != '\n' && l.ch != '\r' && l.ch != 0 {
		l.readChar()
	}
}

// readComment reads a comment (including #).
func (l *Lexer) readComment() string {
	pos := l.pos
	for l.ch != '\n' && l.ch != '\r' && l.ch != 0 {
		l.readChar()
	}
	return l.input[pos:l.pos]
}

// readIdentifier reads an identifier.
func (l *Lexer) readIdentifier() string {
	pos := l.pos
	for isLetter(l.ch) || isDigit(l.ch) || l.ch == '_' || l.ch == '-' || l.ch == '/' || l.ch == '.' || l.ch == '@' {
		l.readChar()
	}
	return l.input[pos:l.pos]
}

// readIdentifierOrKeyword reads an identifier or keyword.
func (l *Lexer) readIdentifierOrKeyword() Token {
	tok := Token{Line: l.line, Col: l.col}
	value := l.readIdentifier()

	// Check for keywords
	switch strings.ToLower(value) {
	case "true", "yes", "on":
		tok.Type = TokenBool
		tok.Value = "true"
	case "false", "no", "off":
		tok.Type = TokenBool
		tok.Value = "false"
	case "null", "nil", "~":
		tok.Type = TokenNull
		tok.Value = "null"
	default:
		tok.Type = TokenString
		tok.Value = value
	}

	return tok
}

// readNumber reads a number (integer or float), or a duration string like "5s".
func (l *Lexer) readNumber() Token {
	tok := Token{Type: TokenNumber, Line: l.line, Col: l.col}
	pos := l.pos

	// Optional sign
	if l.ch == '-' {
		l.readChar()
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

	// Check for duration suffix (s, m, h, d, ms, us, ns, etc.)
	// If we see a letter after a number, treat the whole thing as a string
	if isLetter(l.ch) {
		for isLetter(l.ch) {
			l.readChar()
		}
		tok.Type = TokenString
	}

	tok.Value = l.input[pos:l.pos]
	return tok
}

// readDoubleQuotedString reads a double-quoted string.
func (l *Lexer) readDoubleQuotedString() Token {
	tok := Token{Type: TokenString, Line: l.line, Col: l.col}
	l.readChar() // skip opening quote

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
			default:
				result.WriteByte(l.ch)
			}
		} else {
			result.WriteByte(l.ch)
		}
		l.readChar()
	}

	if l.ch == '"' {
		l.readChar() // skip closing quote
	}

	tok.Value = result.String()
	return tok
}

// readSingleQuotedString reads a single-quoted string.
func (l *Lexer) readSingleQuotedString() Token {
	tok := Token{Type: TokenString, Line: l.line, Col: l.col}
	l.readChar() // skip opening quote

	var result strings.Builder
	for l.ch != '\'' && l.ch != 0 {
		if l.ch == '\'' && l.peek() == '\'' {
			// Escaped quote ('')
			result.WriteByte('\'')
			l.readChar()
			l.readChar()
		} else {
			result.WriteByte(l.ch)
			l.readChar()
		}
	}

	if l.ch == '\'' {
		l.readChar() // skip closing quote
	}

	tok.Value = result.String()
	return tok
}

// readTag reads a tag (starting with !).
func (l *Lexer) readTag() string {
	pos := l.pos
	l.readChar() // skip !
	for isLetter(l.ch) || isDigit(l.ch) || l.ch == '_' || l.ch == '-' || l.ch == '.' {
		l.readChar()
	}
	return l.input[pos:l.pos]
}

// isLetter checks if a character is a letter.
func isLetter(ch byte) bool {
	return unicode.IsLetter(rune(ch))
}

// isDigit checks if a character is a digit.
func isDigit(ch byte) bool {
	return unicode.IsDigit(rune(ch))
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
