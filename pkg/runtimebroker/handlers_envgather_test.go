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

package runtimebroker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptone/scion-agent/pkg/runtime"
)

// newTestServerWithGrovePath creates a test server with a temporary grove path
// that has versioned settings with declared env vars.
func newTestServerWithGrovePath(t *testing.T, settingsYAML string) (*Server, *envCapturingManager, string) {
	t.Helper()

	// Create temp grove directory with settings
	// LoadEffectiveSettings expects a dir that contains settings.yaml directly
	groveDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(groveDir, "settings.yaml"), []byte(settingsYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultServerConfig()
	cfg.BrokerID = "test-broker-id"
	cfg.BrokerName = "test-host"
	cfg.Debug = true

	mgr := &envCapturingManager{}
	rt := &runtime.MockRuntime{}

	return New(cfg, mgr, rt), mgr, groveDir
}

// TestEnvGather_AllSatisfied tests the fast path: all required env keys are provided
// by the Hub and/or Broker, so the agent starts immediately (200/201).
func TestEnvGather_AllSatisfied(t *testing.T) {
	settings := `
schema_version: "1"
harness_configs:
  claude:
    harness: claude
    env:
      API_KEY: ""
profiles:
  default:
    runtime: docker
`
	srv, mgr, groveDir := newTestServerWithGrovePath(t, settings)

	body := `{
		"name": "test-agent",
		"id": "agent-uuid-123",
		"gatherEnv": true,
		"grovePath": "` + groveDir + `",
		"resolvedEnv": {"API_KEY": "sk-test-key", "ANTHROPIC_API_KEY": "sk-ant-key"},
		"config": {"template": "claude", "profile": "default"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Agent should have started with the key
	if mgr.lastEnv == nil {
		t.Fatal("expected env to be set")
	}
	if mgr.lastEnv["API_KEY"] != "sk-test-key" {
		t.Errorf("expected API_KEY='sk-test-key', got %q", mgr.lastEnv["API_KEY"])
	}
}

// TestEnvGather_NeedsKeys tests the gather path: required env keys are missing,
// so the broker returns 202 with requirements.
func TestEnvGather_NeedsKeys(t *testing.T) {
	settings := `
schema_version: "1"
harness_configs:
  claude:
    harness: claude
    env:
      API_KEY: ""
      SECRET_TOKEN: ""
profiles:
  default:
    runtime: docker
`
	srv, _, groveDir := newTestServerWithGrovePath(t, settings)

	body := `{
		"name": "test-agent-gather",
		"id": "agent-uuid-456",
		"gatherEnv": true,
		"grovePath": "` + groveDir + `",
		"resolvedEnv": {"API_KEY": "sk-from-hub"},
		"config": {"template": "claude", "profile": "default"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var envReqs EnvRequirementsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &envReqs); err != nil {
		t.Fatal("failed to decode response:", err)
	}

	if envReqs.AgentID != "agent-uuid-456" {
		t.Errorf("expected agentId='agent-uuid-456', got %q", envReqs.AgentID)
	}

	// API_KEY should be in hubHas
	found := false
	for _, k := range envReqs.HubHas {
		if k == "API_KEY" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected API_KEY in hubHas, got %v", envReqs.HubHas)
	}

	// SECRET_TOKEN should be in needs
	found = false
	for _, k := range envReqs.Needs {
		if k == "SECRET_TOKEN" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SECRET_TOKEN in needs, got %v", envReqs.Needs)
	}
}

// TestEnvGather_BrokerHasKey tests that the broker checks its own environment
// for missing keys before returning 202.
func TestEnvGather_BrokerHasKey(t *testing.T) {
	settings := `
schema_version: "1"
harness_configs:
  claude:
    harness: claude
    env:
      BROKER_LOCAL_KEY: ""
profiles:
  default:
    runtime: docker
`
	srv, mgr, groveDir := newTestServerWithGrovePath(t, settings)

	// Set the keys in the broker's own environment
	t.Setenv("BROKER_LOCAL_KEY", "broker-value")
	t.Setenv("ANTHROPIC_API_KEY", "broker-anthropic-key")

	body := `{
		"name": "test-agent-broker-env",
		"id": "agent-uuid-789",
		"gatherEnv": true,
		"grovePath": "` + groveDir + `",
		"config": {"template": "claude", "profile": "default"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	// Should succeed (broker satisfies the key from its own env)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if mgr.lastEnv == nil {
		t.Fatal("expected env to be set")
	}
	if mgr.lastEnv["BROKER_LOCAL_KEY"] != "broker-value" {
		t.Errorf("expected BROKER_LOCAL_KEY='broker-value', got %q", mgr.lastEnv["BROKER_LOCAL_KEY"])
	}
}

// TestEnvGather_FinalizeEnv tests the finalize-env endpoint: after receiving
// a 202, the caller submits gathered env and the agent starts.
func TestEnvGather_FinalizeEnv(t *testing.T) {
	settings := `
schema_version: "1"
harness_configs:
  claude:
    harness: claude
    env:
      NEEDED_KEY: ""
profiles:
  default:
    runtime: docker
`
	srv, mgr, groveDir := newTestServerWithGrovePath(t, settings)

	// Phase 1: Create agent with gather — should get 202
	createBody := `{
		"name": "test-agent-finalize",
		"id": "agent-uuid-fin",
		"gatherEnv": true,
		"grovePath": "` + groveDir + `",
		"config": {"template": "claude", "profile": "default"}
	}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()

	srv.Handler().ServeHTTP(createW, createReq)

	if createW.Code != http.StatusAccepted {
		t.Fatalf("phase 1: expected 202, got %d: %s", createW.Code, createW.Body.String())
	}

	// Phase 2: Submit gathered env via finalize-env
	finalizeBody := `{"env": {"NEEDED_KEY": "gathered-value"}}`
	finalizeReq := httptest.NewRequest(http.MethodPost, "/api/v1/agents/test-agent-finalize/finalize-env", strings.NewReader(finalizeBody))
	finalizeReq.Header.Set("Content-Type", "application/json")
	finalizeW := httptest.NewRecorder()

	srv.Handler().ServeHTTP(finalizeW, finalizeReq)

	if finalizeW.Code != http.StatusCreated {
		t.Fatalf("phase 2: expected 201, got %d: %s", finalizeW.Code, finalizeW.Body.String())
	}

	// Verify agent was started with the gathered key
	if mgr.lastEnv == nil {
		t.Fatal("expected env to be set after finalize")
	}
	if mgr.lastEnv["NEEDED_KEY"] != "gathered-value" {
		t.Errorf("expected NEEDED_KEY='gathered-value', got %q", mgr.lastEnv["NEEDED_KEY"])
	}
}

// newTestServerWithHarnessConfig creates a test server with a temporary grove path
// that has a harness-config directory and optional settings YAML.
func newTestServerWithHarnessConfig(t *testing.T, harnessConfigName, configYAML, settingsYAML string) (*Server, *envCapturingManager, string) {
	t.Helper()

	groveDir := t.TempDir()

	// Create harness-configs/<name>/config.yaml
	hcDir := filepath.Join(groveDir, "harness-configs", harnessConfigName)
	if err := os.MkdirAll(hcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hcDir, "config.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Write settings.yaml if provided
	if settingsYAML != "" {
		if err := os.WriteFile(filepath.Join(groveDir, "settings.yaml"), []byte(settingsYAML), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := DefaultServerConfig()
	cfg.BrokerID = "test-broker-id"
	cfg.BrokerName = "test-host"
	cfg.Debug = true

	mgr := &envCapturingManager{}
	rt := &runtime.MockRuntime{}

	return New(cfg, mgr, rt), mgr, groveDir
}

// TestEnvGather_HarnessAware tests that the broker detects required env keys
// from the harness itself, even when no harness_configs.env is declared in settings.
func TestEnvGather_HarnessAware(t *testing.T) {
	// No harness_configs in settings, but harness-config dir exists for "claude"
	srv, _, groveDir := newTestServerWithHarnessConfig(t, "claude",
		"harness: claude\nimage: test-image\nuser: scion\n",
		`
schema_version: "1"
profiles:
  default:
    runtime: docker
`)

	body := `{
		"name": "test-agent-harness-aware",
		"id": "agent-uuid-ha",
		"gatherEnv": true,
		"grovePath": "` + groveDir + `",
		"config": {"template": "claude", "profile": "default"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	// Should return 202 because ANTHROPIC_API_KEY is needed but not provided
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var envReqs EnvRequirementsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &envReqs); err != nil {
		t.Fatal("failed to decode response:", err)
	}

	// ANTHROPIC_API_KEY should be in needs
	found := false
	for _, k := range envReqs.Needs {
		if k == "ANTHROPIC_API_KEY" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ANTHROPIC_API_KEY in needs, got needs=%v required=%v", envReqs.Needs, envReqs.Required)
	}
}

// TestEnvGather_GeminiAuthType tests that the gemini harness returns different
// required keys depending on auth_selected_type.
func TestEnvGather_GeminiAuthType(t *testing.T) {
	// Gemini with vertex-ai auth type → needs GOOGLE_CLOUD_PROJECT
	srv, _, groveDir := newTestServerWithHarnessConfig(t, "gemini",
		"harness: gemini\nimage: test-image\nuser: scion\nauth_selected_type: vertex-ai\n",
		`
schema_version: "1"
profiles:
  default:
    runtime: docker
`)

	body := `{
		"name": "test-agent-gemini-vertex",
		"id": "agent-uuid-gv",
		"gatherEnv": true,
		"grovePath": "` + groveDir + `",
		"config": {"template": "gemini", "profile": "default"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var envReqs EnvRequirementsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &envReqs); err != nil {
		t.Fatal("failed to decode response:", err)
	}

	found := false
	for _, k := range envReqs.Needs {
		if k == "GOOGLE_CLOUD_PROJECT" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected GOOGLE_CLOUD_PROJECT in needs for vertex-ai auth, got needs=%v required=%v", envReqs.Needs, envReqs.Required)
	}
}

// TestEnvGather_SettingsAuthTypeOverride tests that a settings profile override
// for auth_selected_type takes precedence over the on-disk harness-config value.
func TestEnvGather_SettingsAuthTypeOverride(t *testing.T) {
	// On-disk config says gemini-api-key, but settings profile overrides to oauth-personal
	srv, mgr, groveDir := newTestServerWithHarnessConfig(t, "gemini",
		"harness: gemini\nimage: test-image\nuser: scion\nauth_selected_type: gemini-api-key\n",
		`
schema_version: "1"
profiles:
  default:
    runtime: docker
    harness_overrides:
      gemini:
        auth_selected_type: oauth-personal
`)

	body := `{
		"name": "test-agent-override",
		"id": "agent-uuid-ov",
		"gatherEnv": true,
		"grovePath": "` + groveDir + `",
		"config": {"template": "gemini", "profile": "default"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	// oauth-personal requires no env keys, so the agent should start immediately
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 (oauth-personal needs no env), got %d: %s", w.Code, w.Body.String())
	}

	if mgr.lastEnv == nil {
		t.Fatal("expected env to be set")
	}
}

// TestEnvGather_NoGatherFlag tests that env-gather is skipped when GatherEnv is false.
func TestEnvGather_NoGatherFlag(t *testing.T) {
	settings := `
schema_version: "1"
harness_configs:
  claude:
    harness: claude
    env:
      MISSING_KEY: ""
profiles:
  default:
    runtime: docker
`
	srv, mgr, groveDir := newTestServerWithGrovePath(t, settings)

	body := `{
		"name": "test-agent-no-gather",
		"id": "agent-uuid-no-gather",
		"gatherEnv": false,
		"grovePath": "` + groveDir + `",
		"config": {"template": "claude", "profile": "default"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	// Should create the agent normally (201) even though env is missing
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Agent was started (env gather skipped)
	if mgr.lastEnv == nil {
		t.Fatal("expected env to be set")
	}
}

// TestEnvGather_SecretAutoUpgrade tests that when all required env keys are
// satisfied by resolved secrets, the env-gather check passes through without
// returning 202. The agent proceeds to creation (which may fail for other
// reasons in the test environment, but the key point is no 202 is returned).
func TestEnvGather_SecretAutoUpgrade(t *testing.T) {
	settings := `
schema_version: "1"
harness_configs:
  claude:
    harness: claude
    env:
      API_KEY: ""
profiles:
  default:
    runtime: docker
`
	srv, _, groveDir := newTestServerWithGrovePath(t, settings)

	// Satisfy ANTHROPIC_API_KEY (from harness) via broker env
	t.Setenv("ANTHROPIC_API_KEY", "broker-ant-key")

	body := `{
		"name": "test-agent-secret-upgrade",
		"id": "agent-uuid-secret",
		"gatherEnv": true,
		"grovePath": "` + groveDir + `",
		"resolvedSecrets": [
			{"name": "API_KEY", "type": "environment", "target": "API_KEY", "value": "secret-api-key", "source": "user"}
		],
		"config": {"template": "claude", "profile": "default"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	// Should NOT return 202 — all env keys are satisfied (API_KEY by secret,
	// ANTHROPIC_API_KEY by broker env). The request proceeds past env-gather.
	if w.Code == http.StatusAccepted {
		t.Fatalf("expected env-gather to pass (not 202), but got 202: %s", w.Body.String())
	}
}

// TestEnvGather_SecretPartialSatisfaction tests that when one required key is
// satisfied by a resolved secret but another is not, the broker returns 202
// with only the unsatisfied key in needs.
func TestEnvGather_SecretPartialSatisfaction(t *testing.T) {
	settings := `
schema_version: "1"
harness_configs:
  claude:
    harness: claude
    env:
      API_KEY: ""
      OTHER_TOKEN: ""
profiles:
  default:
    runtime: docker
`
	srv, _, groveDir := newTestServerWithGrovePath(t, settings)

	body := `{
		"name": "test-agent-partial-secret",
		"id": "agent-uuid-partial",
		"gatherEnv": true,
		"grovePath": "` + groveDir + `",
		"resolvedSecrets": [
			{"name": "API_KEY", "type": "environment", "target": "API_KEY", "value": "secret-api-key", "source": "user"}
		],
		"config": {"template": "claude", "profile": "default"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	// Should return 202 because OTHER_TOKEN is still unsatisfied
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var envReqs EnvRequirementsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &envReqs); err != nil {
		t.Fatal("failed to decode response:", err)
	}

	// API_KEY should be in hubHas (satisfied by secret)
	found := false
	for _, k := range envReqs.HubHas {
		if k == "API_KEY" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected API_KEY in hubHas, got %v", envReqs.HubHas)
	}

	// OTHER_TOKEN should be in needs
	found = false
	for _, k := range envReqs.Needs {
		if k == "OTHER_TOKEN" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected OTHER_TOKEN in needs, got %v", envReqs.Needs)
	}

	// API_KEY should NOT be in needs
	for _, k := range envReqs.Needs {
		if k == "API_KEY" {
			t.Error("API_KEY should not be in needs (satisfied by secret)")
		}
	}
}
