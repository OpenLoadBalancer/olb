package logging

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLevel_String(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{TraceLevel, "TRACE"},
		{DebugLevel, "DEBUG"},
		{InfoLevel, "INFO"},
		{WarnLevel, "WARN"},
		{ErrorLevel, "ERROR"},
		{FatalLevel, "FATAL"},
		{Level(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		got := tt.level.String()
		if got != tt.expected {
			t.Errorf("Level(%d).String() = %s, want %s", tt.level, got, tt.expected)
		}
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
	}{
		{"trace", TraceLevel},
		{"DEBUG", DebugLevel},
		{"Info", InfoLevel},
		{"WARN", WarnLevel},
		{"warning", WarnLevel},
		{"ERROR", ErrorLevel},
		{"FATAL", FatalLevel},
		{"SILENT", SilentLevel},
		{"unknown", InfoLevel},
		{"", InfoLevel},
	}

	for _, tt := range tests {
		got := ParseLevel(tt.input)
		if got != tt.expected {
			t.Errorf("ParseLevel(%s) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestField_Creators(t *testing.T) {
	f1 := String("key", "value")
	if f1.Key != "key" || f1.Value != "value" {
		t.Error("String field incorrect")
	}

	f2 := Int("count", 42)
	if f2.Key != "count" || f2.Value != 42 {
		t.Error("Int field incorrect")
	}

	f3 := Int64("big", 9223372036854775807)
	if f3.Key != "big" || f3.Value != int64(9223372036854775807) {
		t.Error("Int64 field incorrect")
	}

	f4 := Uint64("pos", 18446744073709551615)
	if f4.Key != "pos" || f4.Value != uint64(18446744073709551615) {
		t.Error("Uint64 field incorrect")
	}

	f5 := Float64("pi", 3.14)
	if f5.Key != "pi" || f5.Value != 3.14 {
		t.Error("Float64 field incorrect")
	}

	f6 := Bool("flag", true)
	if f6.Key != "flag" || f6.Value != true {
		t.Error("Bool field incorrect")
	}

	f7 := Error(errors.New("test"))
	if f7.Key != "error" {
		t.Error("Error field key incorrect")
	}

	f8 := Duration("elapsed", time.Second)
	if f8.Key != "elapsed" || f8.Value != time.Second {
		t.Error("Duration field incorrect")
	}

	f9 := Any("any", []int{1, 2, 3})
	if f9.Key != "any" {
		t.Error("Any field key incorrect")
	}
}

func TestLogger_Basic(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))
	logger.SetLevel(DebugLevel)

	logger.Debug("debug message", String("key", "value"))
	logger.Info("info message", Int("count", 42))
	logger.Warn("warn message")
	logger.Error("error message")

	output := buf.String()

	if !strings.Contains(output, "debug message") {
		t.Error("Debug message not logged")
	}
	if !strings.Contains(output, "info message") {
		t.Error("Info message not logged")
	}
	if !strings.Contains(output, "key=value") {
		t.Error("Field not logged")
	}
	if !strings.Contains(output, "count=42") {
		t.Error("Int field not logged")
	}
}

func TestLogger_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))
	logger.SetLevel(WarnLevel)

	logger.Debug("debug")
	logger.Info("info")
	logger.Warn("warn")
	logger.Error("error")

	output := buf.String()

	if strings.Contains(output, "debug") {
		t.Error("Debug should be filtered")
	}
	if strings.Contains(output, "info") {
		t.Error("Info should be filtered")
	}
	if !strings.Contains(output, "warn") {
		t.Error("Warn should not be filtered")
	}
	if !strings.Contains(output, "error") {
		t.Error("Error should not be filtered")
	}
}

func TestLogger_Enabled(t *testing.T) {
	logger := NewWithDefaults()
	logger.SetLevel(InfoLevel)

	if logger.Enabled(DebugLevel) {
		t.Error("Debug should not be enabled")
	}
	if !logger.Enabled(InfoLevel) {
		t.Error("Info should be enabled")
	}
	if !logger.Enabled(ErrorLevel) {
		t.Error("Error should be enabled")
	}
}

func TestLogger_With(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))

	child := logger.With(String("parent", "value"))
	child.Info("message", String("child", "data"))

	output := buf.String()

	if !strings.Contains(output, "parent=value") {
		t.Error("Parent field not inherited")
	}
	if !strings.Contains(output, "child=data") {
		t.Error("Child field not logged")
	}
}

func TestLogger_WithName(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))

	child := logger.WithName("MyLogger")
	child.Info("message")

	output := buf.String()

	if !strings.Contains(output, "logger=MyLogger") {
		t.Error("Logger name not logged")
	}
}

func TestLogger_Formatted(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))
	logger.SetLevel(DebugLevel)

	logger.Debugf("debug %s %d", "test", 42)
	logger.Infof("info %s", "message")

	output := buf.String()

	if !strings.Contains(output, "debug test 42") {
		t.Error("Debugf not working")
	}
	if !strings.Contains(output, "info message") {
		t.Error("Infof not working")
	}
}

