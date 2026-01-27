package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/harness"
	"github.com/ptone/scion-agent/pkg/hubclient"
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
		fmt.Println("Configuration (scion-agent.json):")

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
		harnessName, _ := cmd.Flags().GetString("harness")
		if harnessName == "" {
			harnessName = "gemini"
		}

		h := harness.New(harnessName)

		err := config.CreateTemplate(name, h, global)
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

var templatesCloneCmd = &cobra.Command{
	Use:   "clone <src-name> <dest-name>",
	Short: "Clone an existing template",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		srcName := args[0]
		destName := args[1]
		global, _ := cmd.Flags().GetBool("global")
		err := config.CloneTemplate(srcName, destName, global)
		if err != nil {
			return err
		}
		fmt.Printf("Template %s cloned to %s successfully.\n", srcName, destName)
		return nil
	},
}

var templatesUpdateDefaultCmd = &cobra.Command{
	Use:   "update-default",
	Short: "Update default templates with the latest from the binary",
	RunE: func(cmd *cobra.Command, args []string) error {
		global, _ := cmd.Flags().GetBool("global")
		harnesses := harness.All()
		err := config.UpdateDefaultTemplates(global, harnesses)
		if err != nil {
			return err
		}
		fmt.Println("Default templates updated successfully.")
		return nil
	},
}

// templatesSyncCmd creates or updates a template in the Hub (upsert).
var templatesSyncCmd = &cobra.Command{
	Use:   "sync <name>",
	Short: "Create or update a template in the Hub (Hub only)",
	Long: `Sync a local template to the Hub. Creates the template if it doesn't exist,
or updates it if it does. This is an upsert operation.

Examples:
  # Sync a template from local .scion/templates/custom
  scion template sync custom-claude --from .scion/templates/custom --harness claude

  # Sync with explicit scope
  scion template sync my-template --from ./my-template --scope grove --harness claude`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		fromPath, _ := cmd.Flags().GetString("from")
		scopeFlag, _ := cmd.Flags().GetString("scope")
		harnessFlag, _ := cmd.Flags().GetString("harness")

		// Validate required flags
		if fromPath == "" {
			return fmt.Errorf("--from flag is required")
		}
		if harnessFlag == "" {
			return fmt.Errorf("--harness flag is required")
		}

		// Resolve path
		absPath, err := filepath.Abs(fromPath)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}

		// Verify directory exists
		info, err := os.Stat(absPath)
		if err != nil {
			return fmt.Errorf("template path not found: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("template path must be a directory: %s", absPath)
		}

		// Check Hub availability
		hubCtx, err := CheckHubAvailability(grovePath)
		if err != nil {
			return err
		}
		if hubCtx == nil {
			return fmt.Errorf("Hub integration is not enabled. Use 'scion hub enable' first")
		}

		PrintUsingHub(hubCtx.Endpoint)

		return syncTemplateToHub(hubCtx, name, absPath, scopeFlag, harnessFlag)
	},
}

// templatesPushCmd uploads local template files to an existing Hub template.
var templatesPushCmd = &cobra.Command{
	Use:   "push <name>",
	Short: "Upload local template files to Hub (Hub only)",
	Long: `Push local template changes to an existing template in the Hub.

Examples:
  # Push local template to Hub
  scion template push custom-claude

  # Push from a specific path
  scion template push custom-claude --from .scion/templates/custom`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		fromPath, _ := cmd.Flags().GetString("from")

		// Check Hub availability
		hubCtx, err := CheckHubAvailability(grovePath)
		if err != nil {
			return err
		}
		if hubCtx == nil {
			return fmt.Errorf("Hub integration is not enabled. Use 'scion hub enable' first")
		}

		PrintUsingHub(hubCtx.Endpoint)

		return pushTemplateToHub(hubCtx, name, fromPath)
	},
}

// templatesPullCmd downloads a template from the Hub.
var templatesPullCmd = &cobra.Command{
	Use:   "pull <name>",
	Short: "Download a template from Hub to local cache (Hub only)",
	Long: `Pull a template from the Hub to the local filesystem.

Examples:
  # Pull a template from Hub
  scion template pull custom-claude

  # Pull to a specific location
  scion template pull custom-claude --to .scion/templates/custom`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		toPath, _ := cmd.Flags().GetString("to")

		// Check Hub availability
		hubCtx, err := CheckHubAvailability(grovePath)
		if err != nil {
			return err
		}
		if hubCtx == nil {
			return fmt.Errorf("Hub integration is not enabled. Use 'scion hub enable' first")
		}

		PrintUsingHub(hubCtx.Endpoint)

		return pullTemplateFromHub(hubCtx, name, toPath)
	},
}

