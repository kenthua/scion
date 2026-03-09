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
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEnsureScionGitignore_AddsEntry(t *testing.T) {
	tmpDir := t.TempDir()

	// No .gitignore exists yet
	if err := EnsureScionGitignore(tmpDir); err != nil {
		t.Fatalf("EnsureScionGitignore failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}
	if string(content) != ".scion/\n" {
		t.Errorf("expected '.scion/\\n', got %q", string(content))
	}
}

func TestEnsureScionGitignore_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()

	// Write .gitignore with .scion/ already present
	os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte("node_modules/\n.scion/\n"), 0644)

	if err := EnsureScionGitignore(tmpDir); err != nil {
		t.Fatalf("EnsureScionGitignore failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}
	if string(content) != "node_modules/\n.scion/\n" {
		t.Errorf("expected no change, got %q", string(content))
	}
}

func TestEnsureScionGitignore_AppendsToExisting(t *testing.T) {
	tmpDir := t.TempDir()

	// Write .gitignore without trailing newline
	os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte("node_modules/"), 0644)

	if err := EnsureScionGitignore(tmpDir); err != nil {
		t.Fatalf("EnsureScionGitignore failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}
	if string(content) != "node_modules/\n.scion/\n" {
		t.Errorf("expected 'node_modules/\\n.scion/\\n', got %q", string(content))
	}
}

func TestEnsureScionGitignore_RecognizesVariants(t *testing.T) {
	for _, pattern := range []string{".scion", ".scion/", "/.scion", "/.scion/"} {
		tmpDir := t.TempDir()
		os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(pattern+"\n"), 0644)

		if err := EnsureScionGitignore(tmpDir); err != nil {
			t.Fatalf("EnsureScionGitignore failed for pattern %q: %v", pattern, err)
		}

		content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
		if err != nil {
			t.Fatalf("failed to read .gitignore: %v", err)
		}
		// Should not have added another entry
		if string(content) != pattern+"\n" {
			t.Errorf("for pattern %q: expected no change, got %q", pattern, string(content))
		}
	}
}

func TestInitProject_NonGitCreatesMarkerAndExternalDir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	mockRuntimeDetection(t, "docker")

	// Create a non-git project directory
	projectDir := t.TempDir()
	scionDir := filepath.Join(projectDir, ".scion")

	// Change to the project directory (non-git)
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(projectDir)

	// InitProject with the .scion path as target
	if err := InitProject(scionDir, GetMockHarnesses()); err != nil {
		t.Fatalf("InitProject failed: %v", err)
	}

	// Verify .scion is a marker file (not a directory)
	markerPath := filepath.Join(projectDir, ".scion")
	info, err := os.Stat(markerPath)
	if err != nil {
		t.Fatalf("marker file does not exist: %v", err)
	}
	if info.IsDir() {
		t.Fatal("expected .scion to be a file (marker), but it's a directory")
	}

	// Read the marker and verify content
	marker, err := ReadGroveMarker(markerPath)
	if err != nil {
		t.Fatalf("ReadGroveMarker failed: %v", err)
	}
	if marker.GroveSlug == "" {
		t.Error("marker should have a grove-slug")
	}
	if marker.GroveID == "" {
		t.Error("marker should have a grove-id")
	}

	// Verify external grove directory was created
	externalPath, err := marker.ExternalGrovePath()
	if err != nil {
		t.Fatalf("ExternalGrovePath failed: %v", err)
	}

	// Check standard dirs
	for _, sub := range []string{"templates", "agents"} {
		p := filepath.Join(externalPath, sub)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Errorf("expected %s/ to exist in external grove", sub)
		}
	}

	// Check settings.yaml with workspace_path
	settingsPath := filepath.Join(externalPath, "settings.yaml")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Fatal("expected settings.yaml in external grove")
	}
	data, _ := os.ReadFile(settingsPath)
	if !containsSubstring(string(data), "workspace_path") {
		t.Error("settings.yaml should contain workspace_path")
	}
	if !containsSubstring(string(data), "grove_id") {
		t.Error("settings.yaml should contain grove_id")
	}
}

func TestInitProject_NonGitRejectsOldStyleDir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	mockRuntimeDetection(t, "docker")

	// Create a non-git project with old-style .scion directory
	projectDir := t.TempDir()
	scionDir := filepath.Join(projectDir, ".scion")
	os.MkdirAll(scionDir, 0755)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(projectDir)

	err := InitProject(scionDir, GetMockHarnesses())
	if err == nil {
		t.Fatal("expected error for old-style non-git grove, got nil")
	}
	if !containsSubstring(err.Error(), "outdated") {
		t.Errorf("expected error about outdated format, got: %v", err)
	}
}