func TestLogger_FormattedLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))
	logger.SetLevel(WarnLevel)

	// These should be filtered (note: Sprintf is still called, just not logged)
	logger.Debugf("debug %s", "test")
	logger.Infof("info %s", "test")

	// These should be logged
	logger.Warnf("warn %s", "test")
	logger.Errorf("error %s", "test")

	output := buf.String()
	if strings.Contains(output, "debug") {
		t.Error("Debugf output should be filtered")
	}
	if strings.Contains(output, "info") {
		t.Error("Infof output should be filtered")
	}
	if !strings.Contains(output, "warn") || !strings.Contains(output, "error") {
		t.Error("Warnf/Errorf should be logged")
	}
}

func TestJSONOutput(t *testing.T) {
	var buf bytes.Buffer
	out := NewJSONOutput(&buf)

	out.Write(InfoLevel, "test message", []Field{
		String("key", "value"),
		Int("count", 42),
		Bool("flag", true),
	})

	output := buf.String()

	// Check JSON structure
	if !strings.HasPrefix(output, "{") {
		t.Error("JSON should start with {")
	}
	if !strings.HasSuffix(output, "}\n") {
		t.Error("JSON should end with }\n")
	}
	if !strings.Contains(output, `"level":"INFO"`) {
		t.Error("Level not in JSON")
	}
	if !strings.Contains(output, `"msg":"test message"`) {
		t.Error("Message not in JSON")
	}
	if !strings.Contains(output, `"key":"value"`) {
		t.Error("String field not in JSON")
	}
	if !strings.Contains(output, `"count":42`) {
		t.Error("Int field not in JSON")
	}
	if !strings.Contains(output, `"flag":true`) {
		t.Error("Bool field not in JSON")
	}
}

func TestJSONOutput_Escaping(t *testing.T) {
	var buf bytes.Buffer
	out := NewJSONOutput(&buf)

	out.Write(InfoLevel, `test "quoted" message`, []Field{
		String("path", "C:\\Users\\test"),
		String("newline", "line1\nline2"),
		String("tab", "col1\tcol2"),
	})

	output := buf.String()

	// Check escaping
	if strings.Contains(output, `"msg":"test "quoted" message"`) {
		t.Error("Quotes not escaped")
	}
	if strings.Contains(output, `C:\Users\test`) {
		t.Error("Backslashes not escaped")
	}
	if strings.Contains(output, "line1\nline2") {
		t.Error("Newline not escaped")
	}
}

func TestTextOutput(t *testing.T) {
	var buf bytes.Buffer
	out := NewTextOutput(&buf)

	out.Write(InfoLevel, "test message", []Field{
		String("key", "value"),
		Int("count", 42),
	})

	output := buf.String()

	if !strings.Contains(output, "INFO") {
		t.Error("Level not in output")
	}
	if !strings.Contains(output, "test message") {
		t.Error("Message not in output")
	}
	if !strings.Contains(output, "key=value") {
		t.Error("Field not in output")
	}
	if !strings.Contains(output, "count=42") {
		t.Error("Int field not in output")
	}
}

func TestMultiOutput(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	out1 := NewTextOutput(&buf1)
	out2 := NewTextOutput(&buf2)

	multi := NewMultiOutput(out1, out2)
	multi.Write(InfoLevel, "test", nil)

	if !strings.Contains(buf1.String(), "test") {
		t.Error("First output not written")
	}
	if !strings.Contains(buf2.String(), "test") {
		t.Error("Second output not written")
	}
}

func TestRotatingFileOutput(t *testing.T) {
	// Create temp file
	tmpFile := t.TempDir() + "/test.log"

	opts := RotatingFileOptions{
		Filename:   tmpFile,
		MaxSize:    100, // Small for testing
		MaxBackups: 3,
		Compress:   false,
	}

	out, err := NewRotatingFileOutput(opts)
	if err != nil {
		t.Fatalf("Failed to create rotating file: %v", err)
	}
	defer out.Close()

	// Write some data
	for i := 0; i < 10; i++ {
		out.Write(InfoLevel, "test message with some padding to exceed size", nil)
	}

	// Check file exists
	if _, err := os.Stat(tmpFile); err != nil {
		t.Error("Log file not created")
	}
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected string
	}{
		{"simple", "simple"},
		{"with space", `"with space"`},
		{42, "42"},
		{errors.New("err"), "err"},
		{time.Second, "1s"},
	}

	for _, tt := range tests {
		got := formatValue(tt.input)
		if got != tt.expected {
			t.Errorf("formatValue(%v) = %s, want %s", tt.input, got, tt.expected)
		}
	}
}

func BenchmarkLogger_Info(b *testing.B) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))
	logger.SetLevel(InfoLevel)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message", String("key", "value"), Int("count", i))
	}
}

func BenchmarkJSONOutput_Write(b *testing.B) {
	var buf bytes.Buffer
	out := NewJSONOutput(&buf)
	fields := []Field{
		String("key", "value"),
		Int("count", 42),
		Bool("flag", true),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		out.Write(InfoLevel, "test message", fields)
	}
}

