package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/ptone/scion-agent/pkg/config"
	"github.com/spf13/cobra"
)

// templatesCmd represents the templates command
var templatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Manage agent templates",
	Long:  `List and inspect templates used to provision new agents.`,
}

var templatesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available templates",
	RunE: func(cmd *cobra.Command, args []string) error {
		templates, err := config.ListTemplates()
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPATH")
		for _, t := range templates {
			fmt.Fprintf(w, "%s\t%s\n", t.Name, t.Path)
		}
		w.Flush()
		return nil
	},
}

var templatesShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show template configuration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		tpl, err := config.FindTemplate(name)
		if err != nil {
			return err
		}

		cfg, err := tpl.LoadConfig()
		if err != nil {
			return err
		}

		fmt.Printf("Template: %s\n", tpl.Name)
		fmt.Printf("Path:     %s\n", tpl.Path)
		fmt.Println("Configuration (scion.json):")
		
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(cfg)
	},
}

var templatesCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new template",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		global, _ := cmd.Flags().GetBool("global")
		provider, _ := cmd.Flags().GetString("provider")
		err := config.CreateTemplate(name, provider, global)
		if err != nil {
			return err
		}
		fmt.Printf("Template %s created successfully.\n", name)
		return nil
	},
}

var templatesDeleteCmd = &cobra.Command{
	Use:     "delete <name>",
	Aliases: []string{"rm"},
	Short:   "Delete a template",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		global, _ := cmd.Flags().GetBool("global")
		err := config.DeleteTemplate(name, global)
		if err != nil {
			return err
		}
		fmt.Printf("Template %s deleted successfully.\n", name)
		return nil
	},
}

var templatesUpdateDefaultCmd = &cobra.Command{
	Use:   "update-default",
	Short: "Update default templates with the latest from the binary",
	RunE: func(cmd *cobra.Command, args []string) error {
		global, _ := cmd.Flags().GetBool("global")
		err := config.UpdateDefaultTemplates(global)
		if err != nil {
			return err
		}
		fmt.Println("Default templates updated successfully.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(templatesCmd)
	templatesCmd.AddCommand(templatesListCmd)
	templatesCmd.AddCommand(templatesShowCmd)
	templatesCmd.AddCommand(templatesCreateCmd)
	templatesCmd.AddCommand(templatesDeleteCmd)
	templatesCmd.AddCommand(templatesUpdateDefaultCmd)

	templatesCreateCmd.Flags().StringP("provider", "p", "", "Harness provider (e.g. gemini, claude)")
}