// syncTemplateToHub creates or updates a template in the Hub.
func syncTemplateToHub(hubCtx *HubContext, name, localPath, scope, harnessType string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Default scope
	if scope == "" {
		scope = "grove"
	}

	// Collect local files
	fmt.Printf("Scanning template files in %s...\n", localPath)
	files, err := hubclient.CollectFiles(localPath, nil)
	if err != nil {
		return fmt.Errorf("failed to scan template files: %w", err)
	}
	fmt.Printf("Found %d files\n", len(files))

	// Build file upload request
	fileReqs := make([]hubclient.FileUploadRequest, len(files))
	for i, f := range files {
		fileReqs[i] = hubclient.FileUploadRequest{
			Path: f.Path,
			Size: f.Size,
		}
	}

	// Get grove ID for grove scope
	var groveID string
	if scope == "grove" {
		groveID, err = GetGroveID(hubCtx)
		if err != nil {
			return err
		}
	}

	// Create template with file upload URLs
	fmt.Printf("Creating/updating template '%s' in Hub...\n", name)
	createReq := &hubclient.CreateTemplateRequest{
		Name:    name,
		Harness: harnessType,
		Scope:   scope,
		GroveID: groveID,
	}

	resp, err := hubCtx.Client.Templates().Create(ctx, createReq)
	if err != nil {
		return fmt.Errorf("failed to create template: %w", err)
	}

	templateID := resp.Template.ID
	fmt.Printf("Template created with ID: %s\n", templateID)

	// Request upload URLs
	fmt.Println("Requesting upload URLs...")
	uploadResp, err := hubCtx.Client.Templates().RequestUploadURLs(ctx, templateID, fileReqs)
	if err != nil {
		return fmt.Errorf("failed to get upload URLs: %w", err)
	}

	// Upload files
	fmt.Printf("Uploading %d files...\n", len(uploadResp.UploadURLs))
	for _, urlInfo := range uploadResp.UploadURLs {
		// Find matching file
		var fileInfo *hubclient.FileInfo
		for i := range files {
			if files[i].Path == urlInfo.Path {
				fileInfo = &files[i]
				break
			}
		}
		if fileInfo == nil {
			fmt.Printf("  Warning: no matching file for %s\n", urlInfo.Path)
			continue
		}

		// Open and upload file
		f, err := os.Open(fileInfo.FullPath)
		if err != nil {
			return fmt.Errorf("failed to open %s: %w", fileInfo.Path, err)
		}

		err = hubCtx.Client.Templates().UploadFile(ctx, urlInfo.URL, urlInfo.Method, urlInfo.Headers, f)
		f.Close()
		if err != nil {
			return fmt.Errorf("failed to upload %s: %w", fileInfo.Path, err)
		}
		fmt.Printf("  Uploaded: %s\n", fileInfo.Path)
	}

	// Build manifest
	manifest := &hubclient.TemplateManifest{
		Version: "1.0",
		Harness: harnessType,
		Files:   make([]hubclient.TemplateFile, len(files)),
	}
	for i, f := range files {
		manifest.Files[i] = hubclient.TemplateFile{
			Path: f.Path,
			Size: f.Size,
			Hash: f.Hash,
			Mode: f.Mode,
		}
	}

	// Finalize template
	fmt.Println("Finalizing template...")
	template, err := hubCtx.Client.Templates().Finalize(ctx, templateID, manifest)
	if err != nil {
		return fmt.Errorf("failed to finalize template: %w", err)
	}

	fmt.Printf("Template '%s' synced successfully!\n", name)
	fmt.Printf("  ID: %s\n", template.ID)
	fmt.Printf("  Status: %s\n", template.Status)
	fmt.Printf("  Content Hash: %s\n", template.ContentHash)

	return nil
}

