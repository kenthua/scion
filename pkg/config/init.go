package config

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed embeds/*
var embedsFS embed.FS

func SeedTemplateDir(templateDir string, templateName string, harnessProvider string, force bool) error {
	configDirName := ".gemini"
	embedDir := "gemini"
	if harnessProvider == "claude" {
		configDirName = ".claude"
		embedDir = "claude"
	}

	// Create directories
	dirs := []string{
		templateDir,
		filepath.Join(templateDir, configDirName),
		filepath.Join(templateDir, ".config", "gcloud"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Helper to read embedded file
	readEmbed := func(name string) string {
		data, err := embedsFS.ReadFile(filepath.Join("embeds", embedDir, name))
		if err != nil {
			// Fallback to gemini if not found in provider dir
			data, err = embedsFS.ReadFile(filepath.Join("embeds", "gemini", name))
			if err != nil {
				return ""
			}
		}
		return string(data)
	}

	scionJSON := readEmbed("scion.json")
	if templateName != "" && templateName != "default" {
		scionJSON = strings.Replace(scionJSON, `"template": "default"`, fmt.Sprintf(`"template": %q`, templateName), 1)
	}
	if harnessProvider != "" {
		// Insert harness_provider before unix_username
		providerLine := fmt.Sprintf("  \"harness_provider\": %q,\n", harnessProvider)
		scionJSON = strings.Replace(scionJSON, "\"unix_username\"", providerLine+"  \"unix_username\"", 1)
	}

	mdFile := "gemini.md"
	if harnessProvider == "claude" {
		mdFile = "claude.md"
	}

	// Seed template files
	files := []struct {
		path    string
		content string
	}{
		{filepath.Join(templateDir, "scion.json"), scionJSON},
		{filepath.Join(templateDir, "scion_hook.py"), readEmbed("scion_hook.py")},
		{filepath.Join(templateDir, configDirName, "settings.json"), readEmbed("settings.json")},
		{filepath.Join(templateDir, configDirName, "system_prompt.md"), readEmbed("system_prompt.md")},
		{filepath.Join(templateDir, configDirName, mdFile), readEmbed(mdFile)},
		{filepath.Join(templateDir, ".bashrc"), readEmbed("bashrc")},
	}

	for _, f := range files {
		// Always write settings.json to ensure it matches current defaults
		if force || filepath.Base(f.path) == "settings.json" {
			if err := os.WriteFile(f.path, []byte(f.content), 0644); err != nil {
				return fmt.Errorf("failed to write file %s: %w", f.path, err)
			}
			continue
		}

		if _, err := os.Stat(f.path); os.IsNotExist(err) {
			if err := os.WriteFile(f.path, []byte(f.content), 0644); err != nil {
				return fmt.Errorf("failed to write file %s: %w", f.path, err)
			}
		}
	}

	return nil
}

func InitProject(targetDir string) error {
	var projectDir string
	var err error

	if targetDir != "" {
		projectDir = targetDir
	} else {
		projectDir, err = GetTargetProjectDir()
		if err != nil {
			return err
		}
	}

	templatesDir := filepath.Join(projectDir, "templates")
	agentsDir := filepath.Join(projectDir, "agents")

	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create agents directory: %w", err)
	}

	if err := SeedTemplateDir(filepath.Join(templatesDir, "gemini"), "gemini", "gemini", false); err != nil {
		return fmt.Errorf("failed to seed gemini template: %w", err)
	}

	return SeedTemplateDir(filepath.Join(templatesDir, "claude"), "claude", "claude", false)
}

func InitGlobal() error {
	globalDir, err := GetGlobalDir()
	if err != nil {
		return err
	}

	templatesDir := filepath.Join(globalDir, "templates")
	agentsDir := filepath.Join(globalDir, "agents")

	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create global agents directory: %w", err)
	}

	if err := SeedTemplateDir(filepath.Join(templatesDir, "gemini"), "gemini", "gemini", false); err != nil {
		return fmt.Errorf("failed to seed global gemini template: %w", err)
	}

	return SeedTemplateDir(filepath.Join(templatesDir, "claude"), "claude", "claude", false)
}
