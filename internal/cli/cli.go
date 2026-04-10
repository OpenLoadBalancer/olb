// Package cli provides the command-line interface for OpenLoadBalancer.
// It supports command registration, argument parsing, and output formatting.
package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// CLI is the command-line interface
type CLI struct {
	name     string
	version  string
	out      io.Writer
	err      io.Writer
	commands map[string]Command
}

// Command is a CLI command
type Command interface {
	Name() string
	Description() string
	Run(args []string) error
}

// New creates a new CLI instance with the given name and version.
// Output defaults to os.Stdout and os.Stderr.
func New(name, version string) *CLI {
	return &CLI{
		name:     name,
		version:  version,
		out:      os.Stdout,
		err:      os.Stderr,
		commands: make(map[string]Command),
	}
}

// NewWithWriters creates a new CLI instance with custom output writers.
// This is primarily used for testing.
func NewWithWriters(name, version string, out, errOut io.Writer) *CLI {
	return &CLI{
		name:     name,
		version:  version,
		out:      out,
		err:      errOut,
		commands: make(map[string]Command),
	}
}

// Register adds a command to the CLI.
// Commands must have unique names; registering a command with an existing
// name will overwrite the previous command.
func (c *CLI) Register(cmd Command) {
	c.commands[cmd.Name()] = cmd
}

// Run executes the CLI with the given arguments.
// It parses global flags, finds the appropriate command, and executes it.
// Returns an error if the command fails or if an unknown command is specified.
func (c *CLI) Run(args []string) error {
	// Parse global flags first
	globals, remaining, err := ParseGlobalFlags(args)
	if err != nil {
		return fmt.Errorf("failed to parse global flags: %w", err)
	}

	// Handle --help for general help
	if globals.Help {
		if len(remaining) == 0 {
			fmt.Fprint(c.out, c.Help())
			return nil
		}
		// Help for a specific command
		cmdName := remaining[0]
		if cmd, ok := c.commands[cmdName]; ok {
			fmt.Fprintf(c.out, "Usage: %s %s [options]\n\n%s\n", c.name, cmd.Name(), cmd.Description())
			return nil
		}
		return fmt.Errorf("unknown command: %s", cmdName)
	}

	// Handle --version
	if globals.Version {
		fmt.Fprintf(c.out, "%s version %s\n", c.name, c.version)
		return nil
	}

	// No command specified
	if len(remaining) == 0 {
		fmt.Fprint(c.out, c.Help())
		return nil
	}

	// Find and run command
	cmdName := remaining[0]
	cmd, ok := c.commands[cmdName]
	if !ok {
		return fmt.Errorf("unknown command: %s", cmdName)
	}

	// Pass remaining args (after command name) to the command
	cmdArgs := remaining[1:]
	return cmd.Run(cmdArgs)
}

// Help generates and returns the help text for the CLI.
// It lists all available commands with their descriptions.
func (c *CLI) Help() string {
	var b strings.Builder

	fmt.Fprintf(&b, "Usage: %s [global options] <command> [command options]\n\n", c.name)
	fmt.Fprintf(&b, "%s - OpenLoadBalancer CLI\n\n", c.name)

	fmt.Fprintln(&b, "Global Options:")
	fmt.Fprintln(&b, "  -h, --help     Show help")
	fmt.Fprintln(&b, "  -v, --version  Show version")
	fmt.Fprintln(&b, "  --format       Output format: json or table (default: table)")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "Commands:")

	// Calculate max command name length for alignment
	maxLen := 0
	for name := range c.commands {
		if len(name) > maxLen {
			maxLen = len(name)
		}
	}

	// Sort commands for consistent output
	var names []string
	for name := range c.commands {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		cmd := c.commands[name]
		fmt.Fprintf(&b, "  %-*s  %s\n", maxLen, cmd.Name(), cmd.Description())
	}

	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Use '%s <command> --help' for more information about a command.\n", c.name)

	return b.String()
}

// Commands returns a slice of all registered commands.
// The returned slice is sorted by command name.
func (c *CLI) Commands() []Command {
	var names []string
	for name := range c.commands {
		names = append(names, name)
	}

	// Sort for consistent ordering
	sort.Strings(names)

	var result []Command
	for _, name := range names {
		result = append(result, c.commands[name])
	}
	return result
}

// Command returns a command by name, or nil if not found.
func (c *CLI) Command(name string) Command {
	return c.commands[name]
}

// Name returns the CLI name.
func (c *CLI) Name() string {
	return c.name
}

// Version returns the CLI version.
func (c *CLI) Version() string {
	return c.version
}
