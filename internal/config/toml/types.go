// Package toml implements a TOML parser for OpenLoadBalancer configuration.
package toml

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