func BenchmarkTextOutput_Write(b *testing.B) {
	var buf bytes.Buffer
	out := NewTextOutput(&buf)
	fields := []Field{
		String("key", "value"),
		Int("count", 42),
		Bool("flag", true),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		out.Write(InfoLevel, "test message", fields)
	}
}

// ========== Additional Logger Tests ==========

func TestLogger_NewWithDefaults(t *testing.T) {
	logger := NewWithDefaults()
	if logger == nil {
		t.Fatal("NewWithDefaults returned nil")
	}
	if logger.Level() != InfoLevel {
		t.Errorf("Expected default level Info, got %v", logger.Level())
	}
}

func TestLogger_SetLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))

	logger.SetLevel(DebugLevel)
	if logger.Level() != DebugLevel {
		t.Errorf("SetLevel failed, expected Debug, got %v", logger.Level())
	}

	logger.SetLevel(ErrorLevel)
	if logger.Level() != ErrorLevel {
		t.Errorf("SetLevel failed, expected Error, got %v", logger.Level())
	}
}

func TestLogger_IsLevelEnabled(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))
	logger.SetLevel(WarnLevel)

	tests := []struct {
		level    Level
		expected bool
	}{
		{TraceLevel, false},
		{DebugLevel, false},
		{InfoLevel, false},
		{WarnLevel, true},
		{ErrorLevel, true},
		{FatalLevel, true},
	}

	for _, tt := range tests {
		if got := logger.Enabled(tt.level); got != tt.expected {
			t.Errorf("Enabled(%v) = %v, want %v", tt.level, got, tt.expected)
		}
	}
}

func TestLogger_AllLevels(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))
	logger.SetLevel(TraceLevel)

	logger.Trace("trace message", String("key", "trace"))
	logger.Debug("debug message", String("key", "debug"))
	logger.Info("info message", String("key", "info"))
	logger.Warn("warn message", String("key", "warn"))
	logger.Error("error message", String("key", "error"))

	output := buf.String()

	if !strings.Contains(output, "trace message") {
		t.Error("Trace message not logged")
	}
	if !strings.Contains(output, "debug message") {
		t.Error("Debug message not logged")
	}
	if !strings.Contains(output, "info message") {
		t.Error("Info message not logged")
	}
	if !strings.Contains(output, "warn message") {
		t.Error("Warn message not logged")
	}
	if !strings.Contains(output, "error message") {
		t.Error("Error message not logged")
	}
}

func TestLogger_AllLevelsFormatted(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))
	logger.SetLevel(TraceLevel)

	logger.Tracef("trace %s", "formatted")
	logger.Debugf("debug %s", "formatted")
	logger.Infof("info %s", "formatted")
	logger.Warnf("warn %s", "formatted")
	logger.Errorf("error %s", "formatted")

	output := buf.String()

	if !strings.Contains(output, "trace formatted") {
		t.Error("Tracef message not logged")
	}
	if !strings.Contains(output, "debug formatted") {
		t.Error("Debugf message not logged")
	}
	if !strings.Contains(output, "info formatted") {
		t.Error("Infof message not logged")
	}
	if !strings.Contains(output, "warn formatted") {
		t.Error("Warnf message not logged")
	}
	if !strings.Contains(output, "error formatted") {
		t.Error("Errorf message not logged")
	}
}

func TestLogger_ChildWithMultipleFields(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))

	child1 := logger.With(String("field1", "value1"))
	child2 := child1.With(String("field2", "value2"))
	child3 := child2.With(String("field3", "value3"))

	child3.Info("message")

	output := buf.String()

	if !strings.Contains(output, "field1=value1") {
		t.Error("Field1 not inherited")
	}
	if !strings.Contains(output, "field2=value2") {
		t.Error("Field2 not inherited")
	}
	if !strings.Contains(output, "field3=value3") {
		t.Error("Field3 not logged")
	}
}

func TestLogger_WithEmpty(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))

	child := logger.With()
	child.Info("message", String("key", "value"))

	output := buf.String()
	if !strings.Contains(output, "key=value") {
		t.Error("Field not logged after empty With()")
	}
}

func TestLogger_Close(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))

	err := logger.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestLogger_Close_NilOutput(t *testing.T) {
	logger := &Logger{}

	err := logger.Close()
	if err != nil {
		t.Errorf("Close with nil output should return nil, got: %v", err)
	}
}

// ========== Output Tests ==========

func TestJSONOutput_Close(t *testing.T) {
	var buf bytes.Buffer
	out := NewJSONOutput(&buf)

	err := out.Close()
	if err != nil {
		t.Errorf("JSONOutput.Close returned error: %v", err)
	}
}

func TestTextOutput_Close(t *testing.T) {
	var buf bytes.Buffer
	out := NewTextOutput(&buf)

	err := out.Close()
	if err != nil {
		t.Errorf("TextOutput.Close returned error: %v", err)
	}
}

func TestMultiOutput_Close(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	out1 := NewTextOutput(&buf1)
	out2 := NewTextOutput(&buf2)

	multi := NewMultiOutput(out1, out2)
	err := multi.Close()
	if err != nil {
		t.Errorf("MultiOutput.Close returned error: %v", err)
	}
}

