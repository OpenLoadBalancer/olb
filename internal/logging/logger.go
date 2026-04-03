// Package logging provides a high-performance structured logger for OpenLoadBalancer.
// It features zero-allocation hot paths, manual JSON encoding, log rotation,
// and multiple output formats.
package logging

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Level represents a logging level.
type Level int32

const (
	// TraceLevel is the most verbose level.
	TraceLevel Level = 0
	// DebugLevel is for debugging information.
	DebugLevel Level = 1
	// InfoLevel is for informational messages.
	InfoLevel Level = 2
	// WarnLevel is for warning messages.
	WarnLevel Level = 3
	// ErrorLevel is for error messages.
	ErrorLevel Level = 4
	// FatalLevel is for fatal errors that will terminate the program.
	FatalLevel Level = 5
	// SilentLevel disables all logging.
	SilentLevel Level = 6
)

// String returns the string representation of the level.
func (l Level) String() string {
	switch l {
	case TraceLevel:
		return "TRACE"
	case DebugLevel:
		return "DEBUG"
	case InfoLevel:
		return "INFO"
	case WarnLevel:
		return "WARN"
	case ErrorLevel:
		return "ERROR"
	case FatalLevel:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// MarshalJSON implements json.Marshaler.
func (l Level) MarshalJSON() ([]byte, error) {
	return []byte("\"" + l.String() + "\""), nil
}

// ParseLevel parses a level string.
func ParseLevel(s string) Level {
	switch strings.ToUpper(s) {
	case "TRACE":
		return TraceLevel
	case "DEBUG":
		return DebugLevel
	case "INFO":
		return InfoLevel
	case "WARN", "WARNING":
		return WarnLevel
	case "ERROR":
		return ErrorLevel
	case "FATAL":
		return FatalLevel
	case "SILENT":
		return SilentLevel
	default:
		return InfoLevel
	}
}

// Field represents a structured logging field.
type Field struct {
	Key   string
	Value any
}

// String creates a string field.
func String(key, value string) Field {
	return Field{Key: key, Value: value}
}

// Int creates an int field.
func Int(key string, value int) Field {
	return Field{Key: key, Value: value}
}

// Int64 creates an int64 field.
func Int64(key string, value int64) Field {
	return Field{Key: key, Value: value}
}

// Uint64 creates a uint64 field.
func Uint64(key string, value uint64) Field {
	return Field{Key: key, Value: value}
}

// Float64 creates a float64 field.
func Float64(key string, value float64) Field {
	return Field{Key: key, Value: value}
}

// Bool creates a bool field.
func Bool(key string, value bool) Field {
	return Field{Key: key, Value: value}
}

// Error creates an error field.
func Error(err error) Field {
	return Field{Key: "error", Value: err}
}

// Duration creates a duration field.
func Duration(key string, value time.Duration) Field {
	return Field{Key: key, Value: value}
}

// Time creates a time field.
func Time(key string, value time.Time) Field {
	return Field{Key: key, Value: value}
}

// Any creates a field with any value.
func Any(key string, value any) Field {
	return Field{Key: key, Value: value}
}

// Logger is a structured logger with high-performance output.
type Logger struct {
	level  atomic.Int32
	output Output
	fields []Field
	mu     sync.RWMutex
	name   string
}

// Output is the interface for log outputs.
type Output interface {
	Write(level Level, msg string, fields []Field)
	Close() error
}

// New creates a new Logger with the given output.
func New(output Output) *Logger {
	l := &Logger{
		output: output,
		fields: nil,
	}
	l.level.Store(int32(InfoLevel))
	return l
}

// NewWithDefaults creates a new Logger with default settings.
func NewWithDefaults() *Logger {
	return New(NewTextOutput(os.Stdout))
}

// SetLevel sets the minimum log level.
func (l *Logger) SetLevel(level Level) {
	l.level.Store(int32(level))
}

// Level returns the current log level.
func (l *Logger) Level() Level {
	return Level(l.level.Load())
}

// Enabled returns true if the given level is enabled.
func (l *Logger) Enabled(level Level) bool {
	return level >= l.Level()
}

// With creates a child logger with additional fields.
func (l *Logger) With(fields ...Field) *Logger {
	l.mu.RLock()
	parentFields := l.fields
	l.mu.RUnlock()

	// Combine parent and new fields
	combined := make([]Field, len(parentFields)+len(fields))
	copy(combined, parentFields)
	copy(combined[len(parentFields):], fields)

	child := &Logger{
		output: l.output,
		fields: combined,
		name:   l.name,
	}
	child.level.Store(l.level.Load())
	return child
}

// WithName creates a child logger with a name.
func (l *Logger) WithName(name string) *Logger {
	child := l.With()
	child.name = name
	return child
}

// log is the internal logging method.
func (l *Logger) log(level Level, msg string, fields []Field) {
	// Fast path: check level before allocation
	if level < l.Level() {
		return
	}

	// Combine logger fields with message fields
	var combined []Field
	l.mu.RLock()
	if len(l.fields) > 0 {
		combined = make([]Field, len(l.fields)+len(fields))
		copy(combined, l.fields)
		copy(combined[len(l.fields):], fields)
	} else {
		combined = fields
	}
	l.mu.RUnlock()

	// Add name if set
	if l.name != "" {
		combined = append([]Field{String("logger", l.name)}, combined...)
	}

	// Write to output
	l.output.Write(level, msg, combined)

	// Fatal exits
	if level == FatalLevel {
		os.Exit(1)
	}
}

// Trace logs a trace message.
func (l *Logger) Trace(msg string, fields ...Field) {
	l.log(TraceLevel, msg, fields)
}

// Debug logs a debug message.
func (l *Logger) Debug(msg string, fields ...Field) {
	l.log(DebugLevel, msg, fields)
}

// Info logs an info message.
func (l *Logger) Info(msg string, fields ...Field) {
	l.log(InfoLevel, msg, fields)
}

// Warn logs a warning message.
func (l *Logger) Warn(msg string, fields ...Field) {
	l.log(WarnLevel, msg, fields)
}

// Error logs an error message.
func (l *Logger) Error(msg string, fields ...Field) {
	l.log(ErrorLevel, msg, fields)
}

// Fatal logs a fatal message and exits.
func (l *Logger) Fatal(msg string, fields ...Field) {
	l.log(FatalLevel, msg, fields)
}

// Tracef logs a formatted trace message.
func (l *Logger) Tracef(format string, args ...any) {
	if l.Enabled(TraceLevel) {
		l.Trace(fmt.Sprintf(format, args...))
	}
}

// Debugf logs a formatted debug message.
func (l *Logger) Debugf(format string, args ...any) {
	if l.Enabled(DebugLevel) {
		l.Debug(fmt.Sprintf(format, args...))
	}
}

// Infof logs a formatted info message.
func (l *Logger) Infof(format string, args ...any) {
	if l.Enabled(InfoLevel) {
		l.Info(fmt.Sprintf(format, args...))
	}
}

// Warnf logs a formatted warning message.
func (l *Logger) Warnf(format string, args ...any) {
	if l.Enabled(WarnLevel) {
		l.Warn(fmt.Sprintf(format, args...))
	}
}

// Errorf logs a formatted error message.
func (l *Logger) Errorf(format string, args ...any) {
	if l.Enabled(ErrorLevel) {
		l.Error(fmt.Sprintf(format, args...))
	}
}

// Fatalf logs a formatted fatal message and exits.
func (l *Logger) Fatalf(format string, args ...any) {
	l.Fatal(fmt.Sprintf(format, args...))
}

// Close closes the logger and its output.
func (l *Logger) Close() error {
	if l.output != nil {
		return l.output.Close()
	}
	return nil
}
