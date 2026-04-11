package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// SetupCommand interactively generates an initial configuration file.
type SetupCommand struct{}

func (s *SetupCommand) Name() string        { return "setup" }
func (s *SetupCommand) Description() string { return "Interactive configuration wizard" }

func (s *SetupCommand) Run(args []string) error {
	var outputPath string
	for i, a := range args {
		if a == "--output" || a == "-o" {
			if i+1 < len(args) {
				outputPath = args[i+1]
			}
		}
	}
	if outputPath == "" {
		outputPath = "olb.yaml"
	}

	fmt.Println("╔══════════════════════════════════════════════════╗")
	fmt.Println("║        OpenLoadBalancer Configuration Wizard      ║")
	fmt.Println("╚══════════════════════════════════════════════════╝")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	// --- Admin ---
	fmt.Println("── Admin API ──")
	adminAddr := prompt(reader, "Admin listen address", "127.0.0.1:8081")
	adminUser := prompt(reader, "Admin username (leave empty for no auth)", "")
	adminPass := ""
	if adminUser != "" {
		adminPass = promptSecret(reader, "Admin password")
	}

	// --- Listener ---
	fmt.Println()
	fmt.Println("── Listener ──")
	listenName := prompt(reader, "Listener name", "http")
	listenAddr := prompt(reader, "Listen address", ":8080")
	protoOptions := []string{"http", "https", "tcp", "udp"}
	listenProto := protoOptions[promptChoice(reader, "Protocol", protoOptions, 0)]

	// --- Pool ---
	fmt.Println()
	fmt.Println("── Backend Pool ──")
	poolName := prompt(reader, "Pool name", "web")
	algIdx := promptChoice(reader, "Load balancing algorithm", []string{
		"round_robin", "weighted_round_robin", "least_connections",
		"ip_hash", "consistent_hash", "random", "power_of_two",
	}, 0)

	// --- Backends ---
	fmt.Println()
	fmt.Println("── Backends ──")
	var backends []backendEntry
	for {
		addr := prompt(reader, "Backend address (host:port)", fmt.Sprintf("127.0.0.1:%s", firstFreePort()))
		weight := promptInt(reader, "Weight", 1)
		backends = append(backends, backendEntry{addr, weight})

		if promptYesNo(reader, "Add another backend?", false) {
			continue
		}
		break
	}

	// --- Health Check ---
	fmt.Println()
	fmt.Println("── Health Check ──")
	enableHC := promptYesNo(reader, "Enable health checks?", true)
	var hcType, hcPath, hcInterval string
	if enableHC {
		hcOptions := []string{"http", "tcp", "grpc"}
		hcType = hcOptions[promptChoice(reader, "Health check type", hcOptions, 0)]
		if hcType == "http" || hcType == "grpc" {
			hcPath = prompt(reader, "Health check path", "/health")
		}
		hcInterval = prompt(reader, "Check interval", "10s")
	}

	// --- Middleware ---
	fmt.Println()
	fmt.Println("── Middleware ──")
	enableRateLimit := promptYesNo(reader, "Enable rate limiting?", false)
	var rps int
	if enableRateLimit {
		rps = promptInt(reader, "Requests per second", 1000)
	}
	enableCORS := promptYesNo(reader, "Enable CORS?", false)
	enableCompression := promptYesNo(reader, "Enable gzip compression?", true)

	// --- Generate config ---
	config := generateConfig(configParams{
		adminAddr:         adminAddr,
		adminUser:         adminUser,
		adminPass:         adminPass,
		listenName:        listenName,
		listenAddr:        listenAddr,
		listenProto:       listenProto,
		poolName:          poolName,
		algorithm:         []string{"round_robin", "weighted_round_robin", "least_connections", "ip_hash", "consistent_hash", "random", "power_of_two"}[algIdx],
		backends:          backends,
		enableHC:          enableHC,
		hcType:            hcType,
		hcPath:            hcPath,
		hcInterval:        hcInterval,
		enableRateLimit:   enableRateLimit,
		rps:               rps,
		enableCORS:        enableCORS,
		enableCompression: enableCompression,
	})

	if err := os.WriteFile(outputPath, []byte(config), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", outputPath, err)
	}

	fmt.Println()
	fmt.Printf("Configuration written to %s\n", outputPath)
	fmt.Println()
	fmt.Println("Start OpenLoadBalancer with:")
	fmt.Printf("  olb start --config %s\n", outputPath)
	fmt.Println()
	return nil
}

