package harness

import (
	"os"

	"github.com/ptone/scion-agent/pkg/api"
)

type ClaudeCode struct{}

func (c *ClaudeCode) Name() string {
	return "claude"
}

func (c *ClaudeCode) DiscoverAuth(agentHome string) api.AuthConfig {
	// Placeholder for Claude specific auth discovery
	return api.AuthConfig{
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
	}
}

func (c *ClaudeCode) GetEnv(agentName string, unixUsername string, model string, auth api.AuthConfig) map[string]string {
	env := make(map[string]string)
	// Placeholder for Claude specific env
	return env
}

func (c *ClaudeCode) GetCommand(task string, resume bool) []string {
	// Placeholder for Claude specific command
	return []string{"claude", task}
}

func (c *ClaudeCode) PropagateFiles(homeDir, unixUsername string, auth api.AuthConfig) error {
	return nil
}

func (c *ClaudeCode) GetVolumes(unixUsername string, auth api.AuthConfig) []api.VolumeMount {
	return nil
}

func (c *ClaudeCode) DefaultConfigDir() string {
	return ".claude"
}

func (c *ClaudeCode) HasSystemPrompt() bool {
	return true
}
