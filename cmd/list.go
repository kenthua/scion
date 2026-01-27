package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/ptone/scion-agent/pkg/agent"
	"github.com/ptone/scion-agent/pkg/api"
	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/hubclient"
	"github.com/ptone/scion-agent/pkg/runtime"
	"github.com/spf13/cobra"
)

var (
	listAll bool
)

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List running scion agents",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check if Hub should be used
		hubCtx, err := CheckHubAvailability(grovePath)
		if err != nil {
			return err
		}

		if hubCtx != nil {
			return listAgentsViaHub(hubCtx)
		}

		// Local mode
		return listAgentsLocal()
	},
}

// listAgentsLocal lists agents using the local runtime
func listAgentsLocal() error {
	rt := runtime.GetRuntime(grovePath, profile)
	mgr := agent.NewManager(rt)

	filters := map[string]string{
		"scion.agent": "true",
	}

	if listAll {
		// Cross-grove listing might need a way to find all groves.
		// For now, mgr.List handles current grove and what's provided in filters.
	} else {
		projectDir, _ := config.GetResolvedProjectDir(grovePath)
		if projectDir != "" {
			filters["scion.grove_path"] = projectDir
			filters["scion.grove"] = config.GetGroveName(projectDir)
		}
	}

	agents, err := mgr.List(context.Background(), filters)
	if err != nil {
		return err
	}

	return displayAgents(agents, listAll)
}

// listAgentsViaHub lists agents using the Hub API
func listAgentsViaHub(hubCtx *HubContext) error {
	PrintUsingHub(hubCtx.Endpoint)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	opts := &hubclient.ListAgentsOptions{}

	if !listAll {
		// Get the grove ID for the current project
		groveID, err := GetGroveID(hubCtx)
		if err != nil {
			return wrapHubError(err)
		}
		opts.GroveID = groveID
	}

	resp, err := hubCtx.Client.Agents().List(ctx, opts)
	if err != nil {
		return wrapHubError(fmt.Errorf("failed to list agents via Hub: %w", err))
	}

	// Convert Hub agents to local AgentInfo format
	agents := make([]api.AgentInfo, len(resp.Agents))
	for i, a := range resp.Agents {
		agents[i] = hubAgentToAgentInfo(a)
	}

	return displayAgents(agents, listAll)
}

// hubAgentToAgentInfo converts a Hub API Agent to a local AgentInfo
func hubAgentToAgentInfo(a hubclient.Agent) api.AgentInfo {
	info := api.AgentInfo{
		ID:              a.ID,
		AgentID:         a.AgentID,
		Name:            a.Name,
		Template:        a.Template,
		Grove:           a.Grove,
		GroveID:         a.GroveID,
		Labels:          a.Labels,
		Annotations:     a.Annotations,
		Status:          a.Status,
		ContainerStatus: a.ContainerStatus,
		SessionStatus:   a.SessionStatus,
		Image:           a.Image,
		Detached:        a.Detached,
		Runtime:         a.Runtime,
		RuntimeHostID:   a.RuntimeHostID,
		RuntimeHostType: a.RuntimeHostType,
		RuntimeState:    a.RuntimeState,
		WebPTYEnabled:   a.WebPTYEnabled,
		TaskSummary:     a.TaskSummary,
		Created:         a.Created,
		Updated:         a.Updated,
		LastSeen:        a.LastSeen,
		CreatedBy:       a.CreatedBy,
		OwnerID:         a.OwnerID,
		Visibility:      a.Visibility,
		StateVersion:    a.StateVersion,
	}

	// Convert Kubernetes info if present
	if a.Kubernetes != nil {
		info.Kubernetes = &api.AgentK8sMetadata{
			Cluster:   a.Kubernetes.Cluster,
			Namespace: a.Kubernetes.Namespace,
			PodName:   a.Kubernetes.PodName,
			SyncedAt:  a.Kubernetes.SyncedAt,
		}
	}

	return info
}

// displayAgents displays agents in the requested format
func displayAgents(agents []api.AgentInfo, all bool) error {
	if outputFormat == "json" {
		if agents == nil {
			agents = []api.AgentInfo{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(agents)
	}

	if len(agents) == 0 {
		if all {
			fmt.Println("No active agents found across any groves.")
		} else {
			fmt.Println("No active agents found in the current grove.")
		}
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTEMPLATE\tRUNTIME\tGROVE\tAGENT STATUS\tSESSION\tCONTAINER")
	for _, a := range agents {
		agentStatus := a.Status
		if agentStatus == "" {
			agentStatus = "IDLE"
		}
		sessionStatus := a.SessionStatus
		if sessionStatus == "" {
			sessionStatus = "-"
		}
		containerStatus := a.ContainerStatus
		if containerStatus == "created" && a.ID == "" {
			containerStatus = "none"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", a.Name, a.Template, a.Runtime, a.Grove, agentStatus, sessionStatus, containerStatus)
	}
	w.Flush()
	return nil
}

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.Flags().BoolVarP(&listAll, "all", "a", false, "List all agents across all groves")
}
