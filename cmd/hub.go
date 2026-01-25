package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/harness"
	"github.com/ptone/scion-agent/pkg/hubclient"
	"github.com/ptone/scion-agent/pkg/runtime"
	"github.com/ptone/scion-agent/pkg/util"
	"github.com/ptone/scion-agent/pkg/version"
	"github.com/spf13/cobra"
)

var (
	hubRegisterMode   string
	hubForceRegister  bool
	hubOutputJSON     bool
	hubDeregisterHost bool
)

// hubCmd represents the hub command
var hubCmd = &cobra.Command{
	Use:   "hub",
	Short: "Interact with the Scion Hub",
	Long: `Commands for interacting with a remote Scion Hub.

The Hub provides centralized coordination for groves, agents, and templates
across multiple runtime hosts.

Configure the Hub endpoint via:
  - SCION_HUB_ENDPOINT environment variable
  - hub.endpoint in settings.yaml
  - --hub flag on any command`,
}

// hubStatusCmd shows Hub connection status
var hubStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Hub connection status",
	Long:  `Show the current Hub connection status and configuration.`,
	RunE:  runHubStatus,
}

// hubRegisterCmd registers this host with the Hub
var hubRegisterCmd = &cobra.Command{
	Use:   "register [grove-path]",
	Short: "Register this host with the Hub",
	Long: `Register this host as a runtime contributor for a grove.

If grove-path is not specified, uses the current project grove or global grove.
The host is identified by its hostname to prevent duplicate registrations.

This command will:
1. Create or update the grove in the Hub (matched by git remote or name)
2. Register this host as a contributor to the grove (using hostname as identifier)
3. Save the returned host token for future authentication

Examples:
  # Register the current project grove
  scion hub register

  # Register the global grove
  scion hub register --global`,
	RunE: runHubRegister,
}

// hubDeregisterCmd removes this host from the Hub
var hubDeregisterCmd = &cobra.Command{
	Use:   "deregister",
	Short: "Remove this host from the Hub",
	Long: `Remove this host from the Hub.

This command will:
1. Remove this host from all groves it contributes to
2. Clear the stored host token

Use --host-only to only remove the host record without affecting grove contributions.`,
	RunE: runHubDeregister,
}

// hubGrovesCmd lists groves on the Hub
var hubGrovesCmd = &cobra.Command{
	Use:   "groves",
	Short: "List groves on the Hub",
	Long:  `List groves registered on the Hub that you have access to.`,
	RunE:  runHubGroves,
}

// hubHostsCmd lists runtime hosts on the Hub
var hubHostsCmd = &cobra.Command{
	Use:   "hosts",
	Short: "List runtime hosts on the Hub",
	Long:  `List runtime hosts registered on the Hub.`,
	RunE:  runHubHosts,
}

func init() {
	rootCmd.AddCommand(hubCmd)
	hubCmd.AddCommand(hubStatusCmd)
	hubCmd.AddCommand(hubRegisterCmd)
	hubCmd.AddCommand(hubDeregisterCmd)
	hubCmd.AddCommand(hubGrovesCmd)
	hubCmd.AddCommand(hubHostsCmd)

	// Register flags
	hubRegisterCmd.Flags().StringVar(&hubRegisterMode, "mode", "connected", "Registration mode (connected, read-only)")
	hubRegisterCmd.Flags().BoolVar(&hubForceRegister, "force", false, "Force re-registration even if already registered")

	// Deregister flags
	hubDeregisterCmd.Flags().BoolVar(&hubDeregisterHost, "host-only", false, "Only remove host record, not grove contributions")

	// Common flags
	hubStatusCmd.Flags().BoolVar(&hubOutputJSON, "json", false, "Output in JSON format")
	hubGrovesCmd.Flags().BoolVar(&hubOutputJSON, "json", false, "Output in JSON format")
	hubHostsCmd.Flags().BoolVar(&hubOutputJSON, "json", false, "Output in JSON format")
}

func getHubClient(settings *config.Settings) (hubclient.Client, error) {
	endpoint := GetHubEndpoint(settings)
	if endpoint == "" {
		return nil, fmt.Errorf("Hub endpoint not configured. Set SCION_HUB_ENDPOINT or use --hub flag")
	}

	var opts []hubclient.Option

	// Add authentication
	if settings.Hub != nil {
		if settings.Hub.Token != "" {
			opts = append(opts, hubclient.WithBearerToken(settings.Hub.Token))
		} else if settings.Hub.APIKey != "" {
			opts = append(opts, hubclient.WithAPIKey(settings.Hub.APIKey))
		} else if settings.Hub.HostToken != "" {
			// Use host token for authentication if available
			opts = append(opts, hubclient.WithBearerToken(settings.Hub.HostToken))
		}
	}

	opts = append(opts, hubclient.WithTimeout(30*time.Second))

	return hubclient.New(endpoint, opts...)
}