// pushTemplateToHub uploads files to an existing template.
func pushTemplateToHub(hubCtx *HubContext, name, fromPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// If no path provided, try to find the template locally
	localPath := fromPath
	if localPath == "" {
		tpl, err := config.FindTemplate(name)
		if err != nil {
			return fmt.Errorf("template '%s' not found locally. Use --from to specify the path", name)
		}
		localPath = tpl.Path
	} else {
		var err error
		localPath, err = filepath.Abs(fromPath)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}
	}

	// Verify directory exists
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("template path not found: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("template path must be a directory: %s", localPath)
	}

	// Find the template in Hub
	fmt.Printf("Looking up template '%s' in Hub...\n", name)

	// List templates to find by name
	listResp, err := hubCtx.Client.Templates().List(ctx, &hubclient.ListTemplatesOptions{})
	if err != nil {
		return fmt.Errorf("failed to list templates: %w", err)
	}

	var template *hubclient.Template
	for i := range listResp.Templates {
		if listResp.Templates[i].Name == name || listResp.Templates[i].Slug == name {
			template = &listResp.Templates[i]
			break
		}
	}

	if template == nil {
		return fmt.Errorf("template '%s' not found in Hub. Use 'template sync' to create it first", name)
	}

	// Collect local files
	fmt.Printf("Scanning template files in %s...\n", localPath)
	files, err := hubclient.CollectFiles(localPath, nil)
	if err != nil {
		return fmt.Errorf("failed to scan template files: %w", err)
	}
	fmt.Printf("Found %d files\n", len(files))

	// Build file upload request
	fileReqs := make([]hubclient.FileUploadRequest, len(files))
	for i, f := range files {
		fileReqs[i] = hubclient.FileUploadRequest{
			Path: f.Path,
			Size: f.Size,
		}
	}

	// Request upload URLs
	fmt.Println("Requesting upload URLs...")
	uploadResp, err := hubCtx.Client.Templates().RequestUploadURLs(ctx, template.ID, fileReqs)
	if err != nil {
		return fmt.Errorf("failed to get upload URLs: %w", err)
	}

	// Upload files
	fmt.Printf("Uploading %d files...\n", len(uploadResp.UploadURLs))
	for _, urlInfo := range uploadResp.UploadURLs {
		var fileInfo *hubclient.FileInfo
		for i := range files {
			if files[i].Path == urlInfo.Path {
				fileInfo = &files[i]
				break
			}
		}
		if fileInfo == nil {
			continue
		}

		f, err := os.Open(fileInfo.FullPath)
		if err != nil {
			return fmt.Errorf("failed to open %s: %w", fileInfo.Path, err)
		}

		err = hubCtx.Client.Templates().UploadFile(ctx, urlInfo.URL, urlInfo.Method, urlInfo.Headers, f)
		f.Close()
		if err != nil {
			return fmt.Errorf("failed to upload %s: %w", fileInfo.Path, err)
		}
		fmt.Printf("  Uploaded: %s\n", fileInfo.Path)
	}

	// Build manifest
	manifest := &hubclient.TemplateManifest{
		Version: "1.0",
		Harness: template.Harness,
		Files:   make([]hubclient.TemplateFile, len(files)),
	}
	for i, f := range files {
		manifest.Files[i] = hubclient.TemplateFile{
			Path: f.Path,
			Size: f.Size,
			Hash: f.Hash,
			Mode: f.Mode,
		}
	}

	// Finalize template
	fmt.Println("Finalizing template...")
	updated, err := hubCtx.Client.Templates().Finalize(ctx, template.ID, manifest)
	if err != nil {
		return fmt.Errorf("failed to finalize template: %w", err)
	}

	fmt.Printf("Template '%s' pushed successfully!\n", name)
	fmt.Printf("  Content Hash: %s\n", updated.ContentHash)

	return nil
}