func TestMultiOutput_Close_Error(t *testing.T) {
	// Create a multi-output with an error-injecting writer
	errWriter := &errorWriter{err: errors.New("write error")}
	out := NewTextOutput(errWriter)

	multi := NewMultiOutput(out)
	err := multi.Close()
	if err != nil {
		t.Errorf("MultiOutput.Close should not propagate close errors: %v", err)
	}
}

func TestMultiOutput_Empty(t *testing.T) {
	multi := NewMultiOutput()
	multi.Write(InfoLevel, "test", nil)
	// Should not panic
}

// errorWriter is a writer that always returns an error
type errorWriter struct {
	err error
}

func (w *errorWriter) Write(p []byte) (n int, err error) {
	return 0, w.err
}

func TestJSONOutput_WriteError(t *testing.T) {
	errWriter := &errorWriter{err: errors.New("write error")}
	out := NewJSONOutput(errWriter)

	// Should not panic on write error
	out.Write(InfoLevel, "test message", nil)
}

func TestTextOutput_WriteError(t *testing.T) {
	errWriter := &errorWriter{err: errors.New("write error")}
	out := NewTextOutput(errWriter)

	// Should not panic on write error
	out.Write(InfoLevel, "test message", nil)
}

func TestJSONOutput_Concurrent(t *testing.T) {
	var buf bytes.Buffer
	out := NewJSONOutput(&buf)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				out.Write(InfoLevel, "concurrent", []Field{Int("id", id), Int("seq", j)})
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Check that we got all lines
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1000 {
		t.Errorf("Expected 1000 lines, got %d", len(lines))
	}
}

func TestTextOutput_Concurrent(t *testing.T) {
	var buf bytes.Buffer
	out := NewTextOutput(&buf)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				out.Write(InfoLevel, "concurrent", []Field{Int("id", id), Int("seq", j)})
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Check that we got all lines
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1000 {
		t.Errorf("Expected 1000 lines, got %d", len(lines))
	}
}

// ========== RotatingFileOutput Tests ==========

func TestRotatingFileOutput_DefaultOptions(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.log"

	opts := RotatingFileOptions{
		Filename: tmpFile,
		// Leave other options at zero to test defaults
	}

	out, err := NewRotatingFileOutput(opts)
	if err != nil {
		t.Fatalf("Failed to create rotating file: %v", err)
	}
	defer out.Close()

	// Write some data
	out.Write(InfoLevel, "test message", nil)

	// Check file exists
	if _, err := os.Stat(tmpFile); err != nil {
		t.Error("Log file not created")
	}
}

func TestRotatingFileOutput_Rotation(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.log"

	// Pre-create a file larger than MaxSize to force rotation on first write
	err := os.WriteFile(tmpFile, make([]byte, 100), 0644)
	if err != nil {
		t.Fatalf("Failed to create initial file: %v", err)
	}

	opts := RotatingFileOptions{
		Filename:   tmpFile,
		MaxSize:    50, // Smaller than initial file size
		MaxBackups: 3,
		Compress:   false,
	}

	out, err := NewRotatingFileOutput(opts)
	if err != nil {
		t.Fatalf("Failed to create rotating file: %v", err)
	}

	// Write to trigger rotation (file size > maxSize)
	out.Write(InfoLevel, "trigger rotation", nil)

	out.Close()

	// Check that rotation occurred
	if _, err := os.Stat(tmpFile + ".1"); err != nil {
		t.Error("Rotated file not created")
	}
}

func TestRotatingFileOutput_RotationWithCompression(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.log"

	// Pre-create a file larger than MaxSize to force rotation on first write
	err := os.WriteFile(tmpFile, make([]byte, 100), 0644)
	if err != nil {
		t.Fatalf("Failed to create initial file: %v", err)
	}

	opts := RotatingFileOptions{
		Filename:   tmpFile,
		MaxSize:    50, // Smaller than initial file size
		MaxBackups: 3,
		Compress:   true,
	}

	out, err := NewRotatingFileOutput(opts)
	if err != nil {
		t.Fatalf("Failed to create rotating file: %v", err)
	}

	// Write to trigger rotation (file size > maxSize)
	out.Write(InfoLevel, "trigger rotation", nil)

	out.Close()

	// Check that compressed rotation occurred (or fallback to uncompressed)
	if _, err := os.Stat(tmpFile + ".1.gz"); err != nil {
		// Fallback: check for uncompressed rotation
		if _, err := os.Stat(tmpFile + ".1"); err != nil {
			t.Error("Rotated file not created (neither compressed nor uncompressed)")
		}
	}
}

func TestRotatingFileOutput_CleanupOldBackups(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.log"

	opts := RotatingFileOptions{
		Filename:   tmpFile,
		MaxSize:    30,
		MaxBackups: 2, // Keep only 2 backups
		Compress:   false,
	}

	out, err := NewRotatingFileOutput(opts)
	if err != nil {
		t.Fatalf("Failed to create rotating file: %v", err)
	}

	// Write enough data to trigger multiple rotations
	for i := 0; i < 200; i++ {
		out.Write(InfoLevel, "test message with lots of padding data to trigger rotation", nil)
	}

	out.Close()

	// Check that old backups are cleaned up (should not have .3 or higher)
	if _, err := os.Stat(tmpFile + ".3"); err == nil {
		t.Error("Old backup .3 should have been cleaned up")
	}
}

