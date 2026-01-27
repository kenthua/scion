package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/ptone/scion-agent/pkg/agent"
	"github.com/ptone/scion-agent/pkg/api"
	"github.com/ptone/scion-agent/pkg/hubclient"
	"github.com/ptone/scion-agent/pkg/runtime"
	"github.com/spf13/cobra"
)

// createCmd represents the create command
var createCmd = &cobra.Command{
	Use:   "create <agent-name>",
	Short: "Provision a new scion agent without starting it",
	Long: `Provision a new isolated LLM agent directory to perform a specific task.
The agent will be created from a template.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		agentName := args[0]

		// Check if Hub should be used
		hubCtx, err := CheckHubAvailability(grovePath)
		if err != nil {
			return err
		}

		if hubCtx != nil {
			return createAgentViaHub(hubCtx, agentName)
		}

		// Local mode
		effectiveProfile := profile
		if effectiveProfile == "" {
			effectiveProfile = agent.GetSavedProfile(agentName, grovePath)
		}

		rt := runtime.GetRuntime(grovePath, effectiveProfile)
		mgr := agent.NewManager(rt)

		opts := api.StartOptions{
			Name:      agentName,
			Template:  templateName,
			Profile:   effectiveProfile,
			Image:     agentImage,
			GrovePath: grovePath,
			Branch:    branch,
			Workspace: workspace,
		}

		// Check if container already exists

		agents, err := rt.List(context.Background(), nil)
		if err == nil {
			for _, a := range agents {
				if a.ID == agentName || a.Name == agentName {
					fmt.Printf("Agent container '%s' already exists (Status: %s).\n", agentName, a.Status)
					// We continue to check directory
				}
			}
		}

		_, err = mgr.Provision(context.Background(), opts)
		if err != nil {
			return err
		}

		fmt.Printf("Agent '%s' created successfully.\n", agentName)
		return nil
	},
}

func createAgentViaHub(hubCtx *HubContext, agentName string) error {
	PrintUsingHub(hubCtx.Endpoint)

	// Get the grove ID for this project
	groveID, err := GetGroveID(hubCtx)
	if err != nil {
		return wrapHubError(err)
	}

	// Build create request
	req := &hubclient.CreateAgentRequest{
		Name:     agentName,
		GroveID:  groveID,
		Template: templateName,
		Branch:   branch,
	}

	if agentImage != "" {
		req.Config = &hubclient.AgentConfig{
			Image: agentImage,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := hubCtx.Client.Agents().Create(ctx, req)
	if err != nil {
		return wrapHubError(fmt.Errorf("failed to create agent via Hub: %w", err))
	}

	fmt.Printf("Agent '%s' created via Hub.\n", agentName)
	if resp.Agent != nil {
		fmt.Printf("Agent ID: %s\n", resp.Agent.AgentID)
		fmt.Printf("Status: %s\n", resp.Agent.Status)
	}
	for _, w := range resp.Warnings {
		fmt.Printf("Warning: %s\n", w)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(createCmd)
	createCmd.Flags().StringVarP(&templateName, "type", "t", "", "Template to use")
	createCmd.Flags().StringVarP(&agentImage, "image", "i", "", "Container image to use (overrides template)")
	createCmd.Flags().StringVarP(&branch, "branch", "b", "", "Git branch to use for the agent workspace")
	createCmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Host path to mount as /workspace")
}