func TestInitProject_NonGitIdempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	mockRuntimeDetection(t, "docker")

	projectDir := t.TempDir()
	scionDir := filepath.Join(projectDir, ".scion")

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(projectDir)

	// First init
	if err := InitProject(scionDir, GetMockHarnesses()); err != nil {
		t.Fatalf("first InitProject failed: %v", err)
	}

	// Read marker from first init
	marker1, _ := ReadGroveMarker(filepath.Join(projectDir, ".scion"))

	// Second init should succeed and use existing marker
	if err := InitProject(scionDir, GetMockHarnesses()); err != nil {
		t.Fatalf("second InitProject failed: %v", err)
	}

	// Read marker after second init — should be unchanged
	marker2, _ := ReadGroveMarker(filepath.Join(projectDir, ".scion"))
	if marker1.GroveID != marker2.GroveID {
		t.Errorf("grove-id changed between inits: %q → %q", marker1.GroveID, marker2.GroveID)
	}
}

func TestInitProject_GitCreatesGroveIDAndExternalDir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	mockRuntimeDetection(t, "docker")

	// Create a git repo
	projectDir := filepath.Join(t.TempDir(), "my-git-project")
	os.MkdirAll(projectDir, 0755)
	setupGitRepoDir(t, projectDir)

	scionDir := filepath.Join(projectDir, ".scion")

	// Change to the project directory
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(projectDir)

	if err := InitProject(scionDir, GetMockHarnesses()); err != nil {
		t.Fatalf("InitProject failed: %v", err)
	}

	// Verify .scion is a directory (not a marker file, since it's a git grove)
	info, err := os.Stat(scionDir)
	if err != nil {
		t.Fatalf(".scion does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected .scion to be a directory for git groves")
	}

	// Verify grove-id file was created
	groveID, err := ReadGroveID(scionDir)
	if err != nil {
		t.Fatalf("ReadGroveID failed: %v", err)
	}
	if groveID == "" {
		t.Error("grove-id should not be empty")
	}

	// Verify external agents directory was created
	externalDir, err := GetGitGroveExternalAgentsDir(scionDir)
	if err != nil {
		t.Fatalf("GetGitGroveExternalAgentsDir failed: %v", err)
	}
	if externalDir == "" {
		t.Fatal("external agents dir should not be empty")
	}
	if _, err := os.Stat(externalDir); os.IsNotExist(err) {
		t.Errorf("expected external agents directory to exist at %s", externalDir)
	}

	// Verify templates dir exists in-repo (not external)
	templatesDir := filepath.Join(scionDir, "templates")
	if _, err := os.Stat(templatesDir); os.IsNotExist(err) {
		t.Error("expected templates/ to exist in-repo")
	}

	// Verify agents dir exists in-repo (for worktrees)
	agentsDir := filepath.Join(scionDir, "agents")
	if _, err := os.Stat(agentsDir); os.IsNotExist(err) {
		t.Error("expected agents/ to exist in-repo for worktrees")
	}
}

func TestInitProject_GitIdempotentGroveID(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	mockRuntimeDetection(t, "docker")

	projectDir := filepath.Join(t.TempDir(), "idempotent-project")
	os.MkdirAll(projectDir, 0755)
	setupGitRepoDir(t, projectDir)

	scionDir := filepath.Join(projectDir, ".scion")

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(projectDir)

	// First init
	if err := InitProject(scionDir, GetMockHarnesses()); err != nil {
		t.Fatalf("first InitProject failed: %v", err)
	}
	groveID1, _ := ReadGroveID(scionDir)

	// Second init
	if err := InitProject(scionDir, GetMockHarnesses()); err != nil {
		t.Fatalf("second InitProject failed: %v", err)
	}
	groveID2, _ := ReadGroveID(scionDir)

	if groveID1 != groveID2 {
		t.Errorf("grove-id changed between inits: %q → %q", groveID1, groveID2)
	}
}

// setupGitRepoDir initializes a git repository in the given directory.
func setupGitRepoDir(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "--allow-empty", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
}

func TestInitProject_CreatesEmptyTemplatesDir(t *testing.T) {
	// Create a temporary directory for the project
	tempDir, err := os.MkdirTemp("", "scion-init-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockRuntimeDetection(t, "docker")

	// Run InitProject
	err = InitProject(tempDir, GetMockHarnesses())
	if err != nil {
		t.Fatalf("InitProject failed: %v", err)
	}

	// Verify that templates/ directory exists
	templatesDir := filepath.Join(tempDir, "templates")
	if info, err := os.Stat(templatesDir); err != nil || !info.IsDir() {
		t.Fatalf("Expected templates/ directory to exist at %s", templatesDir)
	}

	// Verify that templates/default does NOT exist (default template lives in global grove only)
	defaultDir := filepath.Join(tempDir, "templates", "default")
	if _, err := os.Stat(defaultDir); !os.IsNotExist(err) {
		t.Errorf("Expected templates/default to NOT exist at project level, but it does at %s", defaultDir)
	}

	// Verify per-harness templates were NOT created
	for _, name := range []string{"gemini", "claude", "opencode", "codex"} {
		perHarnessDir := filepath.Join(tempDir, "templates", name)
		if _, err := os.Stat(perHarnessDir); !os.IsNotExist(err) {
			t.Errorf("Expected per-harness template %s to NOT be created at project level", name)
		}
	}
}
