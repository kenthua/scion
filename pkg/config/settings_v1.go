// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	"github.com/ptone/scion-agent/pkg/api"
)

// VersionedSettings is the root configuration struct for versioned settings (v1+).
type VersionedSettings struct {
	SchemaVersion   string                          `json:"schema_version" yaml:"schema_version" koanf:"schema_version"`
	ActiveProfile   string                          `json:"active_profile,omitempty" yaml:"active_profile,omitempty" koanf:"active_profile"`
	DefaultTemplate string                          `json:"default_template,omitempty" yaml:"default_template,omitempty" koanf:"default_template"`
	Server          *V1ServerConfig                 `json:"server,omitempty" yaml:"server,omitempty" koanf:"server"`
	Hub             *V1HubClientConfig              `json:"hub,omitempty" yaml:"hub,omitempty" koanf:"hub"`
	CLI             *V1CLIConfig                    `json:"cli,omitempty" yaml:"cli,omitempty" koanf:"cli"`
	Runtimes        map[string]V1RuntimeConfig      `json:"runtimes,omitempty" yaml:"runtimes,omitempty" koanf:"runtimes"`
	HarnessConfigs  map[string]HarnessConfigEntry   `json:"harness_configs,omitempty" yaml:"harness_configs,omitempty" koanf:"harness_configs"`
	Profiles        map[string]V1ProfileConfig      `json:"profiles,omitempty" yaml:"profiles,omitempty" koanf:"profiles"`
}

// V1ServerConfig is a minimal stub for Phase 2.
// Full decomposition (broker, database, auth, oauth, storage, secrets) is deferred to Phase 4.
type V1ServerConfig struct {
	Env       string `json:"env,omitempty" yaml:"env,omitempty" koanf:"env"`
	LogLevel  string `json:"log_level,omitempty" yaml:"log_level,omitempty" koanf:"log_level"`
	LogFormat string `json:"log_format,omitempty" yaml:"log_format,omitempty" koanf:"log_format"`
}

// V1HubClientConfig defines hub client connection settings for versioned config.
// Legacy fields (Token, APIKey, BrokerID, BrokerToken, LastSyncedAt) are removed.
type V1HubClientConfig struct {
	Enabled   *bool  `json:"enabled,omitempty" yaml:"enabled,omitempty" koanf:"enabled"`
	Endpoint  string `json:"endpoint,omitempty" yaml:"endpoint,omitempty" koanf:"endpoint"`
	GroveID   string `json:"grove_id,omitempty" yaml:"grove_id,omitempty" koanf:"grove_id"`
	LocalOnly *bool  `json:"local_only,omitempty" yaml:"local_only,omitempty" koanf:"local_only"`
}

// V1CLIConfig defines CLI behavior settings for versioned config.
type V1CLIConfig struct {
	AutoHelp            *bool `json:"autohelp,omitempty" yaml:"autohelp,omitempty" koanf:"autohelp"`
	InteractiveDisabled *bool `json:"interactive_disabled,omitempty" yaml:"interactive_disabled,omitempty" koanf:"interactive_disabled"`
}

// V1RuntimeConfig extends RuntimeConfig with a Type field.
type V1RuntimeConfig struct {
	Type      string            `json:"type,omitempty" yaml:"type,omitempty" koanf:"type"`
	Host      string            `json:"host,omitempty" yaml:"host,omitempty" koanf:"host"`
	Context   string            `json:"context,omitempty" yaml:"context,omitempty" koanf:"context"`
	Namespace string            `json:"namespace,omitempty" yaml:"namespace,omitempty" koanf:"namespace"`
	Tmux      *bool             `json:"tmux,omitempty" yaml:"tmux,omitempty" koanf:"tmux"`
	Env       map[string]string `json:"env,omitempty" yaml:"env,omitempty" koanf:"env"`
	Sync      string            `json:"sync,omitempty" yaml:"sync,omitempty" koanf:"sync"`
}

// HarnessConfigEntry defines a harness configuration entry in versioned settings.
// The Harness field is required and specifies the harness type this config applies to.
type HarnessConfigEntry struct {
	Harness          string            `json:"harness" yaml:"harness" koanf:"harness"`
	Image            string            `json:"image,omitempty" yaml:"image,omitempty" koanf:"image"`
	User             string            `json:"user,omitempty" yaml:"user,omitempty" koanf:"user"`
	Model            string            `json:"model,omitempty" yaml:"model,omitempty" koanf:"model"`
	Args             []string          `json:"args,omitempty" yaml:"args,omitempty" koanf:"args"`
	Env              map[string]string `json:"env,omitempty" yaml:"env,omitempty" koanf:"env"`
	Volumes          []api.VolumeMount `json:"volumes,omitempty" yaml:"volumes,omitempty" koanf:"volumes"`
	AuthSelectedType string            `json:"auth_selected_type,omitempty" yaml:"auth_selected_type,omitempty" koanf:"auth_selected_type"`
}

