package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ptone/scion-agent/pkg/api"
)

type Template struct {
	Name string
	Path string
}

func (t *Template) LoadConfig() (*api.ScionConfig, error) {
	path := filepath.Join(t.Path, "scion.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &api.ScionConfig{}, nil
		}
		return nil, err
	}

	var cfg api.ScionConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func FindTemplate(name string) (*Template, error) {
	// 1. Check project-local templates
	projectTemplatesDir, err := GetProjectTemplatesDir()
	if err == nil {
		path := filepath.Join(projectTemplatesDir, name)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return &Template{Name: name, Path: path}, nil
		}
	}

	// 2. Check global templates
	globalTemplatesDir, err := GetGlobalTemplatesDir()
	if err == nil {
		path := filepath.Join(globalTemplatesDir, name)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return &Template{Name: name, Path: path}, nil
		}
	}

	return nil, fmt.Errorf("template %s not found", name)
}

// GetTemplateChain returns a list of templates in inheritance order (base first)
func GetTemplateChain(name string) ([]*Template, error) {
	var chain []*Template

	tpl, err := FindTemplate(name)
	if err != nil {
		return nil, err
	}
	chain = append(chain, tpl)

	return chain, nil
}

func CreateTemplate(name string, harnessProvider string, global bool) error {
	var templatesDir string
	var err error

	if global {
		templatesDir, err = GetGlobalTemplatesDir()
	} else {
		templatesDir, err = GetProjectTemplatesDir()
	}

	if err != nil {
		return err
	}

	templateDir := filepath.Join(templatesDir, name)
	if _, err := os.Stat(templateDir); err == nil {
		return fmt.Errorf("template %s already exists at %s", name, templateDir)
	}

	return SeedTemplateDir(templateDir, name, harnessProvider, false)
}

func UpdateDefaultTemplates(global bool) error {
	var templatesDir string
	var err error

	if global {
		templatesDir, err = GetGlobalTemplatesDir()
	} else {
		templatesDir, err = GetProjectTemplatesDir()
	}

	if err != nil {
		return err
	}

	if err := SeedTemplateDir(filepath.Join(templatesDir, "gemini"), "gemini", "gemini", true); err != nil {
		return err
	}
	return SeedTemplateDir(filepath.Join(templatesDir, "claude"), "claude", "claude", true)
}

func DeleteTemplate(name string, global bool) error {
	if name == "default" || name == "gemini" || name == "claude" {
		return fmt.Errorf("cannot delete protected template: %s", name)
	}

	var templatesDir string
	var err error

	if global {
		templatesDir, err = GetGlobalTemplatesDir()
	} else {
		templatesDir, err = GetProjectTemplatesDir()
	}

	if err != nil {
		return err
	}

	templateDir := filepath.Join(templatesDir, name)
	if info, err := os.Stat(templateDir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("template %s not found", name)
		}
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", templateDir)
	}

	return os.RemoveAll(templateDir)
}

func ListTemplates() ([]*Template, error) {
	templates := make(map[string]*Template)

	// Helper to scan a directory for templates
	scan := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				templates[e.Name()] = &Template{
					Name: e.Name(),
					Path: filepath.Join(dir, e.Name()),
				}
			}
		}
	}

	// 1. Scan global templates (lower precedence in map)
	if globalDir, err := GetGlobalTemplatesDir(); err == nil {
		scan(globalDir)
	}

	// 2. Scan project templates (higher precedence)
	if projectDir, err := GetProjectTemplatesDir(); err == nil {
		scan(projectDir)
	}

	var list []*Template
	for _, t := range templates {
		list = append(list, t)
	}
	return list, nil
}

func MergeScionConfig(base, override *api.ScionConfig) *api.ScionConfig {
	if base == nil {
		base = &api.ScionConfig{}
	}
	if override == nil {
		return base
	}

	result := *base // Shallow copy initially

	if override.Template != "" {
		result.Template = override.Template
	}
	if override.HarnessProvider != "" {
		result.HarnessProvider = override.HarnessProvider
	}
	if override.ConfigDir != "" {
		result.ConfigDir = override.ConfigDir
	}
	if override.Env != nil {
		newEnv := make(map[string]string, len(base.Env)+len(override.Env))
		for k, v := range base.Env {
			newEnv[k] = v
		}
		for k, v := range override.Env {
			newEnv[k] = v
		}
		result.Env = newEnv
	}
	if override.Volumes != nil {
		newVolumes := make([]api.VolumeMount, 0, len(base.Volumes)+len(override.Volumes))
		newVolumes = append(newVolumes, base.Volumes...)
		newVolumes = append(newVolumes, override.Volumes...)
		result.Volumes = newVolumes
	}
	if override.UnixUsername != "" {
		result.UnixUsername = override.UnixUsername
	}
	if override.Image != "" {
		result.Image = override.Image
	}
	if override.Detached != nil {
		result.Detached = override.Detached
	}
	if override.UseTmux != nil {
		result.UseTmux = override.UseTmux
	}
	if override.Model != "" {
		result.Model = override.Model
	}
	if override.Agent != nil {
		if result.Agent == nil {
			agentCopy := *override.Agent
			result.Agent = &agentCopy
		} else {
			agentCopy := *result.Agent
			if override.Agent.Grove != "" {
				agentCopy.Grove = override.Agent.Grove
			}
			if override.Agent.Name != "" {
				agentCopy.Name = override.Agent.Name
			}
			if override.Agent.Status != "" {
				agentCopy.Status = override.Agent.Status
			}
			result.Agent = &agentCopy
		}
	}

	return &result
}

