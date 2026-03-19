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

package metadata

import (
	"testing"
)

func TestSetupIPTablesRedirect_NoIPTables(t *testing.T) {
	// This test verifies that setupIPTablesRedirect returns an error when
	// iptables is not available (which is the case in most test environments).
	err := setupIPTablesRedirect(18380)
	if err == nil {
		// If it succeeded, we're running in a privileged environment with iptables.
		// Clean up the rule we just created.
		cleanupIPTablesRedirect(18380)
		t.Skip("iptables available in test environment, skipping error path test")
	}
	// Expected: error because iptables is not available
	t.Logf("Expected iptables failure: %v", err)
}

func TestCleanupIPTablesRedirect_NoIPTables(t *testing.T) {
	// Cleanup should be a no-op when iptables is not available
	cleanupIPTablesRedirect(18380)
}

func TestSetupMetadataBlock_NoPrivileges(t *testing.T) {
	// In a non-privileged test environment, both iptables and ip route
	// should fail, and setupMetadataBlock should return an error.
	method, err := setupMetadataBlock()
	if err == nil {
		// If it succeeded, clean up and skip.
		cleanupMetadataBlock(method)
		t.Skip("metadata block succeeded in test environment, skipping error path test")
	}
	if method != blockNone {
		t.Fatalf("expected blockNone on failure, got %v", method)
	}
	t.Logf("Expected metadata block failure: %v", err)
}

func TestCleanupMetadataBlock_Noop(t *testing.T) {
	// Cleanup with blockNone should be a silent no-op
	cleanupMetadataBlock(blockNone)
}