// V1ProfileConfig extends ProfileConfig with new fields for versioned settings.
type V1ProfileConfig struct {
	Runtime              string                     `json:"runtime" yaml:"runtime" koanf:"runtime"`
	DefaultTemplate      string                     `json:"default_template,omitempty" yaml:"default_template,omitempty" koanf:"default_template"`
	DefaultHarnessConfig string                     `json:"default_harness_config,omitempty" yaml:"default_harness_config,omitempty" koanf:"default_harness_config"`
	Tmux                 *bool                      `json:"tmux,omitempty" yaml:"tmux,omitempty" koanf:"tmux"`
	Env                  map[string]string          `json:"env,omitempty" yaml:"env,omitempty" koanf:"env"`
	Volumes              []api.VolumeMount          `json:"volumes,omitempty" yaml:"volumes,omitempty" koanf:"volumes"`
	Resources            *api.ResourceSpec          `json:"resources,omitempty" yaml:"resources,omitempty" koanf:"resources"`
	HarnessOverrides     map[string]HarnessOverride `json:"harness_overrides,omitempty" yaml:"harness_overrides,omitempty" koanf:"harness_overrides"`
}

// resolveEffectiveGrovePath resolves the effective grove path for settings loading.
// Shared by both LoadSettingsKoanf and LoadVersionedSettings.
func resolveEffectiveGrovePath(grovePath string) string {
	effectiveGrovePath := grovePath
	if effectiveGrovePath == "" {
		if projectPath, ok := FindProjectRoot(); ok {
			effectiveGrovePath = projectPath
		}
	} else if effectiveGrovePath == "global" || effectiveGrovePath == "home" {
		effectiveGrovePath = ""
	}
	return effectiveGrovePath
}

