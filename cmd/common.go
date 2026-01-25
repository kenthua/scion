package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ptone/scion-agent/pkg/agent"
	"github.com/ptone/scion-agent/pkg/api"
	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/hubclient"
	"github.com/ptone/scion-agent/pkg/util"
	"github.com/spf13/cobra"
)

var (
	templateName string
	agentImage   string
	noAuth       bool
	attach       bool
	branch       string
	workspace    string
)

// HubContext holds the context for Hub operations.
type HubContext struct {
	Client   hubclient.Client
	Endpoint string
	Settings *config.Settings
}

// CheckHubAvailability checks if Hub integration is enabled and returns a ready-to-use
// Hub context if available. Returns nil if Hub should not be used (not enabled, not configured,
// or --no-hub flag is set).
// If Hub is enabled but unhealthy, returns an error with guidance to disable.
func CheckHubAvailability(grovePath string) (*HubContext, error) {
	// Check if --no-hub flag is set
	if noHub {
		return nil, nil
	}

	settings, err := config.LoadSettings(grovePath)
	if err != nil {
		// If we can't load settings, fall back to local mode
		return nil, nil
	}

	// Check if hub is explicitly enabled
	if !settings.IsHubEnabled() {
		return nil, nil
	}

	endpoint := GetHubEndpoint(settings)
	if endpoint == "" {
		return nil, nil
	}

	// Hub is enabled and configured, try to connect
	client, err := getHubClient(settings)
	if err != nil {
		return nil, wrapHubError(fmt.Errorf("failed to create Hub client: %w", err))
	}

	// Check health
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.Health(ctx); err != nil {
		return nil, wrapHubError(fmt.Errorf("Hub at %s is not responding: %w", endpoint, err))
	}

	return &HubContext{
		Client:   client,
		Endpoint: endpoint,
		Settings: settings,
	}, nil
}

// PrintUsingHub prints the informational message about using the Hub.
func PrintUsingHub(endpoint string) {
	fmt.Printf("Using hub: %s\n", endpoint)
}

// wrapHubError wraps a Hub error with guidance to disable Hub integration.
func wrapHubError(err error) error {
	return fmt.Errorf("%w\n\nTo use local-only mode, run: scion hub disable", err)
}

// GetGroveIDFromGitRemote looks up the grove ID from the Hub based on the git remote.
// Returns the grove ID if found, or an error if the grove is not registered.
func GetGroveIDFromGitRemote(hubCtx *HubContext) (string, error) {
	gitRemote := util.GetGitRemote()
	if gitRemote == "" {
		return "", fmt.Errorf("could not determine git remote for this project.\n\nRun 'scion hub register' to register this grove with the Hub, or use --no-hub for local-only mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Look up groves by git remote
	resp, err := hubCtx.Client.Groves().List(ctx, &hubclient.ListGrovesOptions{
		GitRemote: gitRemote,
	})
	if err != nil {
		return "", fmt.Errorf("failed to look up grove by git remote: %w", err)
	}

	if len(resp.Groves) == 0 {
		return "", fmt.Errorf("no grove found for git remote: %s\n\nRun 'scion hub register' to register this grove with the Hub", gitRemote)
	}

	// Return the first matching grove
	return resp.Groves[0].ID, nil
}

func RunAgent(cmd *cobra.Command, args []string, resume bool) error {
	agentName := args[0]
	task := strings.Join(args[1:], " ")

	// Check if Hub should be used
	hubCtx, err := CheckHubAvailability(grovePath)
	if err != nil {
		return err
	}

	if hubCtx != nil {
		return startAgentViaHub(hubCtx, agentName, task, resume)
	}

	// Local mode
	effectiveProfile := profile
	if effectiveProfile == "" {
		// If no profile flag, check if we have a saved profile for this agent
		effectiveProfile = agent.GetSavedProfile(agentName, grovePath)
	}

	rt := agent.ResolveRuntime(grovePath, agentName, profile)
	mgr := agent.NewManager(rt)

	// Check if already running and we want to attach
	if attach {
		agents, err := rt.List(context.Background(), map[string]string{"scion.name": agentName})
		if err == nil {
			for _, a := range agents {
				if a.Name == agentName || a.ID == agentName || strings.TrimPrefix(a.Name, "/") == agentName {
					status := strings.ToLower(a.ContainerStatus)
					isRunning := strings.HasPrefix(status, "up") || status == "running"
					if isRunning {
						fmt.Printf("Agent '%s' is already running. Attaching...\n", agentName)
						return rt.Attach(context.Background(), a.ID)
					}
				}
			}
		}
	}

	// Flag takes ultimate precedence
	resolvedImage := agentImage

	var detached *bool
	if attach {
		val := false
		detached = &val
	}

	opts := api.StartOptions{
		Name:      agentName,
		Task:      strings.TrimSpace(task),
		Template:  templateName,
		Profile:   effectiveProfile,
		Image:     resolvedImage,
		GrovePath: grovePath,
		Resume:    resume,
		Detached:  detached,
		NoAuth:    noAuth,
		Branch:    branch,
		Workspace: workspace,
	}

	// We still might want to show some progress in the CLI
	if resume {
		fmt.Printf("Resuming agent '%s'...\n", agentName)
	} else {
		fmt.Printf("Starting agent '%s'...\n", agentName)
	}

	info, err := mgr.Start(context.Background(), opts)
	if err != nil {
		return err
	}

	for _, w := range info.Warnings {
		fmt.Fprintln(os.Stderr, w)
	}

	if !info.Detached {
		fmt.Printf("Attaching to agent '%s'...\n", agentName)
		return rt.Attach(context.Background(), info.ID)
	}

	displayStatus := "launched"
	if resume {
		displayStatus = "resumed"
	}
	fmt.Printf("Agent '%s' %s successfully (ID: %s)\n", agentName, displayStatus, info.ID)

	return nil
}

func startAgentViaHub(hubCtx *HubContext, agentName, task string, resume bool) error {
	PrintUsingHub(hubCtx.Endpoint)

	// If attach is requested, we can't do that via Hub yet
	if attach {
		return fmt.Errorf("attach mode is not yet supported when using Hub integration\n\nTo attach locally, use: scion --no-hub start -a %s", agentName)
	}

	// Get the grove ID from the git remote
	groveID, err := GetGroveIDFromGitRemote(hubCtx)
	if err != nil {
		return wrapHubError(err)
	}

	// Build create request (Hub creates and starts in one operation)
	req := &hubclient.CreateAgentRequest{
		Name:     agentName,
		GroveID:  groveID,
		Template: templateName,
		Task:     task,
		Branch:   branch,
		Resume:   resume,
	}

	if agentImage != "" {
		req.Config = &hubclient.AgentConfig{
			Image: agentImage,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	action := "Starting"
	if resume {
		action = "Resuming"
	}
	fmt.Printf("%s agent '%s'...\n", action, agentName)

	resp, err := hubCtx.Client.Agents().Create(ctx, req)
	if err != nil {
		return wrapHubError(fmt.Errorf("failed to start agent via Hub: %w", err))
	}

	displayStatus := "started"
	if resume {
		displayStatus = "resumed"
	}
	fmt.Printf("Agent '%s' %s via Hub.\n", agentName, displayStatus)
	if resp.Agent != nil {
		fmt.Printf("Agent ID: %s\n", resp.Agent.AgentID)
		fmt.Printf("Status: %s\n", resp.Agent.Status)
	}
	for _, w := range resp.Warnings {
		fmt.Printf("Warning: %s\n", w)
	}

	return nil
}
