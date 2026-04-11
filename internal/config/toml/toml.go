// Package toml implements a TOML v1.0 parser with zero external dependencies.
// It provides lexing, parsing, and decoding of TOML documents into Go values.
package toml

import (
	"errors"
	"fmt"
	"os"
	"reflect"
)

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
