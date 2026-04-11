package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

type MetricsShowCommand struct {
	apiAddr string
	format  string
}

// Name returns the command name.
func (c *MetricsShowCommand) Name() string {
	return "metrics-show"
}

// Description returns the command description.
func (c *MetricsShowCommand) Description() string {
	return "Show metrics"
}

// Run executes the metrics-show command.
func (c *MetricsShowCommand) Run(args []string) error {
	fs := flag.NewFlagSet("metrics-show", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "table", "Output format (json, table, or prometheus)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	switch c.format {
	case "json":
		metrics, err := client.GetMetricsJSON()
		if err != nil {
			return fmt.Errorf("failed to get metrics: %w", err)
		}
		data, err := json.MarshalIndent(metrics, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "prometheus":
		metrics, err := client.GetMetricsPrometheus()
		if err != nil {
			return fmt.Errorf("failed to get metrics: %w", err)
		}
		fmt.Println(metrics)
	case "table":
		metrics, err := client.GetMetricsJSON()
		if err != nil {
			return fmt.Errorf("failed to get metrics: %w", err)
		}
		fmt.Println("Metrics")
		fmt.Println("=======")
		for k, v := range metrics {
			fmt.Printf("%-30s: %v\n", k, v)
		}
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}

// MetricsExportCommand exports metrics
type MetricsExportCommand struct {
	apiAddr string
	output  string
	format  string
}

// Name returns the command name.
func (c *MetricsExportCommand) Name() string {
	return "metrics-export"
}

// Description returns the command description.
func (c *MetricsExportCommand) Description() string {
	return "Export metrics to file"
}

// Run executes the metrics-export command.
func (c *MetricsExportCommand) Run(args []string) error {
	fs := flag.NewFlagSet("metrics-export", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.output, "output", "metrics.json", "Output file path")
	fs.StringVar(&c.format, "format", "json", "Export format (json or prometheus)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	var data []byte

	switch c.format {
	case "json":
		metrics, err := client.GetMetricsJSON()
		if err != nil {
			return fmt.Errorf("failed to get metrics: %w", err)
		}
		data, err = json.MarshalIndent(metrics, "", "  ")
		if err != nil {
			return err
		}
	case "prometheus":
		metrics, err := client.GetMetricsPrometheus()
		if err != nil {
			return fmt.Errorf("failed to get metrics: %w", err)
		}
		data = []byte(metrics)
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	if err := os.WriteFile(c.output, data, 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	fmt.Printf("Metrics exported to %s\n", c.output)
	return nil
}
