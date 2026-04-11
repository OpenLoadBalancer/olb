package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
)

type RouteAddCommand struct {
	apiAddr  string
	backend  string
	priority int
}

// Name returns the command name.
func (c *RouteAddCommand) Name() string {
	return "route-add"
}

// Description returns the command description.
func (c *RouteAddCommand) Description() string {
	return "Add a new route"
}

// Run executes the route-add command.
func (c *RouteAddCommand) Run(args []string) error {
	fs := flag.NewFlagSet("route-add", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.backend, "backend", "", "Backend pool name")
	fs.IntVar(&c.priority, "priority", 100, "Route priority")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb route add <path> [--backend <pool>] [--priority N]")
	}

	path := remaining[0]

	if c.backend == "" {
		return fmt.Errorf("--backend is required")
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	route := map[string]any{
		"path":         path,
		"backend_pool": c.backend,
		"priority":     c.priority,
	}

	if err := client.post("/routes", route, nil); err != nil {
		return fmt.Errorf("failed to add route: %w", err)
	}

	fmt.Printf("Route '%s' added with backend '%s' and priority %d\n", path, c.backend, c.priority)
	return nil
}

// RouteRemoveCommand removes a route
type RouteRemoveCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *RouteRemoveCommand) Name() string {
	return "route-remove"
}

// Description returns the command description.
func (c *RouteRemoveCommand) Description() string {
	return "Remove a route"
}

// Run executes the route-remove command.
func (c *RouteRemoveCommand) Run(args []string) error {
	fs := flag.NewFlagSet("route-remove", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb route remove <path>")
	}

	path := remaining[0]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	// URL encode the path
	encodedPath := strings.ReplaceAll(path, "/", "%2F")
	url := fmt.Sprintf("/routes/%s", encodedPath)

	if err := client.delete(url); err != nil {
		return fmt.Errorf("failed to remove route: %w", err)
	}

	fmt.Printf("Route '%s' removed\n", path)
	return nil
}

// RouteTestCommand tests a route
type RouteTestCommand struct {
	apiAddr string
	format  string
}

// Name returns the command name.
func (c *RouteTestCommand) Name() string {
	return "route-test"
}

// Description returns the command description.
func (c *RouteTestCommand) Description() string {
	return "Test a route"
}

// Run executes the route-test command.
func (c *RouteTestCommand) Run(args []string) error {
	fs := flag.NewFlagSet("route-test", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "table", "Output format (json or table)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb route test <path>")
	}

	path := remaining[0]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	// URL encode the path
	encodedPath := strings.ReplaceAll(path, "/", "%2F")
	url := fmt.Sprintf("/routes/test?path=%s", encodedPath)

	var result map[string]any
	if err := client.get(url, &result); err != nil {
		return fmt.Errorf("failed to test route: %w", err)
	}

	switch c.format {
	case "json":
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "table":
		fmt.Printf("Route Test: %s\n", path)
		fmt.Println(strings.Repeat("=", 40))
		for k, v := range result {
			fmt.Printf("%-15s: %v\n", k, v)
		}
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}
