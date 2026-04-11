package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

type CertListCommand struct {
	apiAddr string
	format  string
}

// Name returns the command name.
func (c *CertListCommand) Name() string {
	return "cert-list"
}

// Description returns the command description.
func (c *CertListCommand) Description() string {
	return "List certificates"
}

// Run executes the cert-list command.
func (c *CertListCommand) Run(args []string) error {
	fs := flag.NewFlagSet("cert-list", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "table", "Output format (json or table)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	var certs []map[string]any
	if err := client.get("/certificates", &certs); err != nil {
		return fmt.Errorf("failed to list certificates: %w", err)
	}

	switch c.format {
	case "json":
		data, err := json.MarshalIndent(certs, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "table":
		fmt.Println("Certificates")
		fmt.Println("============")
		fmt.Printf("%-30s %-20s %-15s %-10s\n", "Domain", "Issuer", "Expiry", "Auto")
		fmt.Println(strings.Repeat("-", 80))
		for _, cert := range certs {
			domain, _ := cert["domain"].(string)
			issuer, _ := cert["issuer"].(string)
			expiry, _ := cert["expiry"].(string)
			auto, _ := cert["auto"].(bool)
			autoStr := "no"
			if auto {
				autoStr = "yes"
			}
			fmt.Printf("%-30s %-20s %-15s %-10s\n", domain, issuer, expiry, autoStr)
		}
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}

// CertAddCommand adds a certificate
type CertAddCommand struct {
	apiAddr  string
	auto     bool
	certFile string
	keyFile  string
}

// Name returns the command name.
func (c *CertAddCommand) Name() string {
	return "cert-add"
}

// Description returns the command description.
func (c *CertAddCommand) Description() string {
	return "Add a certificate"
}

// Run executes the cert-add command.
func (c *CertAddCommand) Run(args []string) error {
	fs := flag.NewFlagSet("cert-add", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.BoolVar(&c.auto, "auto", false, "Use ACME/Let's Encrypt")
	fs.StringVar(&c.certFile, "cert", "", "Certificate file path")
	fs.StringVar(&c.keyFile, "key", "", "Private key file path")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb cert add <domain> [--auto] [--cert <file> --key <file>]")
	}

	domain := remaining[0]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	req := map[string]any{
		"domain": domain,
		"auto":   c.auto,
	}

	if !c.auto {
		if c.certFile == "" || c.keyFile == "" {
			return fmt.Errorf("--cert and --key are required when not using --auto")
		}

		certData, err := os.ReadFile(c.certFile)
		if err != nil {
			return fmt.Errorf("failed to read certificate file: %w", err)
		}

		keyData, err := os.ReadFile(c.keyFile)
		if err != nil {
			return fmt.Errorf("failed to read key file: %w", err)
		}

		req["certificate"] = string(certData)
		req["key"] = string(keyData)
	}

	if err := client.post("/certificates", req, nil); err != nil {
		return fmt.Errorf("failed to add certificate: %w", err)
	}

	if c.auto {
		fmt.Printf("Certificate for '%s' will be provisioned via ACME\n", domain)
	} else {
		fmt.Printf("Certificate for '%s' added successfully\n", domain)
	}
	return nil
}

// CertRemoveCommand removes a certificate
type CertRemoveCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *CertRemoveCommand) Name() string {
	return "cert-remove"
}

// Description returns the command description.
func (c *CertRemoveCommand) Description() string {
	return "Remove a certificate"
}

// Run executes the cert-remove command.
func (c *CertRemoveCommand) Run(args []string) error {
	fs := flag.NewFlagSet("cert-remove", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb cert remove <domain>")
	}

	domain := remaining[0]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	url := fmt.Sprintf("/certificates/%s", domain)
	if err := client.delete(url); err != nil {
		return fmt.Errorf("failed to remove certificate: %w", err)
	}

	fmt.Printf("Certificate for '%s' removed\n", domain)
	return nil
}

// CertRenewCommand renews a certificate
type CertRenewCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *CertRenewCommand) Name() string {
	return "cert-renew"
}

// Description returns the command description.
func (c *CertRenewCommand) Description() string {
	return "Renew a certificate"
}

// Run executes the cert-renew command.
func (c *CertRenewCommand) Run(args []string) error {
	fs := flag.NewFlagSet("cert-renew", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb cert renew <domain>")
	}

	domain := remaining[0]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	url := fmt.Sprintf("/certificates/%s/renew", domain)
	if err := client.post(url, nil, nil); err != nil {
		return fmt.Errorf("failed to renew certificate: %w", err)
	}

	fmt.Printf("Certificate for '%s' renewed successfully\n", domain)
	return nil
}

// CertInfoCommand shows certificate information
type CertInfoCommand struct {
	apiAddr string
	format  string
}

// Name returns the command name.
func (c *CertInfoCommand) Name() string {
	return "cert-info"
}

// Description returns the command description.
func (c *CertInfoCommand) Description() string {
	return "Show certificate information"
}

// Run executes the cert-info command.
func (c *CertInfoCommand) Run(args []string) error {
	fs := flag.NewFlagSet("cert-info", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "table", "Output format (json or table)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return fmt.Errorf("usage: olb cert info <domain>")
	}

	domain := remaining[0]

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	url := fmt.Sprintf("/certificates/%s", domain)
	var cert map[string]any
	if err := client.get(url, &cert); err != nil {
		return fmt.Errorf("failed to get certificate info: %w", err)
	}

	switch c.format {
	case "json":
		data, err := json.MarshalIndent(cert, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "table":
		fmt.Printf("Certificate: %s\n", domain)
		fmt.Println(strings.Repeat("=", 40))
		for k, v := range cert {
			fmt.Printf("%-15s: %v\n", k, v)
		}
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}