func runHubStatus(cmd *cobra.Command, args []string) error {
	settings, err := config.LoadSettings(grovePath)
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	endpoint := GetHubEndpoint(settings)

	if hubOutputJSON {
		status := map[string]interface{}{
			"enabled":    !noHub,
			"endpoint":   endpoint,
			"configured": settings.IsHubConfigured(),
		}
		if settings.Hub != nil {
			status["hostId"] = settings.Hub.HostID
			status["hasToken"] = settings.Hub.Token != ""
			status["hasApiKey"] = settings.Hub.APIKey != ""
			status["hasHostToken"] = settings.Hub.HostToken != ""
		}

		// Try to connect and get health
		if endpoint != "" && !noHub {
			client, err := getHubClient(settings)
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if health, err := client.Health(ctx); err == nil {
					status["connected"] = true
					status["hubVersion"] = health.Version
					status["hubStatus"] = health.Status
				} else {
					status["connected"] = false
					status["error"] = err.Error()
				}
			}
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}

	// Text output
	fmt.Println("Hub Integration Status")
	fmt.Println("======================")
	fmt.Printf("Enabled:    %v\n", !noHub)
	fmt.Printf("Endpoint:   %s\n", valueOrNone(endpoint))
	fmt.Printf("Configured: %v\n", settings.IsHubConfigured())

	if settings.Hub != nil {
		fmt.Printf("Host ID:    %s\n", valueOrNone(settings.Hub.HostID))
		fmt.Printf("Has Token:  %v\n", settings.Hub.Token != "")
		fmt.Printf("Has API Key: %v\n", settings.Hub.APIKey != "")
		fmt.Printf("Has Host Token: %v\n", settings.Hub.HostToken != "")
	}

	// Try to connect
	if endpoint != "" && !noHub {
		client, err := getHubClient(settings)
		if err != nil {
			fmt.Printf("\nConnection: failed (%s)\n", err)
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		health, err := client.Health(ctx)
		if err != nil {
			fmt.Printf("\nConnection: failed (%s)\n", err)
		} else {
			fmt.Printf("\nConnection: ok\n")
			fmt.Printf("Hub Version: %s\n", health.Version)
			fmt.Printf("Hub Status:  %s\n", health.Status)
		}
	}

	return nil
}

func runHubRegister(cmd *cobra.Command, args []string) error {
	// Determine grove path
	gp := grovePath
	if len(args) > 0 {
		gp = args[0]
	}
	if gp == "" && globalMode {
		gp = "global"
	}

	// Resolve grove path
	resolvedPath, isGlobal, err := config.ResolveGrovePath(gp)
	if err != nil {
		return fmt.Errorf("failed to resolve grove path: %w", err)
	}

	settings, err := config.LoadSettings(resolvedPath)
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	client, err := getHubClient(settings)
	if err != nil {
		return err
	}

	// Get grove info
	var groveName string
	var gitRemote string

	if isGlobal {
		groveName = "global"
	} else {
		// Get git remote
		gitRemote = util.GetGitRemote()
		if gitRemote == "" {
			return fmt.Errorf("could not determine git remote for this project")
		}
		// Get project name from git remote
		groveName = util.ExtractRepoName(gitRemote)
	}

	// Get hostname (always use system hostname to prevent duplicates)
	hostName, err := os.Hostname()
	if err != nil {
		hostName = "local-host"
	}

	// Detect runtime
	rt := runtime.GetRuntime("", "")
	runtimeType := "docker"
	if rt != nil {
		runtimeType = rt.Name()
	}

	// Get supported harnesses
	allHarnesses := harness.All()
	supportedHarnesses := make([]string, 0, len(allHarnesses))
	for _, h := range allHarnesses {
		supportedHarnesses = append(supportedHarnesses, h.Name())
	}

	// Get existing host ID if available
	var existingHostID string
	if settings.Hub != nil {
		existingHostID = settings.Hub.HostID
	}

	// Build registration request
	req := &hubclient.RegisterGroveRequest{
		Name:      groveName,
		GitRemote: gitRemote,
		Path:      resolvedPath,
		Mode:      hubRegisterMode,
		Host: &hubclient.HostInfo{
			ID:      existingHostID, // May be empty
			Name:    hostName,
			Version: version.Version,
			Capabilities: &hubclient.HostCapabilities{
				WebPTY: false, // Not implemented yet
				Sync:   true,
				Attach: true,
			},
			Runtimes: []hubclient.HostRuntime{
				{Type: runtimeType, Available: true},
			},
			SupportedHarnesses: supportedHarnesses,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Groves().Register(ctx, req)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	// Save the host token
	if resp.HostToken != "" {
		if settings.Hub == nil {
			settings.Hub = &config.HubClientConfig{}
		}
		settings.Hub.HostToken = resp.HostToken
		settings.Hub.HostID = resp.Host.ID

		if err := config.UpdateSetting(resolvedPath, "hub.hostToken", resp.HostToken, isGlobal); err != nil {
			fmt.Printf("Warning: failed to save host token: %v\n", err)
		}
		if err := config.UpdateSetting(resolvedPath, "hub.hostId", resp.Host.ID, isGlobal); err != nil {
			fmt.Printf("Warning: failed to save host ID: %v\n", err)
		}
	}

	if resp.Created {
		fmt.Printf("Created new grove: %s (ID: %s)\n", resp.Grove.Name, resp.Grove.ID)
	} else {
		fmt.Printf("Linked to existing grove: %s (ID: %s)\n", resp.Grove.Name, resp.Grove.ID)
	}
	fmt.Printf("Host registered: %s (ID: %s)\n", resp.Host.Name, resp.Host.ID)

	return nil
}

func runHubDeregister(cmd *cobra.Command, args []string) error {
	settings, err := config.LoadSettings(grovePath)
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	if settings.Hub == nil || settings.Hub.HostID == "" {
		return fmt.Errorf("no host registration found")
	}

	client, err := getHubClient(settings)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hostID := settings.Hub.HostID

	if err := client.RuntimeHosts().Delete(ctx, hostID); err != nil {
		return fmt.Errorf("deregistration failed: %w", err)
	}

	// Clear the stored credentials
	resolvedPath, isGlobal, err := config.ResolveGrovePath(grovePath)
	if err == nil {
		_ = config.UpdateSetting(resolvedPath, "hub.hostToken", "", isGlobal)
		_ = config.UpdateSetting(resolvedPath, "hub.hostId", "", isGlobal)
	}

	fmt.Printf("Host %s deregistered from Hub\n", hostID)
	return nil
}

func runHubGroves(cmd *cobra.Command, args []string) error {
	settings, err := config.LoadSettings(grovePath)
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	client, err := getHubClient(settings)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Groves().List(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to list groves: %w", err)
	}

	if hubOutputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp.Groves)
	}

	if len(resp.Groves) == 0 {
		fmt.Println("No groves found")
		return nil
	}

	fmt.Printf("%-36s  %-20s  %-10s  %s\n", "ID", "NAME", "AGENTS", "GIT REMOTE")
	fmt.Printf("%-36s  %-20s  %-10s  %s\n", "------------------------------------", "--------------------", "----------", "----------")
	for _, g := range resp.Groves {
		gitRemote := g.GitRemote
		if len(gitRemote) > 40 {
			gitRemote = gitRemote[:37] + "..."
		}
		fmt.Printf("%-36s  %-20s  %-10d  %s\n", g.ID, truncate(g.Name, 20), g.AgentCount, gitRemote)
	}

	return nil
}

func runHubHosts(cmd *cobra.Command, args []string) error {
	settings, err := config.LoadSettings(grovePath)
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	client, err := getHubClient(settings)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.RuntimeHosts().List(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to list hosts: %w", err)
	}

	if hubOutputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp.Hosts)
	}

	if len(resp.Hosts) == 0 {
		fmt.Println("No runtime hosts found")
		return nil
	}

	fmt.Printf("%-36s  %-20s  %-10s  %-10s  %s\n", "ID", "NAME", "TYPE", "STATUS", "MODE")
	fmt.Printf("%-36s  %-20s  %-10s  %-10s  %s\n", "------------------------------------", "--------------------", "----------", "----------", "----------")
	for _, h := range resp.Hosts {
		fmt.Printf("%-36s  %-20s  %-10s  %-10s  %s\n", h.ID, truncate(h.Name, 20), h.Type, h.Status, h.Mode)
	}

	return nil
}

func valueOrNone(s string) string {
	if s == "" {
		return "(not configured)"
	}
	return s
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
