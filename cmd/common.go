package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ptone/scion-agent/pkg/api"
	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/harness"
	"github.com/ptone/scion-agent/pkg/runtime"
	"github.com/ptone/scion-agent/pkg/util"
)

func DeleteAgentFiles(agentName string) error {
	var agentsDirs []string
	if projectDir, err := config.GetResolvedProjectDir(grovePath); err == nil {
		agentsDirs = append(agentsDirs, filepath.Join(projectDir, "agents"))
	}
	// Also check global just in case
	if globalDir, err := config.GetGlobalAgentsDir(); err == nil {
		agentsDirs = append(agentsDirs, globalDir)
	}

	for _, dir := range agentsDirs {
		agentDir := filepath.Join(dir, agentName)
		if _, err := os.Stat(agentDir); err != nil {
			continue
		}

		agentWorkspace := filepath.Join(agentDir, "workspace")
		// Check if it's a worktree before trying to remove it
		if _, err := os.Stat(filepath.Join(agentWorkspace, ".git")); err == nil {
			fmt.Printf("Removing git worktree for agent '%s'...\n", agentName)
			if err := util.RemoveWorktree(agentWorkspace); err != nil {
				fmt.Printf("Warning: failed to remove worktree at %s: %v\n", agentWorkspace, err)
			}
		}

		// Also ensure the agent directory is cleaned up
		fmt.Printf("Removing agent directory for '%s'...\n", agentName)
		if err := os.RemoveAll(agentDir); err != nil {
			return fmt.Errorf("failed to remove agent directory: %w", err)
		}
	}
	return nil
}

func ProvisionAgent(agentName string, templateName string, agentImage string, grovePath string, optionalStatus string) (string, string, *api.ScionConfig, error) {
	// 1. Prepare agent directories
	projectDir, err := config.GetResolvedProjectDir(grovePath)
	if err != nil {
		return "", "", nil, err
	}

	groveName := config.GetGroveName(projectDir)

	// Verify .gitignore if in a repo
	if util.IsGitRepo() {
		// Find the projectDir relative to repo root if possible
		root, err := util.RepoRoot()
		if err == nil {
			rel, err := filepath.Rel(root, projectDir)
			if err == nil && !strings.HasPrefix(rel, "..") {
				agentsPath := filepath.Join(rel, "agents")
				if !util.IsIgnored(agentsPath + "/") {
					return "", "", nil, fmt.Errorf("security error: '%s/' must be in .gitignore when using a project-local grove", agentsPath)
				}
			}
		}
	}
	agentsDir := filepath.Join(projectDir, "agents")

	agentDir := filepath.Join(agentsDir, agentName)
	agentHome := filepath.Join(agentDir, "home")
	agentWorkspace := filepath.Join(agentDir, "workspace")

	if err := os.MkdirAll(agentHome, 0755); err != nil {
		return "", "", nil, fmt.Errorf("failed to create agent home: %w", err)
	}

	// Create empty prompt.md in agent root
	promptFile := filepath.Join(agentDir, "prompt.md")
	if _, err := os.Stat(promptFile); os.IsNotExist(err) {
		if err := os.WriteFile(promptFile, []byte(""), 0644); err != nil {
			return "", "", nil, fmt.Errorf("failed to create prompt.md: %w", err)
		}
	}

	if util.IsGitRepo() {
		fmt.Printf("Creating git worktree for agent '%s'...\n", agentName)
		// Remove existing workspace dir if it exists to allow worktree add
		os.RemoveAll(agentWorkspace)
		if err := util.CreateWorktree(agentWorkspace, agentName); err != nil {
			return "", "", nil, fmt.Errorf("failed to create git worktree: %w", err)
		}
	} else {
		if err := os.MkdirAll(agentWorkspace, 0755); err != nil {
			return "", "", nil, fmt.Errorf("failed to create agent workspace: %w", err)
		}
	}

	// 2. Load and copy templates
	chain, err := config.GetTemplateChain(templateName)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to load template: %w", err)
	}

	finalScionCfg := &api.ScionConfig{}

	for _, tpl := range chain {
		fmt.Printf("Applying template: %s\n", tpl.Name)
		if err := util.CopyDir(tpl.Path, agentHome); err != nil {
			return "", "", nil, fmt.Errorf("failed to copy template %s: %w", tpl.Name, err)
		}

		// Load scion.json from this template and merge it
		tplCfg, err := tpl.LoadConfig()
		if err == nil {
			finalScionCfg = config.MergeScionConfig(finalScionCfg, tplCfg)
		}
	}

	// Update agent-specific scion.json
	if finalScionCfg == nil {
		finalScionCfg = &api.ScionConfig{}
	}
	finalScionCfg.Template = templateName
	finalScionCfg.Agent = &api.AgentConfig{
		Grove: groveName,
		Name:  agentName,
	}
	if optionalStatus != "" {
		finalScionCfg.Agent.Status = optionalStatus
	}
	if agentImage != "" {
		finalScionCfg.Image = agentImage
	}
	agentCfgData, _ := json.MarshalIndent(finalScionCfg, "", "  ")
	os.WriteFile(filepath.Join(agentHome, "scion.json"), agentCfgData, 0644)

	return agentHome, agentWorkspace, finalScionCfg, nil
}

