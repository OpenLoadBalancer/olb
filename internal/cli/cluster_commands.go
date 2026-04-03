// Package cli provides command-line interface commands for OpenLoadBalancer.
// This file implements cluster management CLI commands for joining, leaving,
// and inspecting the cluster state.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
)

// clusterStatusResponse represents the status returned by the cluster status API.
type clusterStatusResponse struct {
	NodeID    string              `json:"node_id"`
	State     string              `json:"state"`
	RaftState string              `json:"raft_state"`
	Leader    string              `json:"leader"`
	Term      uint64              `json:"term"`
	Members   []clusterMemberInfo `json:"members"`
	Healthy   bool                `json:"healthy"`
	Uptime    string              `json:"uptime"`
}

// clusterMemberInfo represents a cluster member in API responses.
type clusterMemberInfo struct {
	ID        string `json:"id"`
	Address   string `json:"address"`
	RaftState string `json:"raft_state"`
	IsLeader  bool   `json:"is_leader"`
	Healthy   bool   `json:"healthy"`
}

// ClusterStatusCommand shows the current cluster status.
type ClusterStatusCommand struct {
	apiAddr string
	format  string
}

// Name returns the command name.
func (c *ClusterStatusCommand) Name() string {
	return "cluster-status"
}

// Description returns the command description.
func (c *ClusterStatusCommand) Description() string {
	return "Show cluster status"
}

// Run executes the cluster-status command.
func (c *ClusterStatusCommand) Run(args []string) error {
	fs := flag.NewFlagSet("cluster-status", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "table", "Output format (json or table)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	var status clusterStatusResponse
	if err := client.get("/cluster/status", &status); err != nil {
		return fmt.Errorf("failed to get cluster status: %w", err)
	}

	switch c.format {
	case "json":
		data, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "table":
		fmt.Println("Cluster Status")
		fmt.Println("==============")
		fmt.Printf("Node ID:    %s\n", status.NodeID)
		fmt.Printf("State:      %s\n", status.State)
		fmt.Printf("Raft State: %s\n", status.RaftState)
		fmt.Printf("Leader:     %s\n", status.Leader)
		fmt.Printf("Term:       %d\n", status.Term)
		healthy := "no"
		if status.Healthy {
			healthy = "yes"
		}
		fmt.Printf("Healthy:    %s\n", healthy)
		fmt.Printf("Uptime:     %s\n", status.Uptime)
		fmt.Printf("Members:    %d\n", len(status.Members))
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}

// ClusterJoinCommand joins the node to a cluster.
type ClusterJoinCommand struct {
	apiAddr string
	addr    string
}

// Name returns the command name.
func (c *ClusterJoinCommand) Name() string {
	return "cluster-join"
}

// Description returns the command description.
func (c *ClusterJoinCommand) Description() string {
	return "Join a cluster"
}

// Run executes the cluster-join command.
func (c *ClusterJoinCommand) Run(args []string) error {
	fs := flag.NewFlagSet("cluster-join", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.addr, "addr", "", "Seed address to join (required)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if c.addr == "" {
		return fmt.Errorf("--addr flag is required (seed address to join)")
	}

	// Support comma-separated addresses
	seedAddrs := strings.Split(c.addr, ",")
	for i := range seedAddrs {
		seedAddrs[i] = strings.TrimSpace(seedAddrs[i])
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	body := map[string]any{
		"seed_addrs": seedAddrs,
	}

	var result map[string]string
	if err := client.post("/cluster/join", body, &result); err != nil {
		return fmt.Errorf("failed to join cluster: %w", err)
	}

	fmt.Printf("Successfully joined cluster via %s\n", c.addr)
	if state, ok := result["state"]; ok {
		fmt.Printf("Current state: %s\n", state)
	}

	return nil
}

// ClusterLeaveCommand leaves the cluster.
type ClusterLeaveCommand struct {
	apiAddr string
}

// Name returns the command name.
func (c *ClusterLeaveCommand) Name() string {
	return "cluster-leave"
}

// Description returns the command description.
func (c *ClusterLeaveCommand) Description() string {
	return "Leave the cluster"
}

// Run executes the cluster-leave command.
func (c *ClusterLeaveCommand) Run(args []string) error {
	fs := flag.NewFlagSet("cluster-leave", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")

	if err := fs.Parse(args); err != nil {
		return err
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	var result map[string]string
	if err := client.post("/cluster/leave", nil, &result); err != nil {
		return fmt.Errorf("failed to leave cluster: %w", err)
	}

	fmt.Println("Successfully left the cluster")
	if state, ok := result["state"]; ok {
		fmt.Printf("Current state: %s\n", state)
	}

	return nil
}

// ClusterMembersCommand lists cluster members.
type ClusterMembersCommand struct {
	apiAddr string
	format  string
}

// Name returns the command name.
func (c *ClusterMembersCommand) Name() string {
	return "cluster-members"
}

// Description returns the command description.
func (c *ClusterMembersCommand) Description() string {
	return "List cluster members"
}

// Run executes the cluster-members command.
func (c *ClusterMembersCommand) Run(args []string) error {
	fs := flag.NewFlagSet("cluster-members", flag.ExitOnError)
	fs.StringVar(&c.apiAddr, "api-addr", "localhost:8081", "Admin API address")
	fs.StringVar(&c.format, "format", "table", "Output format (json or table)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	client := NewClient(fmt.Sprintf("http://%s", c.apiAddr))

	var members []clusterMemberInfo
	if err := client.get("/cluster/members", &members); err != nil {
		return fmt.Errorf("failed to get cluster members: %w", err)
	}

	switch c.format {
	case "json":
		data, err := json.MarshalIndent(members, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "table":
		fmt.Println("Cluster Members")
		fmt.Println("===============")
		fmt.Printf("%-20s %-30s %-12s %-10s %-10s\n", "ID", "Address", "Raft State", "Leader", "Healthy")
		fmt.Println(strings.Repeat("-", 82))
		for _, m := range members {
			leader := "no"
			if m.IsLeader {
				leader = "yes"
			}
			healthy := "no"
			if m.Healthy {
				healthy = "yes"
			}
			fmt.Printf("%-20s %-30s %-12s %-10s %-10s\n",
				m.ID, m.Address, m.RaftState, leader, healthy)
		}
		fmt.Printf("\nTotal: %d members\n", len(members))
	default:
		return fmt.Errorf("unknown format: %s", c.format)
	}

	return nil
}
