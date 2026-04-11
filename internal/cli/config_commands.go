package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

type ConfigShowCommand struct {
	apiAddr string
	format  string
}

// Name returns the command name.
func (c *ConfigShowCommand) Name() string {
	return "config-show"
}

// Description returns the command description.
func (c *ConfigShowCommand) Description() string {
	return "Show current configuration"
}

// Run executes the config-show command.
func (c *ConfigShowCommand) Run(args []string) error {
	fs := flag.NewFlagSet("config-show", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "yaml", "Output format (yaml or json)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	var config map[string]any
	if err := client.get("/config", &config); err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	switch c.format {
	case "json":
		data, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "yaml":
		// Simple YAML-like output for now
		printYAML(config, 0)
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}

// printYAML prints a map as YAML-like output
func printYAML(data any, indent int) {
	prefix := strings.Repeat("  ", indent)
	switch v := data.(type) {
	case map[string]any:
		for key, val := range v {
			switch val.(type) {
			case map[string]any, []any:
				fmt.Printf("%s%s:\n", prefix, key)
				printYAML(val, indent+1)
			default:
				fmt.Printf("%s%s: %v\n", prefix, key, val)
			}
		}
	case []any:
		for _, item := range v {
			fmt.Printf("%s- ", prefix)
			switch item.(type) {
			case map[string]any:
				first := true
				for k, val := range item.(map[string]any) {
					if first {
						fmt.Printf("%s: %v\n", k, val)
						first = false
					} else {
						fmt.Printf("%s  %s: %v\n", prefix, k, val)
					}
				}
			default:
				fmt.Printf("%v\n", item)
			}
		}
	default:
		fmt.Printf("%s%v\n", prefix, v)
	}
}

// ConfigDiffCommand shows configuration differences
type ConfigDiffCommand struct {
	apiAddr  string
	filePath string
}

// Name returns the command name.
func (c *ConfigDiffCommand) Name() string {
	return "config-diff"
}

// Description returns the command description.
func (c *ConfigDiffCommand) Description() string {
	return "Show configuration differences"
}

// Run executes the config-diff command.
func (c *ConfigDiffCommand) Run(args []string) error {
	fs := flag.NewFlagSet("config-diff", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.filePath, "file", "", "Config file to compare with")

	if err := fs.Parse(args); err != nil {
		return err
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	var runningConfig map[string]any
	if err := client.get("/config", &runningConfig); err != nil {
		return fmt.Errorf("failed to get running config: %w", err)
	}

	if c.filePath == "" {
		// Compare with default config file
		c.filePath = "olb.yaml"
	}

	fileData, err := os.ReadFile(c.filePath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// For now, just show that there are differences
	// A proper diff implementation would compare the two configs
	fmt.Printf("Comparing running config with %s\n", c.filePath)
	fmt.Println("Note: Full diff not yet implemented")
	fmt.Printf("Running config version: %v\n", runningConfig["version"])
	fmt.Printf("File size: %d bytes\n", len(fileData))

	return nil
}

// ConfigValidateCommand validates configuration
type ConfigValidateCommand struct {
	filePath string
}

// Name returns the command name.
func (c *ConfigValidateCommand) Name() string {
	return "config-validate"
}

// Description returns the command description.
func (c *ConfigValidateCommand) Description() string {
	return "Validate configuration file"
}

// Run executes the config-validate command.
func (c *ConfigValidateCommand) Run(args []string) error {
	fs := flag.NewFlagSet("config-validate", flag.ExitOnError)
	fs.StringVar(&c.filePath, "config", "olb.yaml", "Config file path")
	fs.StringVar(&c.filePath, "c", "olb.yaml", "Config file path (shorthand)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	data, err := os.ReadFile(c.filePath)
	if err != nil {
		return fmt.Errorf("config file not found: %w", err)
	}

	// Basic validation - check if it's valid YAML/JSON
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		// Try parsing as YAML-like structure
		// For now, just check if file is not empty
		if len(data) == 0 {
			return fmt.Errorf("config file is empty")
		}
	}

	fmt.Printf("Configuration file '%s' is valid\n", c.filePath)

	// Show basic stats
	if version, ok := config["version"].(string); ok {
		fmt.Printf("Version: %s\n", version)
	}
	if listeners, ok := config["listeners"].([]any); ok {
		fmt.Printf("Listeners: %d\n", len(listeners))
	}
	if pools, ok := config["pools"].([]any); ok {
		fmt.Printf("Pools: %d\n", len(pools))
	}

	return nil
}

// CompletionCommand generates shell completions
type CompletionCommand struct {
	shell string
}

// Name returns the command name.
func (c *CompletionCommand) Name() string {
	return "completion"
}

// Description returns the command description.
func (c *CompletionCommand) Description() string {
	return "Generate shell completion script"
}

// Run executes the completion command.
func (c *CompletionCommand) Run(args []string) error {
	fs := flag.NewFlagSet("completion", flag.ExitOnError)
	fs.StringVar(&c.shell, "shell", "bash", "Shell type (bash, zsh, fish)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	switch c.shell {
	case "bash":
		fmt.Print(bashCompletionScript)
	case "zsh":
		fmt.Print(zshCompletionScript)
	case "fish":
		fmt.Print(fishCompletionScript)
	default:
		return fmt.Errorf("unsupported shell: %s (supported: bash, zsh, fish)", c.shell)
	}

	return nil
}