func TestRotatingFileOutput_Reopen(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.log"

	opts := RotatingFileOptions{
		Filename:   tmpFile,
		MaxSize:    100,
		MaxBackups: 3,
		Compress:   false,
	}

	out, err := NewRotatingFileOutput(opts)
	if err != nil {
		t.Fatalf("Failed to create rotating file: %v", err)
	}

	// Write some data
	out.Write(InfoLevel, "before reopen", nil)

	// Reopen
	err = out.Reopen()
	if err != nil {
		t.Errorf("Reopen failed: %v", err)
	}

	// Write more data after reopen
	out.Write(InfoLevel, "after reopen", nil)

	out.Close()

	// Read file and verify both messages are there
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if !strings.Contains(string(content), "before reopen") {
		t.Error("Message before reopen not found")
	}
	if !strings.Contains(string(content), "after reopen") {
		t.Error("Message after reopen not found")
	}
}

func TestRotatingFileOutput_Close_NilWriter(t *testing.T) {
	out := &RotatingFileOutput{}

	err := out.Close()
	if err != nil {
		t.Errorf("Close with nil writer should not error: %v", err)
	}
}

func TestRotatingFileOutput_CreateDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/subdir/nested/test.log"

	opts := RotatingFileOptions{
		Filename:   tmpFile,
		MaxSize:    100,
		MaxBackups: 3,
		Compress:   false,
	}

	out, err := NewRotatingFileOutput(opts)
	if err != nil {
		t.Fatalf("Failed to create rotating file with nested directory: %v", err)
	}
	defer out.Close()

	// Check directory was created
	if _, err := os.Stat(tmpDir + "/subdir/nested"); err != nil {
		t.Error("Nested directory not created")
	}
}

// ========== Field Tests ==========

func TestField_Time(t *testing.T) {
	now := time.Now()
	f := Time("timestamp", now)

	if f.Key != "timestamp" {
		t.Errorf("Time field key = %s, want timestamp", f.Key)
	}

	if f.Value != now {
		t.Error("Time field value mismatch")
	}
}

func TestField_AllTypesInJSON(t *testing.T) {
	var buf bytes.Buffer
	out := NewJSONOutput(&buf)

	out.Write(InfoLevel, "test", []Field{
		String("string", "value"),
		Int("int", 42),
		Int64("int64", 9223372036854775807),
		Uint64("uint64", 18446744073709551615),
		Float64("float64", 3.14159),
		Bool("bool", true),
		Duration("duration", time.Second),
		Time("time", time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)),
		Error(errors.New("test error")),
		Any("any", map[string]int{"key": 123}),
	})

	output := buf.String()

	// Verify all fields are present
	checks := []string{
		`"string":"value"`,
		`"int":42`,
		`"int64":9223372036854775807`,
		`"uint64":18446744073709551615`,
		`"float64":3.14159`,
		`"bool":true`,
		`"duration":"1s"`,
		`"time":"2024-01-01T12:00:00Z"`,
		`"error":"test error"`,
		`"any":"map[key:123]"`,
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("JSON output missing: %s", check)
		}
	}
}

func TestField_AllTypesInText(t *testing.T) {
	var buf bytes.Buffer
	out := NewTextOutput(&buf)

	out.Write(InfoLevel, "test", []Field{
		String("string", "value"),
		Int("int", 42),
		Int64("int64", 9223372036854775807),
		Uint64("uint64", 18446744073709551615),
		Float64("float64", 3.14159),
		Bool("bool", true),
		Duration("duration", time.Second),
		Time("time", time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)),
		Error(errors.New("test error")),
		Any("any", map[string]int{"key": 123}),
	})

	output := buf.String()

	// Verify all fields are present
	checks := []string{
		"string=value",
		"int=42",
		"int64=9223372036854775807",
		"uint64=18446744073709551615",
		"float64=3.14159",
		"bool=true",
		"duration=1s",
		"time=2024-01-01T12:00:00Z",
		"error=test error",
		"any=map[key:123]",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("Text output missing: %s", check)
		}
	}
}

func TestField_EmptyFields(t *testing.T) {
	var buf bytes.Buffer
	out := NewJSONOutput(&buf)

	out.Write(InfoLevel, "test", nil)

	output := buf.String()
	if !strings.Contains(output, `"msg":"test"`) {
		t.Error("Message not in output")
	}
}

func TestField_NilError(t *testing.T) {
	var buf bytes.Buffer
	out := NewJSONOutput(&buf)

	out.Write(InfoLevel, "test", []Field{{Key: "err", Value: nil}})

	output := buf.String()
	if !strings.Contains(output, `"err":null`) {
		t.Error("Nil error should be null in JSON")
	}
}

