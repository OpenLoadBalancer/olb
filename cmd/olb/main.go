// olb - OpenLoadBalancer
// A zero-dependency, production-grade load balancer written in Go.
//
// Usage:
//
//	olb start --config /path/to/config.yaml
//	olb stop
//	olb reload
//	olb status
//	olb version
//	olb --help
//
// For more information, visit: https://github.com/openloadbalancer/olb
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/openloadbalancer/olb/internal/cli"
	"github.com/openloadbalancer/olb/pkg/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	c := cli.NewWithWriters("olb", version.String(), stdout, stderr)

	c.Register(&cli.StartCommand{})
	c.Register(&cli.StopCommand{})
	c.Register(&cli.ReloadCommand{})
	c.Register(&cli.StatusCommand{})
	c.Register(&cli.VersionCommand{})
	c.Register(&cli.ConfigCommand{})
	c.Register(&cli.BackendCommand{})
	c.Register(&cli.HealthCommand{})
	c.Register(&cli.SetupCommand{})

	if err := c.Run(args); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}