// --- Types ---

type backendEntry struct {
	address string
	weight  int
}

type configParams struct {
	adminAddr, adminUser, adminPass     string
	listenName, listenAddr, listenProto string
	poolName, algorithm                 string
	backends                            []backendEntry
	enableHC                            bool
	hcType, hcPath, hcInterval          string
	enableRateLimit                     bool
	rps                                 int
	enableCORS, enableCompression       bool
}

// --- Prompt helpers ---

func prompt(r *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("  %s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("  %s: ", label)
	}
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}

func promptSecret(r *bufio.Reader, label string) string {
	fmt.Printf("  %s: ", label)
	raw, _ := r.ReadString('\n')
	return strings.TrimSpace(raw)
}

func promptInt(r *bufio.Reader, label string, defaultVal int) int {
	s := prompt(r, label, strconv.Itoa(defaultVal))
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return defaultVal
}

func promptYesNo(r *bufio.Reader, label string, defaultVal bool) bool {
	defStr := "y"
	if !defaultVal {
		defStr = "n"
	}
	s := prompt(r, label+" (y/n)", defStr)
	switch strings.ToLower(s) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		return defaultVal
	}
}

func promptChoice(r *bufio.Reader, label string, options []string, defaultIdx int) int {
	fmt.Printf("  %s:\n", label)
	for i, o := range options {
		fmt.Printf("    %d) %s\n", i+1, o)
	}
	s := prompt(r, "Choose", strconv.Itoa(defaultIdx+1))
	if v, err := strconv.Atoi(s); err == nil && v >= 1 && v <= len(options) {
		return v - 1
	}
	return defaultIdx
}

func firstFreePort() string {
	return "8080"
}

// --- Config generator ---

func generateConfig(p configParams) string {
	var b strings.Builder

	// Admin
	b.WriteString("# OpenLoadBalancer Configuration\n")
	b.WriteString("# Generated by: olb setup\n\n")
	b.WriteString("admin:\n")
	fmt.Fprintf(&b, "  address: \"%s\"\n", p.adminAddr)
	if p.adminUser != "" {
		fmt.Fprintf(&b, "  username: \"%s\"\n", p.adminUser)
		fmt.Fprintf(&b, "  password: \"%s\"\n", p.adminPass)
	}

	// Middleware
	if p.enableRateLimit || p.enableCORS || p.enableCompression {
		b.WriteString("\nmiddleware:\n")

	}
	if p.enableRateLimit {
		b.WriteString("  rate_limit:\n")
		b.WriteString("    enabled: true\n")
		fmt.Fprintf(&b, "    requests_per_second: %d\n", p.rps)
	}
	if p.enableCORS {
		b.WriteString("  cors:\n")
		b.WriteString("    enabled: true\n")
		b.WriteString("    allowed_origins: [\"*\"]\n")
	}
	if p.enableCompression {
		b.WriteString("  compression:\n")
		b.WriteString("    enabled: true\n")
	}

	// Listener
	b.WriteString("\nlisteners:\n")
	fmt.Fprintf(&b, "  - name: %s\n", p.listenName)
	fmt.Fprintf(&b, "    address: \"%s\"\n", p.listenAddr)
	fmt.Fprintf(&b, "    protocol: %s\n", p.listenProto)
	b.WriteString("    routes:\n")
	fmt.Fprintf(&b, "      - path: /\n")
	fmt.Fprintf(&b, "        pool: %s\n", p.poolName)

	// Pool
	b.WriteString("\npools:\n")
	fmt.Fprintf(&b, "  - name: %s\n", p.poolName)
	fmt.Fprintf(&b, "    algorithm: %s\n", p.algorithm)
	b.WriteString("    backends:\n")
	for _, be := range p.backends {
		fmt.Fprintf(&b, "      - address: \"%s\"\n", be.address)
		if be.weight != 1 {
			fmt.Fprintf(&b, "        weight: %d\n", be.weight)
		}
	}
	if p.enableHC {
		b.WriteString("    health_check:\n")
		fmt.Fprintf(&b, "      type: %s\n", p.hcType)
		if p.hcPath != "" {
			fmt.Fprintf(&b, "      path: %s\n", p.hcPath)
		}
		fmt.Fprintf(&b, "      interval: %s\n", p.hcInterval)
	}

	b.WriteString("\n")

	return b.String()
}