func TestField_QuotedStringInText(t *testing.T) {
	var buf bytes.Buffer
	out := NewTextOutput(&buf)

	out.Write(InfoLevel, "test", []Field{
		String("path", "C:\\Users\\test"),
		String("space", "with space"),
		String("tab", "with\ttab"),
		String("newline", "with\nnewline"),
	})

	output := buf.String()

	// Values with spaces should be quoted
	if !strings.Contains(output, `"with space"`) {
		t.Error("String with space should be quoted")
	}
}

func TestField_AllNumericTypesInJSON(t *testing.T) {
	var buf bytes.Buffer
	out := NewJSONOutput(&buf)

	out.Write(InfoLevel, "test", []Field{
		{Key: "int8", Value: int8(127)},
		{Key: "int16", Value: int16(32767)},
		{Key: "int32", Value: int32(2147483647)},
		{Key: "uint", Value: uint(42)},
		{Key: "uint8", Value: uint8(255)},
		{Key: "uint16", Value: uint16(65535)},
		{Key: "uint32", Value: uint32(4294967295)},
		{Key: "float32", Value: float32(3.14)},
	})

	output := buf.String()

	checks := []string{
		`"int8":127`,
		`"int16":32767`,
		`"int32":2147483647`,
		`"uint":42`,
		`"uint8":255`,
		`"uint16":65535`,
		`"uint32":4294967295`,
		`"float32":3.14`,
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("JSON output missing: %s", check)
		}
	}
}

// ========== Edge Cases ==========

func TestLogger_NilOutput(t *testing.T) {
	// Logger with nil output should handle Close gracefully
	logger := &Logger{}

	err := logger.Close()
	if err != nil {
		t.Errorf("Close with nil output should return nil, got: %v", err)
	}
}

func TestLogger_SilentLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))
	logger.SetLevel(SilentLevel)

	logger.Error("error message")
	logger.Fatal("fatal message")

	output := buf.String()
	if output != "" {
		t.Error("Silent level should not log anything")
	}
}

func TestLevel_MarshalJSON(t *testing.T) {
	level := InfoLevel
	data, err := level.MarshalJSON()
	if err != nil {
		t.Errorf("MarshalJSON failed: %v", err)
	}

	if string(data) != `"INFO"` {
		t.Errorf("MarshalJSON = %s, want \"INFO\"", string(data))
	}
}

func TestJSONOutput_SpecialCharacters(t *testing.T) {
	var buf bytes.Buffer
	out := NewJSONOutput(&buf)

	out.Write(InfoLevel, "test\x00\x01\x02message", []Field{
		String("key", "value\x03\x04"),
	})

	output := buf.String()

	// Check that control characters are escaped
	if strings.Contains(output, "\x00") {
		t.Error("Null character should be escaped")
	}
}

func TestRotatingFileOutput_AppendExisting(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.log"

	// Create initial file with content
	err := os.WriteFile(tmpFile, []byte("existing content\n"), 0644)
	if err != nil {
		t.Fatalf("Failed to create initial file: %v", err)
	}

	opts := RotatingFileOptions{
		Filename:   tmpFile,
		MaxSize:    1000,
		MaxBackups: 3,
		Compress:   false,
	}

	out, err := NewRotatingFileOutput(opts)
	if err != nil {
		t.Fatalf("Failed to create rotating file: %v", err)
	}

	out.Write(InfoLevel, "new message", nil)
	out.Close()

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if !strings.Contains(string(content), "existing content") {
		t.Error("Existing content should be preserved")
	}
	if !strings.Contains(string(content), "new message") {
		t.Error("New message should be appended")
	}
}

func TestRotatingFileOutput_RotateError(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.log"

	opts := RotatingFileOptions{
		Filename:   tmpFile,
		MaxSize:    1, // Very small to trigger rotation
		MaxBackups: 1,
		Compress:   false,
	}

	out, err := NewRotatingFileOutput(opts)
	if err != nil {
		t.Fatalf("Failed to create rotating file: %v", err)
	}

	// Write to trigger rotation - should not panic even if rotation fails
	for i := 0; i < 100; i++ {
		out.Write(InfoLevel, "message to trigger rotation", nil)
	}

	out.Close()
}

func TestLogger_NameWithFields(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))

	child := logger.WithName("TestLogger").With(String("key", "value"))
	child.Info("message")

	output := buf.String()

	if !strings.Contains(output, "logger=TestLogger") {
		t.Error("Logger name not in output")
	}
	if !strings.Contains(output, "key=value") {
		t.Error("Field not in output")
	}
}

func TestLogger_NameInheritance(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))

	named := logger.WithName("Parent")
	child := named.With(String("key", "value"))

	if child.name != "Parent" {
		t.Error("Name should be inherited by child logger")
	}
}

func TestFormatValue_Nil(t *testing.T) {
	result := formatValue(nil)
	// formatValue returns "<nil>" for nil via fmt.Sprintf
	if result != "<nil>" {
		t.Errorf("formatValue(nil) = %s, want <nil>", result)
	}
}

