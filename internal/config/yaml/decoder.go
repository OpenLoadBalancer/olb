package yaml

import (
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Decoder decodes YAML nodes into Go values.
type Decoder struct {
	node *Node
}

// NewDecoder creates a new decoder.
func NewDecoder(node *Node) *Decoder {
	return &Decoder{node: node}
}

// Decode decodes the node into the target value.
func (d *Decoder) Decode(v interface{}) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return errors.New("decode target must be a non-nil pointer")
	}

	return d.decode(d.node, rv.Elem())
}

// decode decodes a node into a reflect.Value.
func (d *Decoder) decode(node *Node, v reflect.Value) error {
	if node == nil {
		return nil
	}

	// Handle pointers
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return d.decode(node, v.Elem())
	}

	switch node.Type {
	case NodeDocument:
		if len(node.Children) > 0 {
			return d.decode(node.Children[0], v)
		}
		return nil

	case NodeMapping:
		return d.decodeMapping(node, v)

	case NodeSequence:
		return d.decodeSequence(node, v)

	case NodeScalar:
		return d.decodeScalar(node.Value, v)

	case NodeAlias:
		// Aliases would be resolved here
		return d.decodeScalar(node.Value, v)

	default:
		return fmt.Errorf("unknown node type: %v", node.Type)
	}
}

// decodeMapping decodes a mapping node.
func (d *Decoder) decodeMapping(node *Node, v reflect.Value) error {
	switch v.Kind() {
	case reflect.Struct:
		return d.decodeStruct(node, v)

	case reflect.Map:
		return d.decodeMap(node, v)

	case reflect.Interface:
		// Create a map[string]interface{}
		m := make(map[string]interface{})
		mv := reflect.ValueOf(&m).Elem()
		if err := d.decodeMapToInterface(node, mv); err != nil {
			return err
		}
		v.Set(mv)
		return nil

	default:
		return fmt.Errorf("cannot decode mapping into %v", v.Type())
	}
}

// decodeStruct decodes a mapping into a struct.
func (d *Decoder) decodeStruct(node *Node, v reflect.Value) error {
	t := v.Type()

	// Build field map
	fieldMap := make(map[string]int)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" { // Skip unexported fields
			continue
		}

		// Get field name from tag or use field name
		name := field.Name
		tag := field.Tag.Get("yaml")
		if tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] != "" && parts[0] != "-" {
				name = parts[0]
			}
		} else {
			// Try json tag as fallback
			tag = field.Tag.Get("json")
			if tag != "" {
				parts := strings.Split(tag, ",")
				if parts[0] != "" && parts[0] != "-" {
					name = parts[0]
				}
			}
		}

		// Store lowercase version for case-insensitive matching
		fieldMap[strings.ToLower(name)] = i
	}

	// Decode each field
	for _, child := range node.Children {
		if child.Key == "" {
			continue
		}

		key := strings.ToLower(child.Key)
		if idx, ok := fieldMap[key]; ok {
			field := v.Field(idx)
			if err := d.decode(child, field); err != nil {
				return fmt.Errorf("field %s: %w", child.Key, err)
			}
		}
	}

	return nil
}

// decodeMap decodes a mapping into a map.
func (d *Decoder) decodeMap(node *Node, v reflect.Value) error {
	if v.IsNil() {
		v.Set(reflect.MakeMap(v.Type()))
	}

	keyType := v.Type().Key()
	elemType := v.Type().Elem()

	for _, child := range node.Children {
		if child.Key == "" {
			continue
		}

		// Create key
		key := reflect.New(keyType).Elem()
		if err := d.decodeScalar(child.Key, key); err != nil {
			return fmt.Errorf("map key %s: %w", child.Key, err)
		}

		// Create value
		elem := reflect.New(elemType).Elem()
		if err := d.decode(child, elem); err != nil {
			return fmt.Errorf("map value for %s: %w", child.Key, err)
		}

		v.SetMapIndex(key, elem)
	}

	return nil
}

// decodeMapToInterface decodes a mapping into map[string]interface{}.
func (d *Decoder) decodeMapToInterface(node *Node, v reflect.Value) error {
	m := make(map[string]interface{})

	for _, child := range node.Children {
		if child.Key == "" {
			continue
		}

		var val interface{}

		// Decode based on node type
		switch child.Type {
		case NodeMapping:
			childMap := make(map[string]interface{})
			childV := reflect.ValueOf(&childMap).Elem()
			if err := d.decodeMapToInterface(child, childV); err != nil {
				return err
			}
			val = childMap

		case NodeSequence:
			var arr []interface{}
			childV := reflect.ValueOf(&arr).Elem()
			if err := d.decodeSequenceToInterface(child, childV); err != nil {
				return err
			}
			val = arr

		case NodeScalar:
			val = child.Value

		default:
			val = child.Value
		}

		m[child.Key] = val
	}

	v.Set(reflect.ValueOf(m))
	return nil
}

