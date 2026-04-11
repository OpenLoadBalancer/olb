package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// MockCommand is a test command that records its execution
type MockCommand struct {
	name        string
	description string
	runCalled   bool
	runArgs     []string
	returnErr   error
}

func (m *MockCommand) Name() string {
	return m.name
}

func (m *MockCommand) Description() string {
	return m.description
}

func (m *MockCommand) Run(args []string) error {
	m.runCalled = true
	m.runArgs = args
	return m.returnErr
}

func TestNew(t *testing.T) {
	cli := New("olb", "0.1.0")

	if cli.Name() != "olb" {
		t.Errorf("expected name 'olb', got '%s'", cli.Name())
	}

	if cli.Version() != "0.1.0" {
		t.Errorf("expected version '1.0.0', got '%s'", cli.Version())
	}

	if len(cli.Commands()) != 0 {
		t.Errorf("expected 0 commands, got %d", len(cli.Commands()))
	}
}

func TestNewWithWriters(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	cli := NewWithWriters("olb", "0.1.0", &out, &errOut)

	if cli.Name() != "olb" {
		t.Errorf("expected name 'olb', got '%s'", cli.Name())
	}

	// Test that output goes to custom writers
	cli.Run([]string{"--version"})

	if !strings.Contains(out.String(), "0.1.0") {
		t.Errorf("expected version in output, got: %s", out.String())
	}
}

func TestCLI_Register(t *testing.T) {
	cli := New("olb", "0.1.0")
	cmd := &MockCommand{name: "test", description: "Test command"}

	cli.Register(cmd)

	commands := cli.Commands()
	if len(commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(commands))
	}

	if commands[0].Name() != "test" {
		t.Errorf("expected command name 'test', got '%s'", commands[0].Name())
	}

	// Test retrieving command by name
	retrieved := cli.Command("test")
	if retrieved == nil {
		t.Error("expected to retrieve command 'test', got nil")
	}

	if retrieved.Name() != "test" {
		t.Errorf("expected retrieved command name 'test', got '%s'", retrieved.Name())
	}

	// Test non-existent command
	if cli.Command("nonexistent") != nil {
		t.Error("expected nil for non-existent command")
	}
}

func TestCLI_RegisterOverwrite(t *testing.T) {
	cli := New("olb", "0.1.0")
	cmd1 := &MockCommand{name: "test", description: "First command"}
	cmd2 := &MockCommand{name: "test", description: "Second command"}

	cli.Register(cmd1)
	cli.Register(cmd2)

	commands := cli.Commands()
	if len(commands) != 1 {
		t.Fatalf("expected 1 command (overwritten), got %d", len(commands))
	}

	if commands[0].Description() != "Second command" {
		t.Error("expected command to be overwritten with second description")
	}
}

func TestCLI_Run_NoCommand(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	cli := NewWithWriters("olb", "0.1.0", &out, &errOut)

	err := cli.Run([]string{})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	// Should print help
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("expected help output, got: %s", out.String())
	}
}