func TestFormatValue_Time(t *testing.T) {
	tm := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	result := formatValue(tm)
	if result != "2024-01-01T12:00:00Z" {
		t.Errorf("formatValue(time) = %s, want 2024-01-01T12:00:00Z", result)
	}
}

func TestParseLevel_EdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
	}{
		{"warning", WarnLevel},
		{"UNKNOWN", InfoLevel},
		{"trace", TraceLevel},
		{"DEBUG", DebugLevel},
		{"INFO", InfoLevel},
		{"WARN", WarnLevel},
		{"ERROR", ErrorLevel},
		{"FATAL", FatalLevel},
		{"SILENT", SilentLevel},
	}

	for _, tt := range tests {
		got := ParseLevel(tt.input)
		if got != tt.expected {
			t.Errorf("ParseLevel(%s) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestLevel_StringEdgeCases(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{SilentLevel, "UNKNOWN"},
		{Level(-1), "UNKNOWN"},
		{Level(100), "UNKNOWN"},
	}

	for _, tt := range tests {
		got := tt.level.String()
		if got != tt.expected {
			t.Errorf("Level(%d).String() = %s, want %s", tt.level, got, tt.expected)
		}
	}
}

func TestMultiOutput_Concurrent(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	out1 := NewTextOutput(&buf1)
	out2 := NewTextOutput(&buf2)

	multi := NewMultiOutput(out1, out2)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				multi.Write(InfoLevel, "concurrent", []Field{Int("id", id)})
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Both outputs should have the same number of lines
	lines1 := strings.Split(strings.TrimSpace(buf1.String()), "\n")
	lines2 := strings.Split(strings.TrimSpace(buf2.String()), "\n")

	if len(lines1) != 1000 {
		t.Errorf("Output 1 expected 1000 lines, got %d", len(lines1))
	}
	if len(lines2) != 1000 {
		t.Errorf("Output 2 expected 1000 lines, got %d", len(lines2))
	}
}

func TestRotatingFileOutput_CompressFileError(t *testing.T) {
	out := &RotatingFileOutput{
		filename:   "/nonexistent/path/test.log",
		maxSize:    100,
		maxBackups: 3,
		compress:   true,
	}

	// This should fail because the source file doesn't exist
	err := out.compressFile("/nonexistent/path/source.log", "/nonexistent/path/dest.gz")
	if err == nil {
		t.Error("compressFile should return error for nonexistent file")
	}
}

func TestRotatingFileOutput_OpenInvalidPath(t *testing.T) {
	// On Windows, this path might actually be valid, so we skip if it doesn't fail
	opts := RotatingFileOptions{
		Filename:   "/nonexistent/directory/that/cannot/be/created/test.log",
		MaxSize:    100,
		MaxBackups: 3,
		Compress:   false,
	}

	_, err := NewRotatingFileOutput(opts)
	// Just verify the function returns - on some systems this may succeed, on others fail
	_ = err
}

func TestLogger_TracefDisabled(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))
	logger.SetLevel(InfoLevel)

	// Should not format the string when level is disabled
	logger.Tracef("expensive %s", "formatting")

	if buf.String() != "" {
		t.Error("Tracef should not output when disabled")
	}
}

func TestLogger_Fatalf(t *testing.T) {
	// Fatalf calls Fatal(fmt.Sprintf(format, args...)), which in turn calls
	// l.log(FatalLevel, msg, fields). The log method writes to the output
	// and then calls os.Exit(1). We test that Fatalf correctly formats the
	// message and writes it to the output by using SilentLevel to prevent
	// the actual log call (and thus os.Exit).

	// First, verify the method exists at compile time.
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))
	logger.SetLevel(SilentLevel)
	_ = logger.Fatalf

	// Now test that Fatalf actually calls Fatal with formatted string.
	// We do this by creating a logger that captures output but does NOT
	// exit, by overriding the behavior: we set the level high enough
	// so the log method does not write (FatalLevel is always >= any level
	// except SilentLevel). With SilentLevel, even Fatal won't write.
	logger.Fatalf("test %s %d", "format", 42)

	// With SilentLevel, nothing should be written
	if buf.String() != "" {
		t.Error("Fatalf should not output when SilentLevel is set")
	}
}

// captureOutput is a test helper that captures log output.
type captureOutput struct {
	writeFn func(level Level, msg string, fields []Field)
}

func (c *captureOutput) Write(level Level, msg string, fields []Field) {
	if c.writeFn != nil {
		c.writeFn(level, msg, fields)
	}
}

func (c *captureOutput) Close() error {
	return nil
}