// pullTemplateFromHub downloads a template from the Hub.
func pullTemplateFromHub(hubCtx *HubContext, name, toPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Find the template in Hub
	fmt.Printf("Looking up template '%s' in Hub...\n", name)

	listResp, err := hubCtx.Client.Templates().List(ctx, &hubclient.ListTemplatesOptions{})
	if err != nil {
		return fmt.Errorf("failed to list templates: %w", err)
	}

	var template *hubclient.Template
	for i := range listResp.Templates {
		if listResp.Templates[i].Name == name || listResp.Templates[i].Slug == name {
			template = &listResp.Templates[i]
			break
		}
	}

	if template == nil {
		return fmt.Errorf("template '%s' not found in Hub", name)
	}

	// Determine destination path
	destPath := toPath
	if destPath == "" {
		// Default to project templates directory
		projectTemplatesDir, err := config.GetProjectTemplatesDir()
		if err != nil {
			return fmt.Errorf("failed to get templates directory: %w", err)
		}
		destPath = filepath.Join(projectTemplatesDir, name)
	} else {
		var err error
		destPath, err = filepath.Abs(toPath)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}
	}

	// Create destination directory
	if err := os.MkdirAll(destPath, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Request download URLs
	fmt.Printf("Requesting download URLs for template '%s'...\n", name)
	downloadResp, err := hubCtx.Client.Templates().RequestDownloadURLs(ctx, template.ID)
	if err != nil {
		return fmt.Errorf("failed to get download URLs: %w", err)
	}

	// Download files
	fmt.Printf("Downloading %d files to %s...\n", len(downloadResp.Files), destPath)
	for _, fileInfo := range downloadResp.Files {
		filePath := filepath.Join(destPath, filepath.FromSlash(fileInfo.Path))

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", fileInfo.Path, err)
		}

		// Download file content
		content, err := hubCtx.Client.Templates().DownloadFile(ctx, fileInfo.URL)
		if err != nil {
			return fmt.Errorf("failed to download %s: %w", fileInfo.Path, err)
		}

		// Write file
		if err := os.WriteFile(filePath, content, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", fileInfo.Path, err)
		}
		fmt.Printf("  Downloaded: %s\n", fileInfo.Path)
	}

	fmt.Printf("Template '%s' pulled successfully to %s\n", name, destPath)

	return nil
}

func init() {
	rootCmd.AddCommand(templatesCmd)
	templatesCmd.AddCommand(templatesListCmd)
	templatesCmd.AddCommand(templatesShowCmd)
	templatesCmd.AddCommand(templatesCreateCmd)
	templatesCmd.AddCommand(templatesCloneCmd)
	templatesCmd.AddCommand(templatesDeleteCmd)
	templatesCmd.AddCommand(templatesUpdateDefaultCmd)

	// Hub-only commands
	templatesCmd.AddCommand(templatesSyncCmd)
	templatesCmd.AddCommand(templatesPushCmd)
	templatesCmd.AddCommand(templatesPullCmd)

	// Flags for create command
	templatesCreateCmd.Flags().StringP("harness", "H", "", "Harness type (e.g. gemini, claude)")

	// Flags for sync command
	templatesSyncCmd.Flags().String("from", "", "Source path for template files (required)")
	templatesSyncCmd.Flags().String("scope", "grove", "Template scope (global, grove, user)")
	templatesSyncCmd.Flags().StringP("harness", "H", "", "Harness type (required)")

	// Flags for push command
	templatesPushCmd.Flags().String("from", "", "Source path for template files")

	// Flags for pull command
	templatesPullCmd.Flags().String("to", "", "Destination path for downloaded template")

	// Also add a 'template' alias (singular) for convenience
	templateCmd := &cobra.Command{
		Use:   "template",
		Short: "Manage agent templates (alias for 'templates')",
		Long:  `List and inspect templates used to provision new agents.`,
	}
	rootCmd.AddCommand(templateCmd)
	templateCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List available templates",
		RunE:  templatesListCmd.RunE,
	})
	templateCmd.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show template configuration",
		Args:  cobra.ExactArgs(1),
		RunE:  templatesShowCmd.RunE,
	})
	// Add sync, push, pull to singular alias
	syncAlias := &cobra.Command{
		Use:   "sync <name>",
		Short: "Create or update a template in the Hub (Hub only)",
		Args:  cobra.ExactArgs(1),
		RunE:  templatesSyncCmd.RunE,
	}
	syncAlias.Flags().String("from", "", "Source path for template files (required)")
	syncAlias.Flags().String("scope", "grove", "Template scope (global, grove, user)")
	syncAlias.Flags().StringP("harness", "H", "", "Harness type (required)")
	templateCmd.AddCommand(syncAlias)

	pushAlias := &cobra.Command{
		Use:   "push <name>",
		Short: "Upload local template files to Hub (Hub only)",
		Args:  cobra.ExactArgs(1),
		RunE:  templatesPushCmd.RunE,
	}
	pushAlias.Flags().String("from", "", "Source path for template files")
	templateCmd.AddCommand(pushAlias)

	pullAlias := &cobra.Command{
		Use:   "pull <name>",
		Short: "Download a template from Hub to local cache (Hub only)",
		Args:  cobra.ExactArgs(1),
		RunE:  templatesPullCmd.RunE,
	}
	pullAlias.Flags().String("to", "", "Destination path for downloaded template")
	templateCmd.AddCommand(pullAlias)
}
