package toml

import (
	"encoding"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

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
	for i := range t.NumField() {
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