// LoadVersionedSettings loads settings using Koanf into VersionedSettings.
// Provider priority:
// 1. Embedded defaults (YAML) with OS-specific runtime adjustment
// 2. Global settings file (~/.scion/settings.yaml or .json)
// 3. Grove settings file (.scion/settings.yaml or .json)
// 4. Environment variables (SCION_ prefix)
func LoadVersionedSettings(grovePath string) (*VersionedSettings, error) {
	k := koanf.New(".")

	// 1. Load embedded defaults (YAML)
	if defaultData, err := GetDefaultSettingsDataYAML(); err == nil {
		_ = k.Load(rawbytes.Provider(defaultData), yaml.Parser())
	}

	// 2. Load global settings (~/.scion/settings.yaml or .json)
	globalDir, _ := GetGlobalDir()
	if globalDir != "" {
		if err := loadSettingsFile(k, globalDir); err != nil {
			return nil, err
		}
	}

	// 3. Load grove settings
	effectiveGrovePath := resolveEffectiveGrovePath(grovePath)
	if effectiveGrovePath != "" && effectiveGrovePath != globalDir {
		if err := loadSettingsFile(k, effectiveGrovePath); err != nil {
			return nil, err
		}
	}

	// 4. Load environment variables (SCION_ prefix)
	_ = k.Load(env.Provider("SCION_", ".", versionedEnvKeyMapper), nil)

	// Unmarshal into VersionedSettings struct
	settings := &VersionedSettings{
		Runtimes:       make(map[string]V1RuntimeConfig),
		HarnessConfigs: make(map[string]HarnessConfigEntry),
		Profiles:       make(map[string]V1ProfileConfig),
	}

	if err := k.Unmarshal("", settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// versionedEnvKeyMapper maps SCION_* environment variables to versioned settings keys.
// All keys are snake_case so no camelCase conversion is needed.
func versionedEnvKeyMapper(s string) string {
	key := strings.ToLower(strings.TrimPrefix(s, "SCION_"))

	// Handle nested hub keys
	if strings.HasPrefix(key, "hub_") {
		return "hub." + strings.TrimPrefix(key, "hub_")
	}
	// Handle nested cli keys
	if strings.HasPrefix(key, "cli_") {
		return "cli." + strings.TrimPrefix(key, "cli_")
	}
	// Handle nested server keys
	if strings.HasPrefix(key, "server_") {
		return "server." + strings.TrimPrefix(key, "server_")
	}

	return key
}

// AdaptLegacySettings converts a legacy Settings struct to VersionedSettings.
// Returns the adapted settings and a slice of deprecation warnings.
// This is a pure function with no I/O.
func AdaptLegacySettings(legacy *Settings) (*VersionedSettings, []string) {
	if legacy == nil {
		return &VersionedSettings{SchemaVersion: "1"}, nil
	}

	var warnings []string

	vs := &VersionedSettings{
		SchemaVersion:   "1",
		ActiveProfile:   legacy.ActiveProfile,
		DefaultTemplate: legacy.DefaultTemplate,
	}

	// Adapt Hub config
	if legacy.Hub != nil {
		vs.Hub = &V1HubClientConfig{
			Enabled:   legacy.Hub.Enabled,
			Endpoint:  legacy.Hub.Endpoint,
			GroveID:   legacy.Hub.GroveID,
			LocalOnly: legacy.Hub.LocalOnly,
		}
		if legacy.Hub.Token != "" {
			warnings = append(warnings, "hub.token is deprecated; use server.auth.dev_token for dev mode authentication")
		}
		if legacy.Hub.APIKey != "" {
			warnings = append(warnings, "hub.apiKey is deprecated; API key authentication is no longer supported")
		}
		if legacy.Hub.BrokerID != "" {
			warnings = append(warnings, "hub.brokerId is deprecated; moved to server.broker.broker_id")
		}
		if legacy.Hub.BrokerNickname != "" {
			warnings = append(warnings, "hub.brokerNickname is deprecated; moved to server.broker.broker_nickname")
		}
		if legacy.Hub.BrokerToken != "" {
			warnings = append(warnings, "hub.brokerToken is deprecated; moved to server.broker.broker_token")
		}
		if legacy.Hub.LastSyncedAt != "" {
			warnings = append(warnings, "hub.lastSyncedAt is deprecated; moved to state.yaml")
		}
	}

	// Adapt CLI config
	if legacy.CLI != nil {
		vs.CLI = &V1CLIConfig{
			AutoHelp: legacy.CLI.AutoHelp,
		}
	}

	// Adapt Runtimes — set Type from map key
	if legacy.Runtimes != nil {
		vs.Runtimes = make(map[string]V1RuntimeConfig, len(legacy.Runtimes))
		for name, rc := range legacy.Runtimes {
			vs.Runtimes[name] = V1RuntimeConfig{
				Type:      name,
				Host:      rc.Host,
				Context:   rc.Context,
				Namespace: rc.Namespace,
				Tmux:      rc.Tmux,
				Env:       rc.Env,
				Sync:      rc.Sync,
			}
		}
	}

	// Adapt Harnesses → HarnessConfigs — set Harness from map key
	if legacy.Harnesses != nil {
		vs.HarnessConfigs = make(map[string]HarnessConfigEntry, len(legacy.Harnesses))
		for name, hc := range legacy.Harnesses {
			vs.HarnessConfigs[name] = HarnessConfigEntry{
				Harness:          name,
				Image:            hc.Image,
				User:             hc.User,
				Env:              hc.Env,
				Volumes:          hc.Volumes,
				AuthSelectedType: hc.AuthSelectedType,
			}
		}
		warnings = append(warnings, "harnesses is deprecated; renamed to harness_configs with a required 'harness' field")
	}

	// Adapt Profiles
	if legacy.Profiles != nil {
		vs.Profiles = make(map[string]V1ProfileConfig, len(legacy.Profiles))
		for name, pc := range legacy.Profiles {
			vs.Profiles[name] = V1ProfileConfig{
				Runtime:          pc.Runtime,
				Tmux:             pc.Tmux,
				Env:              pc.Env,
				Volumes:          pc.Volumes,
				Resources:        pc.Resources,
				HarnessOverrides: pc.HarnessOverrides,
			}
		}
	}

	// Warn about Bucket config
	if legacy.Bucket != nil && (legacy.Bucket.Provider != "" || legacy.Bucket.Name != "" || legacy.Bucket.Prefix != "") {
		warnings = append(warnings, "bucket config is deprecated; will consolidate into server.storage")
	}

	return vs, warnings
}

// convertVersionedToLegacy maps VersionedSettings back to legacy Settings.
// Used by GetDefaultSettingsData() so the legacy Koanf loader receives valid data
// after the default file changes format.
func convertVersionedToLegacy(vs *VersionedSettings) *Settings {
	if vs == nil {
		return &Settings{}
	}

	s := &Settings{
		ActiveProfile:   vs.ActiveProfile,
		DefaultTemplate: vs.DefaultTemplate,
	}

	// Convert Hub
	if vs.Hub != nil {
		s.Hub = &HubClientConfig{
			Enabled:   vs.Hub.Enabled,
			Endpoint:  vs.Hub.Endpoint,
			GroveID:   vs.Hub.GroveID,
			LocalOnly: vs.Hub.LocalOnly,
		}
	}

	// Convert CLI
	if vs.CLI != nil {
		s.CLI = &CLIConfig{
			AutoHelp: vs.CLI.AutoHelp,
		}
	}

	// Convert Runtimes — drop Type field
	if vs.Runtimes != nil {
		s.Runtimes = make(map[string]RuntimeConfig, len(vs.Runtimes))
		for name, rc := range vs.Runtimes {
			s.Runtimes[name] = RuntimeConfig{
				Host:      rc.Host,
				Context:   rc.Context,
				Namespace: rc.Namespace,
				Tmux:      rc.Tmux,
				Env:       rc.Env,
				Sync:      rc.Sync,
			}
		}
	}

	// Convert HarnessConfigs → Harnesses — drop new fields (Model, Args, Harness)
	if vs.HarnessConfigs != nil {
		s.Harnesses = make(map[string]HarnessConfig, len(vs.HarnessConfigs))
		for name, hc := range vs.HarnessConfigs {
			s.Harnesses[name] = HarnessConfig{
				Image:            hc.Image,
				User:             hc.User,
				Env:              hc.Env,
				Volumes:          hc.Volumes,
				AuthSelectedType: hc.AuthSelectedType,
			}
		}
	}

	// Convert Profiles — drop new fields (DefaultTemplate, DefaultHarnessConfig)
	if vs.Profiles != nil {
		s.Profiles = make(map[string]ProfileConfig, len(vs.Profiles))
		for name, pc := range vs.Profiles {
			s.Profiles[name] = ProfileConfig{
				Runtime:          pc.Runtime,
				Tmux:             pc.Tmux,
				Env:              pc.Env,
				Volumes:          pc.Volumes,
				Resources:        pc.Resources,
				HarnessOverrides: pc.HarnessOverrides,
			}
		}
	}

	return s
}

// detectHierarchyFormat checks settings files in the global and grove directories
// to determine if any user file uses the versioned format.
// Returns true if any user file is versioned (has schema_version).
func detectHierarchyFormat(grovePath string) (hasVersioned bool) {
	// Check global settings
	globalDir, _ := GetGlobalDir()
	if globalDir != "" {
		if path := GetSettingsPath(globalDir); path != "" {
			if data, err := os.ReadFile(path); err == nil {
				if version, _ := DetectSettingsFormat(data); version != "" {
					return true
				}
			}
		}
	}

	// Check grove settings
	effectiveGrovePath := resolveEffectiveGrovePath(grovePath)
	if effectiveGrovePath != "" && effectiveGrovePath != globalDir {
		if path := GetSettingsPath(effectiveGrovePath); path != "" {
			if data, err := os.ReadFile(path); err == nil {
				if version, _ := DetectSettingsFormat(data); version != "" {
					return true
				}
			}
		}
	}

	return false
}

// LoadEffectiveSettings is a unified entry point that detects the settings format
// and loads using the appropriate path.
// - If any user file is versioned → uses LoadVersionedSettings
// - If all user files are legacy or absent → uses LoadSettingsKoanf + AdaptLegacySettings
// Returns (settings, deprecation_warnings, error).
func LoadEffectiveSettings(grovePath string) (*VersionedSettings, []string, error) {
	if detectHierarchyFormat(grovePath) {
		vs, err := LoadVersionedSettings(grovePath)
		if err != nil {
			return nil, nil, fmt.Errorf("loading versioned settings: %w", err)
		}
		return vs, nil, nil
	}

	// Legacy path: load via existing loader, then adapt
	legacy, err := LoadSettingsKoanf(grovePath)
	if err != nil {
		return nil, nil, fmt.Errorf("loading legacy settings: %w", err)
	}
	vs, warnings := AdaptLegacySettings(legacy)
	return vs, warnings, nil
}

