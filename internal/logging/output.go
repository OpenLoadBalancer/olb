package logging

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// jsonBufferSize is the initial buffer size for JSON encoding.
const jsonBufferSize = 1024

// JSONOutput writes log entries as JSON.
type JSONOutput struct {
	w   io.Writer
	mu  sync.Mutex
	buf []byte
}

// NewJSONOutput creates a new JSON output.
func NewJSONOutput(w io.Writer) *JSONOutput {
	return &JSONOutput{
		w:   w,
		buf: make([]byte, 0, jsonBufferSize),
	}
}

// Write writes a log entry as JSON.
func (o *JSONOutput) Write(level Level, msg string, fields []Field) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Reset buffer
	o.buf = o.buf[:0]

	// Start object
	o.buf = append(o.buf, '{')

	// Timestamp
	o.buf = appendJSONKey(o.buf, "time")
	o.buf = append(o.buf, '"')
	o.buf = time.Now().UTC().AppendFormat(o.buf, time.RFC3339Nano)
	o.buf = append(o.buf, '"', ',')

	// Level
	o.buf = appendJSONKey(o.buf, "level")
	o.buf = append(o.buf, '"')
	o.buf = append(o.buf, level.String()...)
	o.buf = append(o.buf, '"', ',')

	// Message
	o.buf = appendJSONKey(o.buf, "msg")
	o.buf = appendJSONString(o.buf, msg)

	// Fields
	for _, f := range fields {
		o.buf = append(o.buf, ',')
		o.buf = appendJSONKey(o.buf, f.Key)
		o.buf = appendJSONValue(o.buf, f.Value)
	}

	// Close object
	o.buf = append(o.buf, '}', '\n')

	// Write
	_, _ = o.w.Write(o.buf)
}

// Close closes the output.
func (o *JSONOutput) Close() error {
	return nil
}

// appendJSONKey appends a JSON key.
func appendJSONKey(buf []byte, key string) []byte {
	buf = append(buf, '"')
	buf = append(buf, key...)
	buf = append(buf, '"', ':')
	return buf
}

// appendJSONString appends a JSON string with proper escaping.
func appendJSONString(buf []byte, s string) []byte {
	buf = append(buf, '"')
	for i := range len(s) {
		c := s[i]
		switch c {
		case '"':
			buf = append(buf, '\\', '"')
		case '\\':
			buf = append(buf, '\\', '\\')
		case '\n':
			buf = append(buf, '\\', 'n')
		case '\r':
			buf = append(buf, '\\', 'r')
		case '\t':
			buf = append(buf, '\\', 't')
		default:
			if c < 0x20 {
				buf = append(buf, '\\', 'u', '0', '0',
					"0123456789abcdef"[c>>4],
					"0123456789abcdef"[c&0xF])
			} else {
				buf = append(buf, c)
			}
		}
	}
	buf = append(buf, '"')
	return buf
}

// appendJSONValue appends a JSON value based on type.
func appendJSONValue(buf []byte, v any) []byte {
	switch val := v.(type) {
	case string:
		return appendJSONString(buf, val)
	case int:
		return strconv.AppendInt(buf, int64(val), 10)
	case int8:
		return strconv.AppendInt(buf, int64(val), 10)
	case int16:
		return strconv.AppendInt(buf, int64(val), 10)
	case int32:
		return strconv.AppendInt(buf, int64(val), 10)
	case int64:
		return strconv.AppendInt(buf, val, 10)
	case uint:
		return strconv.AppendUint(buf, uint64(val), 10)
	case uint8:
		return strconv.AppendUint(buf, uint64(val), 10)
	case uint16:
		return strconv.AppendUint(buf, uint64(val), 10)
	case uint32:
		return strconv.AppendUint(buf, uint64(val), 10)
	case uint64:
		return strconv.AppendUint(buf, val, 10)
	case float32:
		return strconv.AppendFloat(buf, float64(val), 'f', -1, 32)
	case float64:
		return strconv.AppendFloat(buf, val, 'f', -1, 64)
	case bool:
		return strconv.AppendBool(buf, val)
	case time.Duration:
		return appendJSONString(buf, val.String())
	case time.Time:
		buf = append(buf, '"')
		buf = val.UTC().AppendFormat(buf, time.RFC3339Nano)
		buf = append(buf, '"')
		return buf
	case error:
		if val != nil {
			return appendJSONString(buf, val.Error())
		}
		return append(buf, "null"...)
	case nil:
		return append(buf, "null"...)
	default:
		return appendJSONString(buf, fmt.Sprintf("%v", v))
	}
}

// TextOutput writes log entries in human-readable format.
type TextOutput struct {
	w  io.Writer
	mu sync.Mutex
}

// NewTextOutput creates a new text output.
func NewTextOutput(w io.Writer) *TextOutput {
	return &TextOutput{w: w}
}

// Write writes a log entry as text.
func (o *TextOutput) Write(level Level, msg string, fields []Field) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Format: 2006-01-02 15:04:05.000 LEVEL message key=value key=value
	timestamp := time.Now().UTC().Format("2006-01-02 15:04:05.000")

	var buf strings.Builder
	buf.Grow(256)

	buf.WriteString(timestamp)
	buf.WriteByte(' ')
	buf.WriteString(level.String())
	buf.WriteByte(' ')
	buf.WriteString(msg)

	for _, f := range fields {
		buf.WriteByte(' ')
		buf.WriteString(f.Key)
		buf.WriteByte('=')
		buf.WriteString(formatValue(f.Value))
	}

	buf.WriteByte('\n')

	_, _ = o.w.Write([]byte(buf.String()))
}