var (
	templateName string
	agentImage   string
	noAuth       bool
	attach       bool
	model        string
)

func RunAgent(cmd *cobra.Command, args []string, resume bool) error {
	agentName := args[0]
	task := strings.Join(args[1:], " ")

	// 0. Check if container already exists
	rt := runtime.GetRuntime()
	agents, err := rt.List(context.Background(), nil)
	if err == nil {
		for _, a := range agents {
			if a.ID == agentName || a.Name == agentName {
				isRunning := strings.Contains(a.Status, "Up") || strings.Contains(a.Status, "running")
				if isRunning {
					fmt.Printf("Agent container '%s' is already running.\n", agentName)
					if attach {
						fmt.Printf("Attaching to agent '%s'...\n", agentName)
						return rt.Attach(context.Background(), a.ID)
					}
					return nil
				}
				// If it exists but not running, we delete it so we can recreate it
				action := "Re-starting"
				if resume {
					action = "Resuming"
				}
				fmt.Printf("Agent container '%s' exists but is not running (Status: %s). %s...\n", agentName, a.Status, action)
				_ = rt.Delete(context.Background(), a.ID)
			}
		}
	}

	projectDir, err := config.GetResolvedProjectDir(grovePath)
	if err != nil {
		return err
	}
	groveName := config.GetGroveName(projectDir)

	agentDir, agentHome, agentWorkspace, finalScionCfg, err := GetAgent(agentName, templateName, agentImage, grovePath, "")
	if err != nil {
		return err
	}

	promptFile := filepath.Join(agentDir, "prompt.md")
	promptFileContent := ""
	if content, err := os.ReadFile(promptFile); err == nil {
		promptFileContent = strings.TrimSpace(string(content))
	}

	if task == "" && promptFileContent == "" {
		return fmt.Errorf("no task provided: prompt.md is empty and no task arguments were given")
	}

	if task != "" && promptFileContent != "" {
		return fmt.Errorf("task conflict: both prompt.md and command line arguments provide a task")
	}

	if task == "" {
		task = promptFileContent
	} else if promptFileContent == "" {
		// Update prompt.md for posterity if it was empty
		_ = os.WriteFile(promptFile, []byte(task), 0644)
	}

	if resume {
		fmt.Printf("Resuming agent '%s' for task: %s\n", agentName, task)
	} else {
		fmt.Printf("Starting agent '%s' for task: %s\n", agentName, task)
	}

	// Resolve image
	resolvedImage := ""
	if finalScionCfg != nil && finalScionCfg.Image != "" {
		resolvedImage = finalScionCfg.Image
	}
	// Flag takes ultimate precedence
	if agentImage != "" {
		resolvedImage = agentImage
	}
	if resolvedImage == "" {
		resolvedImage = "gemini-cli-sandbox"
	}

	if os.Getenv("SCION_DEBUG") != "" {
		fmt.Printf("Debug: resolved container image='%s'\n", resolvedImage)
	}

	harnessProvider := ""
	if finalScionCfg != nil {
		harnessProvider = finalScionCfg.HarnessProvider
	}
	h := harness.New(harnessProvider)

	// 3. Propagate credentials
	var auth api.AuthConfig
	if !noAuth {
		auth = h.DiscoverAuth(agentHome)
	}

	// 4. Launch container
	rt = runtime.GetRuntime()

	detached := true
	useTmux := false
	resolvedModel := "flash"
	unixUsername := "node"

	if finalScionCfg != nil {
		detached = finalScionCfg.IsDetached()
		useTmux = finalScionCfg.IsUseTmux()
		if finalScionCfg.Model != "" {
			resolvedModel = finalScionCfg.Model
		}
		if finalScionCfg.UnixUsername != "" {
			unixUsername = finalScionCfg.UnixUsername
		}
	}

	// -a flag overrides detached config
	if cmd.Flags().Changed("attach") {
		detached = !attach
	}

	if model != "" {
		resolvedModel = model
	}

	if useTmux {
		tmuxImage := resolvedImage
		if !strings.Contains(tmuxImage, ":") {
			tmuxImage = tmuxImage + ":tmux"
		} else {
			parts := strings.SplitN(resolvedImage, ":", 2)
			tmuxImage = parts[0] + ":tmux"
		}
		resolvedImage = tmuxImage
	}

	exists, err := rt.ImageExists(context.Background(), resolvedImage)
	if err != nil || !exists {
		fmt.Printf("Image '%s' not found locally, pulling...\n", resolvedImage)
		if err := rt.PullImage(context.Background(), resolvedImage); err != nil {
			if useTmux {
				return fmt.Errorf("tmux support requested but image '%s' not found and pull failed: %w. Please ensure the image has a :tmux tag.", resolvedImage, err)
			}
			return fmt.Errorf("failed to pull image '%s': %w", resolvedImage, err)
		}
	}

	agentEnv := []string{}
	if finalScionCfg != nil && finalScionCfg.Env != nil {
		for k, v := range finalScionCfg.Env {
			agentEnv = append(agentEnv, fmt.Sprintf("%s=%s", k, v))
		}
	}

	template := ""
	if finalScionCfg != nil {
		template = finalScionCfg.Template
	}

	repoRoot := ""
	if util.IsGitRepo() {
		repoRoot, _ = util.RepoRoot()
	}

	runCfg := runtime.RunConfig{
		Name:         agentName,
		Template:     template,
		UnixUsername: unixUsername,
		Image:        resolvedImage,
		HomeDir:      agentHome,
		Workspace:    agentWorkspace,
		RepoRoot:     repoRoot,
		Auth:         auth,
		Harness:      h,
		UseTmux:      useTmux,
		Model:        resolvedModel,
		Task:         task,
		Env:          agentEnv,
		Volumes: func() []api.VolumeMount {
			if finalScionCfg != nil {
				return finalScionCfg.Volumes
			}
			return nil
		}(),
		Resume: resume,
		Labels: map[string]string{
			"scion.agent":      "true",
			"scion.name":       agentName,
			"scion.grove":      groveName,
			"scion.grove_path": projectDir,
		},
	}

	id, err := rt.Run(context.Background(), runCfg)
	if err != nil {
		return fmt.Errorf("failed to launch container: %w", err)
	}

	status := "running"
	if resume {
		status = "resumed"
	}
	_ = UpdateAgentStatus(agentName, status)

	if detached {
		displayStatus := "launched"
		if resume {
			displayStatus = "resumed"
		}
		fmt.Printf("Agent '%s' %s successfully (ID: %s)\n", agentName, displayStatus, id)
	} else {
		fmt.Printf("Attaching to agent '%s'...\n", agentName)
		return rt.Attach(context.Background(), id)
	}

	return nil
}

