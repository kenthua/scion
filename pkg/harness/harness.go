package harness

import (
	"github.com/ptone/scion-agent/pkg/api"
)

type Harness interface {
	Name() string
	DiscoverAuth(agentHome string) api.AuthConfig
	GetEnv(agentName string, unixUsername string, model string, auth api.AuthConfig) map[string]string
	GetCommand(task string, resume bool) []string
	PropagateFiles(homeDir, unixUsername string, auth api.AuthConfig) error
	GetVolumes(unixUsername string, auth api.AuthConfig) []api.VolumeMount
	DefaultConfigDir() string
	HasSystemPrompt() bool
}

func New(provider string) Harness {
	switch provider {
	case "claude":
		return &ClaudeCode{}
	case "gemini":
		return &GeminiCLI{}
	}
}
