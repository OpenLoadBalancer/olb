// Package cli provides the command-line interface for OpenLoadBalancer.
package cli

import (
	"flag"
)

// TopCommand implements the "olb top" TUI dashboard command.
type TopCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *TopCommand) Name() string {
	return "top"
}

// Description returns the command description.
func (c *TopCommand) Description() string {
	return "Interactive TUI dashboard for real-time monitoring"
}

// Run executes the top command.
func (c *TopCommand) Run(args []string) error {
	fs := flag.NewFlagSet("top", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Create metrics fetcher
	fetcher := NewMetricsFetcher(c.apiAddr)

	// Create and run TUI
	tui := NewTUI(fetcher)
	return tui.Run()
}
