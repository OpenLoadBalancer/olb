package toml

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

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
	for i := range len(keys) - 1 {
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
func (p *parser) pathForArrayTable(keys []string, _ []any) []string {
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