// decodeSequence decodes a sequence node.
func (d *Decoder) decodeSequence(node *Node, v reflect.Value) error {
	switch v.Kind() {
	case reflect.Slice:
		return d.decodeSlice(node, v)

	case reflect.Array:
		return d.decodeArray(node, v)

	case reflect.Interface:
		// Create a []interface{}
		arr := make([]interface{}, 0, len(node.Children))
		arrV := reflect.ValueOf(&arr).Elem()
		if err := d.decodeSequenceToInterface(node, arrV); err != nil {
			return err
		}
		v.Set(arrV)
		return nil

	default:
		return fmt.Errorf("cannot decode sequence into %v", v.Type())
	}
}

// decodeSlice decodes a sequence into a slice.
func (d *Decoder) decodeSlice(node *Node, v reflect.Value) error {
	elemType := v.Type().Elem()
	slice := reflect.MakeSlice(v.Type(), 0, len(node.Children))

	for _, child := range node.Children {
		elem := reflect.New(elemType).Elem()
		if err := d.decode(child, elem); err != nil {
			return err
		}
		slice = reflect.Append(slice, elem)
	}

	v.Set(slice)
	return nil
}

// decodeArray decodes a sequence into an array.
func (d *Decoder) decodeArray(node *Node, v reflect.Value) error {
	if len(node.Children) > v.Len() {
		return fmt.Errorf("sequence too long for array")
	}

	for i, child := range node.Children {
		if err := d.decode(child, v.Index(i)); err != nil {
			return err
		}
	}

	return nil
}

// decodeSequenceToInterface decodes a sequence into []interface{}.
func (d *Decoder) decodeSequenceToInterface(node *Node, v reflect.Value) error {
	arr := make([]interface{}, 0, len(node.Children))

	for _, child := range node.Children {
		var val interface{}

		switch child.Type {
		case NodeMapping:
			childMap := make(map[string]interface{})
			childV := reflect.ValueOf(&childMap).Elem()
			if err := d.decodeMapToInterface(child, childV); err != nil {
				return err
			}
			val = childMap

		case NodeSequence:
			nestedArr := make([]interface{}, 0)
			childV := reflect.ValueOf(&nestedArr).Elem()
			if err := d.decodeSequenceToInterface(child, childV); err != nil {
				return err
			}
			val = nestedArr

		case NodeScalar:
			val = child.Value

		default:
			val = child.Value
		}

		arr = append(arr, val)
	}

	v.Set(reflect.ValueOf(arr))
	return nil
}

// decodeScalar decodes a scalar value.
func (d *Decoder) decodeScalar(s string, v reflect.Value) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
		return nil

	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return fmt.Errorf("cannot parse %q as bool", s)
		}
		v.SetBool(b)
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// Try duration parsing for time.Duration
		if v.Type() == reflect.TypeOf(time.Duration(0)) {
			dur, err := time.ParseDuration(s)
			if err != nil {
				return fmt.Errorf("cannot parse %q as duration", s)
			}
			v.SetInt(int64(dur))
			return nil
		}

		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("cannot parse %q as int", s)
		}
		v.SetInt(i)
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		i, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return fmt.Errorf("cannot parse %q as uint", s)
		}
		v.SetUint(i)
		return nil

	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("cannot parse %q as float", s)
		}
		v.SetFloat(f)
		return nil

	case reflect.Interface:
		// Try to guess the type
		if val := guessType(s); val != nil {
			v.Set(reflect.ValueOf(val))
		} else {
			v.Set(reflect.ValueOf(s))
		}
		return nil

	case reflect.Ptr:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return d.decodeScalar(s, v.Elem())

	case reflect.Struct:
		// Check for TextUnmarshaler
		if v.CanAddr() {
			if u, ok := v.Addr().Interface().(encoding.TextUnmarshaler); ok {
				return u.UnmarshalText([]byte(s))
			}
		}
		return fmt.Errorf("cannot decode scalar into %v", v.Type())

	default:
		return fmt.Errorf("cannot decode scalar into %v", v.Type())
	}
}

// guessType tries to guess the type of a string value.
func guessType(s string) interface{} {
	// Try integer
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}

	// Try float
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}

	// Try bool
	if b, err := strconv.ParseBool(s); err == nil {
		return b
	}

	return nil
}

// Unmarshal parses YAML data and stores the result in the value pointed to by v.
func Unmarshal(data []byte, v interface{}) error {
	node, err := Parse(string(data))
	if err != nil {
		return err
	}

	decoder := NewDecoder(node)
	return decoder.Decode(v)
}

// UnmarshalString parses YAML string and stores the result in v.
func UnmarshalString(s string, v interface{}) error {
	return Unmarshal([]byte(s), v)
}