func UpdateAgentStatus(agentName string, status string) error {
	projectDir, err := config.GetResolvedProjectDir(grovePath)
	if err != nil {
		return err
	}
	agentsDir := filepath.Join(projectDir, "agents")
	agentDir := filepath.Join(agentsDir, agentName)
	agentHome := filepath.Join(agentDir, "home")
	scionJsonPath := filepath.Join(agentHome, "scion.json")

	if _, err := os.Stat(scionJsonPath); os.IsNotExist(err) {
		return nil // Nothing to update
	}

	data, err := os.ReadFile(scionJsonPath)
	if err != nil {
		return err
	}

	var cfg api.ScionConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	if cfg.Agent == nil {
		cfg.Agent = &api.AgentConfig{}
	}
	cfg.Agent.Status = status

	newData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(scionJsonPath, newData, 0644)
}

func GetAgent(agentName string, templateName string, agentImage string, grovePath string, optionalStatus string) (string, string, string, *api.ScionConfig, error) {
	projectDir, err := config.GetResolvedProjectDir(grovePath)
	if err != nil {
		return "", "", "", nil, err
	}
	agentsDir := filepath.Join(projectDir, "agents")
	agentDir := filepath.Join(agentsDir, agentName)
	agentHome := filepath.Join(agentDir, "home")
	agentWorkspace := filepath.Join(agentDir, "workspace")

	if _, err := os.Stat(agentDir); os.IsNotExist(err) {
		fmt.Printf("Provisioning agent '%s'...\n", agentName)
		home, ws, cfg, err := ProvisionAgent(agentName, templateName, agentImage, grovePath, optionalStatus)
		return agentDir, home, ws, cfg, err
	}

	fmt.Printf("Using existing agent '%s'...\n", agentName)

	// Load the agent's scion.json
	tpl := &config.Template{Path: agentHome}
	agentCfg, err := tpl.LoadConfig()
	if err != nil {
		return agentDir, agentHome, agentWorkspace, nil, fmt.Errorf("failed to load agent config: %w", err)
	}

	// Re-construct the full config by merging the template chain
	// The agent's scion.json acts as the final override

	// Determine the template name from the agent's config or default
	effectiveTemplate := "gemini"
	if agentCfg.Template != "" {
		effectiveTemplate = agentCfg.Template
	}

	chain, err := config.GetTemplateChain(effectiveTemplate)
	if err != nil {
		// If we can't find the template, warn but proceed with just the agent config
		fmt.Printf("Warning: failed to load template chain for '%s': %v. Using agent config as is.\n", effectiveTemplate, err)
		return agentDir, agentHome, agentWorkspace, agentCfg, nil
	}

	mergedCfg := &api.ScionConfig{}
	for _, tpl := range chain {
		tplCfg, err := tpl.LoadConfig()
		if err == nil {
			if os.Getenv("SCION_DEBUG") != "" {
				fmt.Printf("Debug: merging template '%s', image='%s'\n", tpl.Name, tplCfg.Image)
			}
			mergedCfg = config.MergeScionConfig(mergedCfg, tplCfg)
		}
	}

	if os.Getenv("SCION_DEBUG") != "" {
		fmt.Printf("Debug: agent config image='%s'\n", agentCfg.Image)
	}

	// Finally merge the agent's specific config on top
	finalCfg := config.MergeScionConfig(mergedCfg, agentCfg)

	if os.Getenv("SCION_DEBUG") != "" {
		fmt.Printf("Debug: final merged config image='%s'\n", finalCfg.Image)
	}

	return agentDir, agentHome, agentWorkspace, finalCfg, nil
}