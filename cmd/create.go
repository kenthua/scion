package cmd

import (
	"context"
	"fmt"

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

		// Check if container already exists
		rt := runtime.GetRuntime()
		agents, err := rt.List(context.Background(), nil)
		if err == nil {
			for _, a := range agents {
				if a.ID == agentName || a.Name == agentName {
					fmt.Printf("Agent container '%s' already exists (Status: %s).\n", agentName, a.Status)
					// We continue to check directory
				}
			}
		}

		_, _, _, _, err = GetAgent(agentName, templateName, agentImage, grovePath, "created")
		if err != nil {
			return err
		}

		fmt.Printf("Agent '%s' created successfully.\n", agentName)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(createCmd)
	createCmd.Flags().StringVarP(&templateName, "type", "t", "gemini", "Template to use")
	createCmd.Flags().StringVarP(&agentImage, "image", "i", "", "Container image to use (overrides template)")
}

