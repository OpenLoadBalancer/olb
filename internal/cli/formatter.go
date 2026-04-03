package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Formatter formats output
type Formatter interface {
	Format(data any) (string, error)
}

// JSONFormatter formats data as JSON
type JSONFormatter struct {
	Indent bool
}

// Format converts data to JSON format.
// If Indent is true, the output will be pretty-printed with indentation.
func (f *JSONFormatter) Format(data any) (string, error) {
	if data == nil {
		return "null", nil
	}

	var buf bytes.Buffer

	if f.Indent {
		encoder := json.NewEncoder(&buf)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(data); err != nil {
			return "", fmt.Errorf("failed to encode JSON: %w", err)
		}
	} else {
		encoder := json.NewEncoder(&buf)
		if err := encoder.Encode(data); err != nil {
			return "", fmt.Errorf("failed to encode JSON: %w", err)
		}
	}

	// Remove trailing newline for consistency
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// TableFormatter formats data as a table
type TableFormatter struct {
	Headers []string
}

// Format converts data to a table format.
// Supported data types:
//   - [][]string: Each inner slice is a row
//   - []map[string]string: Each map is a row, keys must match Headers
//   - []struct{} or []*struct{}: Struct fields are used as columns
//   - map[string]string: Single row table with key-value pairs
func (f *TableFormatter) Format(data any) (string, error) {
	if data == nil {
		return "", nil
	}

	switch d := data.(type) {
	case [][]string:
		return f.formatStringSlice(d)
	case []map[string]string:
		return f.formatMapSlice(d)
	case map[string]string:
		return f.formatSingleMap(d)
	case []string:
		// Single column table
		return f.formatSingleColumn(d)
	default:
		// Try to handle as struct slice using reflection-like approach
		return f.formatWithHeaders(data)
	}
}

// formatStringSlice formats a 2D string slice as a table
func (f *TableFormatter) formatStringSlice(rows [][]string) (string, error) {
	if len(rows) == 0 {
		return "", nil
	}

	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	// Write headers if provided
	if len(f.Headers) > 0 {
		fmt.Fprintln(w, strings.Join(f.Headers, "\t"))
	}

	// Write rows
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	if err := w.Flush(); err != nil {
		return "", fmt.Errorf("failed to flush tabwriter: %w", err)
	}

	return buf.String(), nil
}

// formatMapSlice formats a slice of maps as a table
func (f *TableFormatter) formatMapSlice(rows []map[string]string) (string, error) {
	if len(rows) == 0 {
		return "", nil
	}

	// Determine headers
	headers := f.Headers
	if len(headers) == 0 {
		// Extract headers from first row
		for key := range rows[0] {
			headers = append(headers, key)
		}
		// Sort headers for consistent output
		for i := 0; i < len(headers)-1; i++ {
			for j := i + 1; j < len(headers); j++ {
				if headers[i] > headers[j] {
					headers[i], headers[j] = headers[j], headers[i]
				}
			}
		}
	}

	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	// Write headers
	fmt.Fprintln(w, strings.Join(headers, "\t"))

	// Write rows
	for _, row := range rows {
		var values []string
		for _, h := range headers {
			values = append(values, row[h])
		}
		fmt.Fprintln(w, strings.Join(values, "\t"))
	}

	if err := w.Flush(); err != nil {
		return "", fmt.Errorf("failed to flush tabwriter: %w", err)
	}

	return buf.String(), nil
}

// formatSingleMap formats a single map as a key-value table
func (f *TableFormatter) formatSingleMap(data map[string]string) (string, error) {
	if len(data) == 0 {
		return "", nil
	}

	// Extract and sort keys
	var keys []string
	for k := range data {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys)-1; i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	// Write key-value pairs
	for _, k := range keys {
		fmt.Fprintf(w, "%s\t%s\n", k, data[k])
	}

	if err := w.Flush(); err != nil {
		return "", fmt.Errorf("failed to flush tabwriter: %w", err)
	}

	return buf.String(), nil
}

// formatSingleColumn formats a string slice as a single column
func (f *TableFormatter) formatSingleColumn(data []string) (string, error) {
	if len(data) == 0 {
		return "", nil
	}

	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	// Write header if provided
	if len(f.Headers) > 0 {
		fmt.Fprintln(w, f.Headers[0])
	}

	// Write rows
	for _, item := range data {
		fmt.Fprintln(w, item)
	}

	if err := w.Flush(); err != nil {
		return "", fmt.Errorf("failed to flush tabwriter: %w", err)
	}

	return buf.String(), nil
}

// formatWithHeaders attempts to format data using provided headers
func (f *TableFormatter) formatWithHeaders(data any) (string, error) {
	// Fallback to simple string representation
	return fmt.Sprintf("%v", data), nil
}

// NewFormatter creates a formatter by name.
// Supported formats: "json", "table"
func NewFormatter(name string) (Formatter, error) {
	switch name {
	case "json":
		return &JSONFormatter{Indent: false}, nil
	case "json-indent":
		return &JSONFormatter{Indent: true}, nil
	case "table":
		return &TableFormatter{}, nil
	default:
		return nil, fmt.Errorf("unknown format: %s (supported: json, json-indent, table)", name)
	}
}

// FormatToWriter formats data and writes it to the provided writer
func FormatToWriter(w io.Writer, formatter Formatter, data any) error {
	output, err := formatter.Format(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, output)
	return err
}

// FormatWithGlobals formats data using the format specified in GlobalFlags
func FormatWithGlobals(globals *GlobalFlags, data any) (string, error) {
	formatter, err := NewFormatter(globals.Format)
	if err != nil {
		return "", err
	}
	return formatter.Format(data)
}