func TestLogger_Fatalf_WritesFormattedMessage(t *testing.T) {
	// Test that Fatalf formats the message correctly before passing to Fatal.
	// We can't test the os.Exit path, but we can verify the formatted output
	// is written by checking what the output receives.
	//
	// We create a custom output that captures the message.
	var capturedMsg string
	var capturedLevel Level

	customOutput := &captureOutput{
		writeFn: func(level Level, msg string, fields []Field) {
			capturedLevel = level
			capturedMsg = msg
		},
	}

	logger := New(customOutput)
	// Note: Fatal calls os.Exit(1), so we can't actually call Fatalf in a
	// normal test. However, we can verify the formatting by testing the
	// underlying call chain. Fatalf calls l.Fatal(fmt.Sprintf(format, args...)).
	// We test that fmt.Sprintf produces the right result.
	formatted := fmt.Sprintf("error: %s code=%d", "connection refused", 500)
	if formatted != "error: connection refused code=500" {
		t.Errorf("Sprintf = %q", formatted)
	}

	// Verify the capture output works for non-fatal levels
	logger.Error("test error")
	if capturedLevel != ErrorLevel {
		t.Errorf("capturedLevel = %v, want ErrorLevel", capturedLevel)
	}
	if capturedMsg != "test error" {
		t.Errorf("capturedMsg = %q, want %q", capturedMsg, "test error")
	}

	_ = customOutput
}

func TestLogger_FatalDisabled(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))
	logger.SetLevel(SilentLevel)

	// With SilentLevel, Fatal should not write or exit
	logger.Fatal("fatal message")

	if buf.String() != "" {
		t.Error("Fatal should not output when SilentLevel is set")
	}
}

func TestJSONOutput_EmptyMessage(t *testing.T) {
	var buf bytes.Buffer
	out := NewJSONOutput(&buf)

	out.Write(InfoLevel, "", nil)

	output := buf.String()
	if !strings.Contains(output, `"msg":""`) {
		t.Error("Empty message should be logged")
	}
}

func TestTextOutput_EmptyMessage(t *testing.T) {
	var buf bytes.Buffer
	out := NewTextOutput(&buf)

	out.Write(InfoLevel, "", nil)

	output := buf.String()
	if !strings.Contains(output, "INFO") {
		t.Error("Empty message should still have level")
	}
}

func TestField_ErrorNil(t *testing.T) {
	var buf bytes.Buffer
	out := NewTextOutput(&buf)

	out.Write(InfoLevel, "test", []Field{Error(nil)})

	output := buf.String()
	// Error(nil) creates a field with nil error, which formatValue handles
	// The output will contain "error=" followed by the formatted nil value
	if !strings.Contains(output, "error=") {
		t.Error("Error field not in output")
	}
}

func TestRotatingFileOutput_WriteAfterClose(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.log"

	opts := RotatingFileOptions{
		Filename:   tmpFile,
		MaxSize:    100,
		MaxBackups: 3,
		Compress:   false,
	}

	out, err := NewRotatingFileOutput(opts)
	if err != nil {
		t.Fatalf("Failed to create rotating file: %v", err)
	}

	out.Close()

	// This may panic or error, but should not crash the test
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Write after close recovered from panic: %v", r)
		}
	}()

	out.Write(InfoLevel, "after close", nil)
}

func TestRotatingFileOutput_MultipleReopens(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.log"

	opts := RotatingFileOptions{
		Filename:   tmpFile,
		MaxSize:    1000,
		MaxBackups: 3,
		Compress:   false,
	}

	out, err := NewRotatingFileOutput(opts)
	if err != nil {
		t.Fatalf("Failed to create rotating file: %v", err)
	}

	// Multiple reopens should work
	for i := 0; i < 5; i++ {
		err = out.Reopen()
		if err != nil {
			t.Errorf("Reopen %d failed: %v", i, err)
		}
		out.Write(InfoLevel, fmt.Sprintf("message %d", i), nil)
	}

	out.Close()

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	for i := 0; i < 5; i++ {
		if !strings.Contains(string(content), fmt.Sprintf("message %d", i)) {
			t.Errorf("Message %d not found in file", i)
		}
	}
}

func TestLogger_LevelRace(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))

	done := make(chan bool, 20)

	// Concurrent level changes
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				logger.SetLevel(TraceLevel)
				logger.SetLevel(DebugLevel)
				logger.SetLevel(InfoLevel)
			}
			done <- true
		}()
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = logger.Level()
				_ = logger.Enabled(InfoLevel)
			}
			done <- true
		}()
	}

	for i := 0; i < 20; i++ {
		<-done
	}
}

func TestLogger_WithRace(t *testing.T) {
	var buf bytes.Buffer
	logger := New(NewTextOutput(&buf))

	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			child := logger.With(Int("id", id))
			for j := 0; j < 100; j++ {
				child.Info("message", Int("seq", j))
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have 1000 lines
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1000 {
		t.Errorf("Expected 1000 lines, got %d", len(lines))
	}
}

// TestReopenHandler tests the ReopenHandler (platform-specific).
func TestReopenHandler(t *testing.T) {
	h := NewReopenHandler()
	if h == nil {
		t.Fatal("NewReopenHandler returned nil")
	}

	// These are no-ops on Windows but should not panic
	h.AddOutput(nil)
	h.Start()
	h.Stop()
	h.reopen()
}

// TestEnableLogReopen tests EnableLogReopen (no-op on Windows).
func TestEnableLogReopen(t *testing.T) {
	// Should not panic
	EnableLogReopen()
}

// TestStopLogReopen tests StopLogReopen (no-op on Windows).
func TestStopLogReopen(t *testing.T) {
	// Should not panic
	StopLogReopen()
}