// Close closes the output.
func (o *TextOutput) Close() error {
	return nil
}

// formatValue formats a value for text output.
func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		// Quote if contains spaces or special chars
		if strings.ContainsAny(val, " =\t\n\r") {
			return fmt.Sprintf("%q", val)
		}
		return val
	case error:
		if val != nil {
			return val.Error()
		}
		return "null"
	case time.Duration:
		return val.String()
	case time.Time:
		return val.UTC().Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// MultiOutput fans out to multiple outputs.
type MultiOutput struct {
	outputs []Output
}

// NewMultiOutput creates a new multi output.
func NewMultiOutput(outputs ...Output) *MultiOutput {
	return &MultiOutput{outputs: outputs}
}

// Write writes to all outputs.
func (o *MultiOutput) Write(level Level, msg string, fields []Field) {
	for _, out := range o.outputs {
		out.Write(level, msg, fields)
	}
}

// Close closes all outputs.
func (o *MultiOutput) Close() error {
	var firstErr error
	for _, out := range o.outputs {
		if err := out.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// RotatingFileOutput writes to a file with rotation.
type RotatingFileOutput struct {
	filename   string
	maxSize    int64
	maxBackups int
	compress   bool

	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer
	size   int64
}

// RotatingFileOptions configures the rotating file output.
type RotatingFileOptions struct {
	Filename   string
	MaxSize    int64 // bytes
	MaxBackups int
	Compress   bool
}

// NewRotatingFileOutput creates a new rotating file output.
func NewRotatingFileOutput(opts RotatingFileOptions) (*RotatingFileOutput, error) {
	if opts.MaxSize <= 0 {
		opts.MaxSize = 100 * 1024 * 1024 // 100MB default
	}
	if opts.MaxBackups <= 0 {
		opts.MaxBackups = 10
	}

	o := &RotatingFileOutput{
		filename:   opts.Filename,
		maxSize:    opts.MaxSize,
		maxBackups: opts.MaxBackups,
		compress:   opts.Compress,
	}

	if err := o.open(); err != nil {
		return nil, err
	}

	return o, nil
}

// open opens or creates the log file.
func (o *RotatingFileOutput) open() error {
	// Create directory if needed
	dir := filepath.Dir(o.filename)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	// Open file
	file, err := os.OpenFile(o.filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	// Get current size
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}

	o.file = file
	o.writer = bufio.NewWriter(file)
	o.size = stat.Size()

	return nil
}

// Write writes to the file, rotating if necessary.
func (o *RotatingFileOutput) Write(level Level, msg string, fields []Field) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Check if rotation needed
	if o.size >= o.maxSize {
		if err := o.rotate(); err != nil {
			// Write to stderr as fallback
			fmt.Fprintf(os.Stderr, "log rotation failed: %v\n", err)
		}
	}

	// Format and write
	timestamp := time.Now().UTC().Format("2006-01-02 15:04:05.000")

	o.writer.WriteString(timestamp)
	o.writer.WriteByte(' ')
	o.writer.WriteString(level.String())
	o.writer.WriteByte(' ')
	o.writer.WriteString(msg)

	for _, f := range fields {
		o.writer.WriteByte(' ')
		o.writer.WriteString(f.Key)
		o.writer.WriteByte('=')
		o.writer.WriteString(formatValue(f.Value))
	}

	o.writer.WriteByte('\n')

	// Track bytes written before Flush clears the buffer
	o.size += int64(o.writer.Buffered())
	o.writer.Flush()
}

// rotate performs log rotation.
func (o *RotatingFileOutput) rotate() error {
	// Flush and close current file
	o.writer.Flush()
	o.file.Close()

	// Remove oldest backup if exists
	oldest := o.filename + "." + strconv.Itoa(o.maxBackups)
	if o.compress {
		oldest += ".gz"
	}
	os.Remove(oldest)

	// Shift backups
	for i := o.maxBackups - 1; i >= 1; i-- {
		oldName := o.filename + "." + strconv.Itoa(i)
		newName := o.filename + "." + strconv.Itoa(i+1)
		if o.compress {
			oldName += ".gz"
			newName += ".gz"
		}
		os.Rename(oldName, newName)
	}

	// Move current file to .1
	if o.compress {
		if err := o.compressFile(o.filename, o.filename+".1.gz"); err != nil {
			// Fallback to rename without compression
			os.Rename(o.filename, o.filename+".1")
		}
	} else {
		os.Rename(o.filename, o.filename+".1")
	}

	// Reopen
	return o.open()
}

// compressFile compresses a file to gzip.
func (o *RotatingFileOutput) compressFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}

	gzWriter := gzip.NewWriter(dstFile)

	_, err = io.Copy(gzWriter, srcFile)
	if err != nil {
		gzWriter.Close()
		dstFile.Close()
		return err
	}

	// Close gzip writer first to flush the footer
	if err := gzWriter.Close(); err != nil {
		dstFile.Close()
		return err
	}

	// Flush and close destination file
	if err := dstFile.Close(); err != nil {
		return err
	}

	// Remove source file only after successful compression
	return os.Remove(src)
}

// Reopen reopens the log file (for SIGUSR1 handling).
func (o *RotatingFileOutput) Reopen() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.writer.Flush()
	o.file.Close()
	return o.open()
}

// Close closes the file.
func (o *RotatingFileOutput) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.writer != nil {
		o.writer.Flush()
	}
	if o.file != nil {
		return o.file.Close()
	}
	return nil
}