func TestCLI_Run_Help(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	cli := NewWithWriters("olb", "0.1.0", &out, &errOut)

	err := cli.Run([]string{"--help"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("expected help output, got: %s", out.String())
	}
}

func TestCLI_Run_HelpShort(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	cli := NewWithWriters("olb", "0.1.0", &out, &errOut)

	err := cli.Run([]string{"-h"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("expected help output, got: %s", out.String())
	}
}

func TestCLI_Run_Version(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	cli := NewWithWriters("olb", "0.1.0", &out, &errOut)

	err := cli.Run([]string{"--version"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !strings.Contains(out.String(), "0.1.0") {
		t.Errorf("expected version in output, got: %s", out.String())
	}
}

func TestCLI_Run_VersionShort(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	cli := NewWithWriters("olb", "0.1.0", &out, &errOut)

	err := cli.Run([]string{"-v"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !strings.Contains(out.String(), "0.1.0") {
		t.Errorf("expected version in output, got: %s", out.String())
	}
}

func TestCLI_Run_UnknownCommand(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	cli := NewWithWriters("olb", "0.1.0", &out, &errOut)

	err := cli.Run([]string{"unknown"})
	if err == nil {
		t.Error("expected error for unknown command")
	}

	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("expected 'unknown command' error, got: %v", err)
	}
}

func TestCLI_Run_Command(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	cli := NewWithWriters("olb", "0.1.0", &out, &errOut)
	cmd := &MockCommand{name: "test", description: "Test command"}
	cli.Register(cmd)

	err := cli.Run([]string{"test", "arg1", "arg2"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !cmd.runCalled {
		t.Error("expected command Run to be called")
	}

	if len(cmd.runArgs) != 2 || cmd.runArgs[0] != "arg1" || cmd.runArgs[1] != "arg2" {
		t.Errorf("expected args ['arg1', 'arg2'], got: %v", cmd.runArgs)
	}
}

func TestCLI_Run_CommandError(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	cli := NewWithWriters("olb", "0.1.0", &out, &errOut)
	cmd := &MockCommand{name: "test", description: "Test command", returnErr: errors.New("command failed")}
	cli.Register(cmd)

	err := cli.Run([]string{"test"})
	if err == nil {
		t.Error("expected error from command")
	}

	if err.Error() != "command failed" {
		t.Errorf("expected 'command failed' error, got: %v", err)
	}
}

func TestCLI_Run_CommandHelp(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	cli := NewWithWriters("olb", "0.1.0", &out, &errOut)
	cmd := &MockCommand{name: "test", description: "Test command description"}
	cli.Register(cmd)

	err := cli.Run([]string{"--help", "test"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !strings.Contains(out.String(), "test") {
		t.Errorf("expected command name in help, got: %s", out.String())
	}

	if !strings.Contains(out.String(), "Test command description") {
		t.Errorf("expected command description in help, got: %s", out.String())
	}
}

func TestCLI_Run_CommandHelpUnknown(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	cli := NewWithWriters("olb", "0.1.0", &out, &errOut)

	err := cli.Run([]string{"--help", "unknown"})
	if err == nil {
		t.Error("expected error for unknown command help")
	}

	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("expected 'unknown command' error, got: %v", err)
	}
}

func TestCLI_Help(t *testing.T) {
	cli := New("olb", "0.1.0")
	cli.Register(&MockCommand{name: "start", description: "Start the server"})
	cli.Register(&MockCommand{name: "stop", description: "Stop the server"})
	cli.Register(&MockCommand{name: "status", description: "Show status"})

	help := cli.Help()

	// Check for expected sections
	if !strings.Contains(help, "Usage:") {
		t.Error("expected 'Usage:' in help")
	}

	if !strings.Contains(help, "Global Options:") {
		t.Error("expected 'Global Options:' in help")
	}

	if !strings.Contains(help, "Commands:") {
		t.Error("expected 'Commands:' in help")
	}

	// Check for commands
	if !strings.Contains(help, "start") {
		t.Error("expected 'start' command in help")
	}

	if !strings.Contains(help, "stop") {
		t.Error("expected 'stop' command in help")
	}

	if !strings.Contains(help, "status") {
		t.Error("expected 'status' command in help")
	}

	// Check for descriptions
	if !strings.Contains(help, "Start the server") {
		t.Error("expected 'Start the server' description in help")
	}
}

func TestCLI_HelpSorted(t *testing.T) {
	cli := New("olb", "0.1.0")
	cli.Register(&MockCommand{name: "zebra", description: "Zebra command"})
	cli.Register(&MockCommand{name: "alpha", description: "Alpha command"})
	cli.Register(&MockCommand{name: "mike", description: "Mike command"})

	help := cli.Help()

	// Check that commands are sorted alphabetically
	zebraIdx := strings.Index(help, "zebra")
	alphaIdx := strings.Index(help, "alpha")
	mikeIdx := strings.Index(help, "mike")

	if alphaIdx == -1 || mikeIdx == -1 || zebraIdx == -1 {
		t.Fatal("not all commands found in help")
	}

	if !(alphaIdx < mikeIdx && mikeIdx < zebraIdx) {
		t.Error("commands should be sorted alphabetically")
	}
}

func TestCLI_Commands(t *testing.T) {
	cli := New("olb", "0.1.0")
	cmd1 := &MockCommand{name: "start", description: "Start"}
	cmd2 := &MockCommand{name: "stop", description: "Stop"}

	cli.Register(cmd1)
	cli.Register(cmd2)

	commands := cli.Commands()
	if len(commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(commands))
	}

	// Commands should be sorted
	if commands[0].Name() != "start" {
		t.Errorf("expected first command 'start', got '%s'", commands[0].Name())
	}

	if commands[1].Name() != "stop" {
		t.Errorf("expected second command 'stop', got '%s'", commands[1].Name())
	}
}

// ParseArgs tests

func TestParseArgs_Empty(t *testing.T) {
	args, err := ParseArgs([]string{})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if args.Command != "" {
		t.Errorf("expected empty command, got '%s'", args.Command)
	}

	if len(args.Flags) != 0 {
		t.Errorf("expected 0 flags, got %d", len(args.Flags))
	}

	if len(args.Args) != 0 {
		t.Errorf("expected 0 args, got %d", len(args.Args))
	}
}

func TestParseArgs_CommandOnly(t *testing.T) {
	args, err := ParseArgs([]string{"start"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if args.Command != "start" {
		t.Errorf("expected command 'start', got '%s'", args.Command)
	}
}

func TestParseArgs_CommandAndSubcommand(t *testing.T) {
	args, err := ParseArgs([]string{"backend", "add"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if args.Command != "backend" {
		t.Errorf("expected command 'backend', got '%s'", args.Command)
	}

	if args.Subcommand != "add" {
		t.Errorf("expected subcommand 'add', got '%s'", args.Subcommand)
	}
}

func TestParseArgs_LongFlags(t *testing.T) {
	args, err := ParseArgs([]string{"start", "--host=localhost", "--port", "8080"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if args.Command != "start" {
		t.Errorf("expected command 'start', got '%s'", args.Command)
	}

	if args.Flags["host"] != "localhost" {
		t.Errorf("expected host='localhost', got '%s'", args.Flags["host"])
	}

	if args.Flags["port"] != "8080" {
		t.Errorf("expected port='8080', got '%s'", args.Flags["port"])
	}
}

func TestParseArgs_ShortFlags(t *testing.T) {
	args, err := ParseArgs([]string{"start", "-h=localhost", "-p", "8080"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if args.Flags["h"] != "localhost" {
		t.Errorf("expected h='localhost', got '%s'", args.Flags["h"])
	}

	if args.Flags["p"] != "8080" {
		t.Errorf("expected p='8080', got '%s'", args.Flags["p"])
	}
}

func TestParseArgs_BoolFlags(t *testing.T) {
	args, err := ParseArgs([]string{"start", "--verbose", "-d"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if args.Flags["verbose"] != "true" {
		t.Errorf("expected verbose='true', got '%s'", args.Flags["verbose"])
	}

	if args.Flags["d"] != "true" {
		t.Errorf("expected d='true', got '%s'", args.Flags["d"])
	}
}

func TestParseArgs_PositionalArgs(t *testing.T) {
	args, err := ParseArgs([]string{"backend", "add", "server1", "192.168.1.1:8080"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if args.Command != "backend" {
		t.Errorf("expected command 'backend', got '%s'", args.Command)
	}

	if args.Subcommand != "add" {
		t.Errorf("expected subcommand 'add', got '%s'", args.Subcommand)
	}

	if len(args.Args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args.Args))
	}

	if args.Args[0] != "server1" {
		t.Errorf("expected arg[0]='server1', got '%s'", args.Args[0])
	}

	if args.Args[1] != "192.168.1.1:8080" {
		t.Errorf("expected arg[1]='192.168.1.1:8080', got '%s'", args.Args[1])
	}
}

func TestParseArgs_Mixed(t *testing.T) {
	args, err := ParseArgs([]string{"backend", "add", "--weight=10", "-t", "http", "server1"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if args.Command != "backend" {
		t.Errorf("expected command 'backend', got '%s'", args.Command)
	}

	if args.Subcommand != "add" {
		t.Errorf("expected subcommand 'add', got '%s'", args.Subcommand)
	}

	if args.Flags["weight"] != "10" {
		t.Errorf("expected weight='10', got '%s'", args.Flags["weight"])
	}

	if args.Flags["t"] != "http" {
		t.Errorf("expected t='http', got '%s'", args.Flags["t"])
	}

	if len(args.Args) != 1 || args.Args[0] != "server1" {
		t.Errorf("expected args=['server1'], got %v", args.Args)
	}
}

func TestParsedArgs_HasFlag(t *testing.T) {
	args := &ParsedArgs{
		Flags: map[string]string{
			"verbose": "true",
			"port":    "8080",
		},
	}

	if !args.HasFlag("verbose") {
		t.Error("expected HasFlag('verbose') to be true")
	}

	if !args.HasFlag("port") {
		t.Error("expected HasFlag('port') to be true")
	}

	if args.HasFlag("nonexistent") {
		t.Error("expected HasFlag('nonexistent') to be false")
	}
}

func TestParsedArgs_GetFlag(t *testing.T) {
	args := &ParsedArgs{
		Flags: map[string]string{
			"port": "8080",
		},
	}

	val, ok := args.GetFlag("port")
	if !ok {
		t.Error("expected GetFlag('port') to return ok=true")
	}
	if val != "8080" {
		t.Errorf("expected port='8080', got '%s'", val)
	}

	_, ok = args.GetFlag("nonexistent")
	if ok {
		t.Error("expected GetFlag('nonexistent') to return ok=false")
	}
}

func TestParsedArgs_GetFlagDefault(t *testing.T) {
	args := &ParsedArgs{
		Flags: map[string]string{
			"port": "8080",
		},
	}

	val := args.GetFlagDefault("port", "3000")
	if val != "8080" {
		t.Errorf("expected port='8080', got '%s'", val)
	}

	val = args.GetFlagDefault("host", "localhost")
	if val != "localhost" {
		t.Errorf("expected default host='localhost', got '%s'", val)
	}
}

// ParseGlobalFlags tests

func TestParseGlobalFlags_Empty(t *testing.T) {
	globals, remaining, err := ParseGlobalFlags([]string{})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if globals.Help {
		t.Error("expected Help to be false")
	}

	if globals.Version {
		t.Error("expected Version to be false")
	}

	if globals.Format != "table" {
		t.Errorf("expected default format 'table', got '%s'", globals.Format)
	}

	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining args, got %d", len(remaining))
	}
}

func TestParseGlobalFlags_Help(t *testing.T) {
	globals, remaining, err := ParseGlobalFlags([]string{"--help"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !globals.Help {
		t.Error("expected Help to be true")
	}

	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining args, got %d", len(remaining))
	}
}

func TestParseGlobalFlags_HelpShort(t *testing.T) {
	globals, _, err := ParseGlobalFlags([]string{"-h"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !globals.Help {
		t.Error("expected Help to be true")
	}
}

func TestParseGlobalFlags_Version(t *testing.T) {
	globals, remaining, err := ParseGlobalFlags([]string{"--version"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !globals.Version {
		t.Error("expected Version to be true")
	}

	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining args, got %d", len(remaining))
	}
}

func TestParseGlobalFlags_VersionShort(t *testing.T) {
	globals, _, err := ParseGlobalFlags([]string{"-v"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !globals.Version {
		t.Error("expected Version to be true")
	}
}

func TestParseGlobalFlags_Format(t *testing.T) {
	globals, _, err := ParseGlobalFlags([]string{"--format=json"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if globals.Format != "json" {
		t.Errorf("expected format 'json', got '%s'", globals.Format)
	}
}

func TestParseGlobalFlags_FormatShort(t *testing.T) {
	globals, _, err := ParseGlobalFlags([]string{"-f", "json"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if globals.Format != "json" {
		t.Errorf("expected format 'json', got '%s'", globals.Format)
	}
}

func TestParseGlobalFlags_FormatInvalid(t *testing.T) {
	_, _, err := ParseGlobalFlags([]string{"--format=xml"})
	if err == nil {
		t.Error("expected error for invalid format")
	}

	if !strings.Contains(err.Error(), "invalid format") {
		t.Errorf("expected 'invalid format' error, got: %v", err)
	}
}

func TestParseGlobalFlags_FormatMissingValue(t *testing.T) {
	_, _, err := ParseGlobalFlags([]string{"--format"})
	if err == nil {
		t.Error("expected error for missing format value")
	}

	if !strings.Contains(err.Error(), "requires a value") {
		t.Errorf("expected 'requires a value' error, got: %v", err)
	}
}

func TestParseGlobalFlags_Mixed(t *testing.T) {
	globals, remaining, err := ParseGlobalFlags([]string{"--format", "json", "start", "--verbose", "arg1"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if globals.Format != "json" {
		t.Errorf("expected format 'json', got '%s'", globals.Format)
	}

	if len(remaining) != 3 {
		t.Fatalf("expected 3 remaining args, got %d: %v", len(remaining), remaining)
	}

	if remaining[0] != "start" {
		t.Errorf("expected remaining[0]='start', got '%s'", remaining[0])
	}
}

// Formatter tests

func TestNewFormatter(t *testing.T) {
	tests := []struct {
		name        string
		wantErr     bool
		errContains string
	}{
		{"json", false, ""},
		{"json-indent", false, ""},
		{"table", false, ""},
		{"xml", true, "unknown format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatter, err := NewFormatter(tt.name)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing '%s', got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
				if formatter == nil {
					t.Error("expected formatter, got nil")
				}
			}
		})
	}
}

func TestJSONFormatter_Format(t *testing.T) {
	tests := []struct {
		name     string
		indent   bool
		data     any
		expected string
	}{
		{
			name:     "simple map",
			indent:   false,
			data:     map[string]string{"name": "test", "status": "running"},
			expected: `{"name":"test","status":"running"}`,
		},
		{
			name:   "simple map indented",
			indent: true,
			data:   map[string]string{"name": "test"},
			expected: `{
  "name": "test"
}`,
		},
		{
			name:     "nil",
			indent:   false,
			data:     nil,
			expected: "null",
		},
		{
			name:     "slice",
			indent:   false,
			data:     []string{"a", "b", "c"},
			expected: `["a","b","c"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &JSONFormatter{Indent: tt.indent}
			result, err := f.Format(tt.data)
			if err != nil {
				t.Errorf("expected no error, got: %v", err)
			}

			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestTableFormatter_Format_StringSlice(t *testing.T) {
	f := &TableFormatter{
		Headers: []string{"Name", "Status", "Port"},
	}

	data := [][]string{
		{"server1", "up", "8080"},
		{"server2", "down", "8081"},
	}

	result, err := f.Format(data)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	// Check that headers and data are present
	if !strings.Contains(result, "Name") {
		t.Error("expected 'Name' header in output")
	}

	if !strings.Contains(result, "server1") {
		t.Error("expected 'server1' in output")
	}

	if !strings.Contains(result, "up") {
		t.Error("expected 'up' in output")
	}
}

func TestTableFormatter_Format_MapSlice(t *testing.T) {
	f := &TableFormatter{
		Headers: []string{"name", "status"},
	}

	data := []map[string]string{
		{"name": "server1", "status": "up"},
		{"name": "server2", "status": "down"},
	}

	result, err := f.Format(data)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !strings.Contains(result, "server1") {
		t.Error("expected 'server1' in output")
	}

	if !strings.Contains(result, "down") {
		t.Error("expected 'down' in output")
	}
}

func TestTableFormatter_Format_SingleMap(t *testing.T) {
	f := &TableFormatter{}

	data := map[string]string{
		"name":   "server1",
		"status": "up",
	}

	result, err := f.Format(data)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !strings.Contains(result, "name") {
		t.Error("expected 'name' in output")
	}

	if !strings.Contains(result, "server1") {
		t.Error("expected 'server1' in output")
	}
}

func TestTableFormatter_Format_SingleColumn(t *testing.T) {
	f := &TableFormatter{
		Headers: []string{"Servers"},
	}

	data := []string{"server1", "server2", "server3"}

	result, err := f.Format(data)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !strings.Contains(result, "server1") {
		t.Error("expected 'server1' in output")
	}

	if !strings.Contains(result, "server3") {
		t.Error("expected 'server3' in output")
	}
}

func TestTableFormatter_Format_Empty(t *testing.T) {
	f := &TableFormatter{}

	result, err := f.Format([][]string{})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if result != "" {
		t.Errorf("expected empty result, got '%s'", result)
	}
}

func TestFormatToWriter(t *testing.T) {
	var buf bytes.Buffer
	formatter := &JSONFormatter{Indent: false}
	data := map[string]string{"key": "value"}

	err := FormatToWriter(&buf, formatter, data)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"key":"value"`) {
		t.Errorf("expected JSON output, got: %s", output)
	}
}

func TestFormatWithGlobals(t *testing.T) {
	globals := &GlobalFlags{Format: "json"}
	data := map[string]string{"status": "ok"}

	result, err := FormatWithGlobals(globals, data)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !strings.Contains(result, `"status":"ok"`) {
		t.Errorf("expected JSON output, got: %s", result)
	}
}

func TestFormatWithGlobals_InvalidFormat(t *testing.T) {
	globals := &GlobalFlags{Format: "invalid"}
	data := map[string]string{"status": "ok"}

	_, err := FormatWithGlobals(globals, data)
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

// Additional tests for coverage

func TestTableFormatter_Format_Nil(t *testing.T) {
	f := &TableFormatter{}
	result, err := f.Format(nil)
	if err != nil {
		t.Errorf("expected no error for nil data, got: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result for nil data, got: %q", result)
	}
}

func TestTableFormatter_formatWithHeaders(t *testing.T) {
	f := &TableFormatter{Headers: []string{"Col1", "Col2"}}
	// Pass an unsupported type to trigger formatWithHeaders fallback
	data := 42
	result, err := f.Format(data)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if result != "42" {
		t.Errorf("expected '42', got: %q", result)
	}
}

func TestTableFormatter_formatWithHeaders_Struct(t *testing.T) {
	f := &TableFormatter{}
	type testStruct struct {
		Name string
	}
	data := testStruct{Name: "test"}
	result, err := f.Format(data)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result for struct")
	}
}

func TestFormatToWriter_Error(t *testing.T) {
	// Test with data that will cause JSON encoding to fail
	type badType struct {
		Ch chan int
	}
	formatter := &JSONFormatter{Indent: false}
	var buf bytes.Buffer
	err := FormatToWriter(&buf, formatter, badType{Ch: make(chan int)})
	if err == nil {
		t.Error("expected error for unmarshalable data")
	}
}

func TestTableFormatter_Format_MapSliceNoHeaders(t *testing.T) {
	// Test formatMapSlice with no pre-set Headers to trigger auto-extraction
	f := &TableFormatter{}
	data := []map[string]string{
		{"name": "alice", "age": "30"},
		{"name": "bob", "age": "25"},
	}
	result, err := f.Format(data)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if !strings.Contains(result, "alice") {
		t.Error("expected 'alice' in output")
	}
	if !strings.Contains(result, "bob") {
		t.Error("expected 'bob' in output")
	}
}

func TestTableFormatter_Format_EmptyMapSlice(t *testing.T) {
	f := &TableFormatter{}
	result, err := f.Format([]map[string]string{})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result for empty map slice, got: %q", result)
	}
}

func TestTableFormatter_Format_EmptySingleMap(t *testing.T) {
	f := &TableFormatter{}
	result, err := f.Format(map[string]string{})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result for empty map, got: %q", result)
	}
}

func TestTableFormatter_Format_EmptySingleColumn(t *testing.T) {
	f := &TableFormatter{}
	result, err := f.Format([]string{})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result for empty string slice, got: %q", result)
	}
}

func TestTableFormatter_Format_SingleColumnNoHeader(t *testing.T) {
	f := &TableFormatter{}
	data := []string{"item1", "item2"}
	result, err := f.Format(data)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if !strings.Contains(result, "item1") {
		t.Error("expected 'item1' in output")
	}
}

func TestTableFormatter_Format_StringSliceNoHeaders(t *testing.T) {
	f := &TableFormatter{}
	data := [][]string{
		{"a", "b"},
		{"c", "d"},
	}
	result, err := f.Format(data)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if !strings.Contains(result, "a") {
		t.Error("expected 'a' in output")
	}
}

func TestFormatWithGlobals_Table(t *testing.T) {
	globals := &GlobalFlags{Format: "table"}
	data := map[string]string{"key": "val"}
	result, err := FormatWithGlobals(globals, data)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if !strings.Contains(result, "key") {
		t.Errorf("expected 'key' in output, got: %s", result)
	}
}

func TestCLI_Run_GlobalFlagParseError(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	cli := NewWithWriters("olb", "0.1.0", &out, &errOut)

	// --format with invalid value triggers error
	err := cli.Run([]string{"--format=xml"})
	if err == nil {
		t.Error("expected error for invalid global flag")
	}
}

// TestParseArgs_GlobalFlagsBeforeCommand tests ParseArgs skipping global flags
func TestParseArgs_GlobalFlagsBeforeCommand(t *testing.T) {
	args, err := ParseArgs([]string{"--format", "json", "start", "--port", "8080"})
	if err != nil {
		t.Fatalf("ParseArgs failed: %v", err)
	}
	if args.Command != "start" {
		t.Errorf("expected command 'start', got %q", args.Command)
	}
}

// TestParseArgs_ShortFlagsBeforeCommand tests ParseArgs skipping short global flags
func TestParseArgs_ShortFlagsBeforeCommand(t *testing.T) {
	args, err := ParseArgs([]string{"-f=json", "run", "sub1", "--verbose"})
	if err != nil {
		t.Fatalf("ParseArgs failed: %v", err)
	}
	if args.Command != "run" {
		t.Errorf("expected command 'run', got %q", args.Command)
	}
	if args.Subcommand != "sub1" {
		t.Errorf("expected subcommand 'sub1', got %q", args.Subcommand)
	}
}

// TestParseArgs_FlagWithValueBeforeCommand tests ParseArgs skipping --flag value before command
func TestParseArgs_FlagWithValueBeforeCommand(t *testing.T) {
	// --output=dir starts with --, has =, so i++ only; then "deploy" is the command
	args, err := ParseArgs([]string{"--output=dir", "deploy", "web", "--verbose"})
	if err != nil {
		t.Fatalf("ParseArgs failed: %v", err)
	}
	if args.Command != "deploy" {
		t.Errorf("expected command 'deploy', got %q", args.Command)
	}
	if args.Subcommand != "web" {
		t.Errorf("expected subcommand 'web', got %q", args.Subcommand)
	}
	if args.Flags["verbose"] != "true" {
		t.Errorf("expected verbose=true, got %q", args.Flags["verbose"])
	}
}

// TestParseArgs_ShortFlagWithValueBeforeCommand tests ParseArgs skipping -f=val before command
func TestParseArgs_ShortFlagWithValueBeforeCommand(t *testing.T) {
	args, err := ParseArgs([]string{"-n=3", "run"})
	if err != nil {
		t.Fatalf("ParseArgs failed: %v", err)
	}
	if args.Command != "run" {
		t.Errorf("expected command 'run', got %q", args.Command)
	}
}

// TestParseGlobalFlags_FormatShortEqual tests -f=json format
func TestParseGlobalFlags_FormatShortEqual(t *testing.T) {
	globals, remaining, err := ParseGlobalFlags([]string{"-f=json"})
	if err != nil {
		t.Fatalf("ParseGlobalFlags failed: %v", err)
	}
	if globals.Format != "json" {
		t.Errorf("expected format 'json', got %q", globals.Format)
	}
	if len(remaining) != 0 {
		t.Errorf("expected no remaining args, got %v", remaining)
	}
}

// TestParseGlobalFlags_FormatShortInvalid tests -f with invalid format value
func TestParseGlobalFlags_FormatShortInvalid(t *testing.T) {
	_, _, err := ParseGlobalFlags([]string{"-f", "xml"})
	if err == nil {
		t.Error("expected error for invalid format with -f")
	}
	if !strings.Contains(err.Error(), "invalid format") {
		t.Errorf("expected 'invalid format' error, got %v", err)
	}
}

// TestParseGlobalFlags_FormatShortMissingValue tests -f without value
func TestParseGlobalFlags_FormatShortMissingValue(t *testing.T) {
	_, _, err := ParseGlobalFlags([]string{"-f"})
	if err == nil {
		t.Error("expected error for -f without value")
	}
	if !strings.Contains(err.Error(), "-f requires a value") {
		t.Errorf("expected '-f requires a value' error, got %v", err)
	}
}

// TestParseGlobalFlags_FormatLongInvalid tests --format with invalid value
func TestParseGlobalFlags_FormatLongInvalid(t *testing.T) {
	_, _, err := ParseGlobalFlags([]string{"--format", "xml"})
	if err == nil {
		t.Error("expected error for invalid format with --format")
	}
	if !strings.Contains(err.Error(), "invalid format") {
		t.Errorf("expected 'invalid format' error, got %v", err)
	}
}
