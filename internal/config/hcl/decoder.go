package hcl

import (
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

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

	for i := range t.NumField() {
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

		for i := range src.Len() {
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

	case reflect.Pointer:
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
