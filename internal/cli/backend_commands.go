package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"strings"
)

type BackendAddCommand struct {
	apiAddr     string
	weight      int
	healthCheck string
}

// Name returns the command name.
func (c *BackendAddCommand) Name() string {
	return "backend-add"
}

// Description returns the command description.
func (c *BackendAddCommand) Description() string {
	return "Add a backend to a pool"
}

// Run executes the backend-add command.
func (c *BackendAddCommand) Run(args []string) error {
	fs := flag.NewFlagSet("backend-add", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.IntVar(&c.weight, "weight", 100, "Backend weight")
	fs.StringVar(&c.healthCheck, "health-check", "", "Health check path")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 2 {
		return fmt.Errorf("usage: olb backend add <pool> <address> [--weight N] [--health-check path]")
	}

	pool := remaining[0]
	address := remaining[1]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	req := &AddBackendRequest{
		ID:      address,
		Address: address,
		Weight:  c.weight,
	}

	if err := client.AddBackend(pool, req); err != nil {
		return fmt.Errorf("failed to add backend: %w", err)
	}

	fmt.Printf("Backend '%s' added to pool '%s' with weight %d\n", address, pool, c.weight)
	return nil
}

// BackendRemoveCommand removes a backend from a pool
type BackendRemoveCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *BackendRemoveCommand) Name() string {
	return "backend-remove"
}

// Description returns the command description.
func (c *BackendRemoveCommand) Description() string {
	return "Remove a backend from a pool"
}

// Run executes the backend-remove command.
func (c *BackendRemoveCommand) Run(args []string) error {
	fs := flag.NewFlagSet("backend-remove", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 2 {
		return fmt.Errorf("usage: olb backend remove <pool> <address>")
	}

	pool := remaining[0]
	address := remaining[1]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	if err := client.RemoveBackend(pool, address); err != nil {
		return fmt.Errorf("failed to remove backend: %w", err)
	}

	fmt.Printf("Backend '%s' removed from pool '%s'\n", address, pool)
	return nil
}

// BackendDrainCommand marks a backend as draining
type BackendDrainCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *BackendDrainCommand) Name() string {
	return "backend-drain"
}

// Description returns the command description.
func (c *BackendDrainCommand) Description() string {
	return "Mark a backend as draining"
}

// Run executes the backend-drain command.
func (c *BackendDrainCommand) Run(args []string) error {
	fs := flag.NewFlagSet("backend-drain", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 2 {
		return fmt.Errorf("usage: olb backend drain <pool> <address>")
	}

	pool := remaining[0]
	address := remaining[1]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	if err := client.DrainBackend(pool, address); err != nil {
		return fmt.Errorf("failed to drain backend: %w", err)
	}

	fmt.Printf("Backend '%s' in pool '%s' is now draining\n", address, pool)
	return nil
}

// BackendEnableCommand enables a backend
type BackendEnableCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *BackendEnableCommand) Name() string {
	return "backend-enable"
}

// Description returns the command description.
func (c *BackendEnableCommand) Description() string {
	return "Enable a backend"
}

// Run executes the backend-enable command.
func (c *BackendEnableCommand) Run(args []string) error {
	fs := flag.NewFlagSet("backend-enable", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 2 {
		return fmt.Errorf("usage: olb backend enable <pool> <address>")
	}

	pool := remaining[0]
	address := remaining[1]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	// Use the generic doRequest to enable backend
	url := fmt.Sprintf("/backends/%s/backends/%s/enable", pool, address)
	resp, err := client.doRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to enable backend: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("failed to enable backend: HTTP %d", resp.StatusCode)
	}

	fmt.Printf("Backend '%s' in pool '%s' enabled\n", address, pool)
	return nil
}

// BackendDisableCommand disables a backend
type BackendDisableCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *BackendDisableCommand) Name() string {
	return "backend-disable"
}

// Description returns the command description.
func (c *BackendDisableCommand) Description() string {
	return "Disable a backend"
}

// Run executes the backend-disable command.
func (c *BackendDisableCommand) Run(args []string) error {
	fs := flag.NewFlagSet("backend-disable", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 2 {
		return fmt.Errorf("usage: olb backend disable <pool> <address>")
	}

	pool := remaining[0]
	address := remaining[1]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	url := fmt.Sprintf("/backends/%s/backends/%s/disable", pool, address)
	resp, err := client.doRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to disable backend: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("failed to disable backend: HTTP %d", resp.StatusCode)
	}

	fmt.Printf("Backend '%s' in pool '%s' disabled\n", address, pool)
	return nil
}

// BackendStatsCommand shows backend statistics
type BackendStatsCommand struct {
	apiAddr string
	format  string
}

// Name returns the command name.
func (c *BackendStatsCommand) Name() string {
	return "backend-stats"
}

// Description returns the command description.
func (c *BackendStatsCommand) Description() string {
	return "Show backend statistics"
}

// Run executes the backend-stats command.
func (c *BackendStatsCommand) Run(args []string) error {
	fs := flag.NewFlagSet("backend-stats", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "table", "Output format (json or table)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb backend stats <pool>")
	}

	pool := remaining[0]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	poolInfo, err := client.GetPool(pool)
	if err != nil {
		return fmt.Errorf("failed to get pool stats: %w", err)
	}

	switch c.format {
	case "json":
		data, err := json.MarshalIndent(poolInfo, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "table":
		fmt.Printf("Pool: %s\n", poolInfo.Name)
		fmt.Printf("Algorithm: %s\n", poolInfo.Algorithm)
		fmt.Println("\nBackends:")
		fmt.Printf("%-20s %-25s %-8s %-10s %-10s %-10s\n", "ID", "Address", "Weight", "State", "Healthy", "Requests")
		fmt.Println(strings.Repeat("-", 85))
		for _, b := range poolInfo.Backends {
			healthy := "no"
			if b.Healthy {
				healthy = "yes"
			}
			fmt.Printf("%-20s %-25s %-8d %-10s %-10s %-10d\n", b.ID, b.Address, b.Weight, b.State, healthy, b.Requests)
		}
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}
