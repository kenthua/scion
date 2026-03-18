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

//go:build !no_sqlite

package hub

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/GoogleCloudPlatform/scion/pkg/hubclient"
	"github.com/GoogleCloudPlatform/scion/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroveSettings_GetEmpty(t *testing.T) {
	srv, s := testServer(t)
	grove := createTestGroveForSettings(t, s)

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/groves/"+grove.ID+"/settings", nil)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var settings hubclient.GroveSettings
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&settings))
	assert.Empty(t, settings.DefaultTemplate)
	assert.Empty(t, settings.DefaultHarnessConfig)
	assert.Nil(t, settings.TelemetryEnabled)
}

func TestGroveSettings_PutAndGet(t *testing.T) {
	srv, s := testServer(t)
	grove := createTestGroveForSettings(t, s)

	telemetry := true
	putBody := hubclient.GroveSettings{
		DefaultTemplate:      "my-template",
		DefaultHarnessConfig: "claude-default",
		TelemetryEnabled:     &telemetry,
	}

	rec := doRequest(t, srv, http.MethodPut, "/api/v1/groves/"+grove.ID+"/settings", putBody)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var putResp hubclient.GroveSettings
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&putResp))
	assert.Equal(t, "my-template", putResp.DefaultTemplate)
	assert.Equal(t, "claude-default", putResp.DefaultHarnessConfig)
	require.NotNil(t, putResp.TelemetryEnabled)
	assert.True(t, *putResp.TelemetryEnabled)

	// GET should return persisted values
	rec = doRequest(t, srv, http.MethodGet, "/api/v1/groves/"+grove.ID+"/settings", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var getResp hubclient.GroveSettings
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&getResp))
	assert.Equal(t, "my-template", getResp.DefaultTemplate)
	assert.Equal(t, "claude-default", getResp.DefaultHarnessConfig)
	require.NotNil(t, getResp.TelemetryEnabled)
	assert.True(t, *getResp.TelemetryEnabled)
}

func TestGroveSettings_ClearValues(t *testing.T) {
	srv, s := testServer(t)
	grove := createTestGroveForSettings(t, s)

	// Set values first
	telemetry := true
	putBody := hubclient.GroveSettings{
		DefaultTemplate:      "my-template",
		DefaultHarnessConfig: "claude-default",
		TelemetryEnabled:     &telemetry,
	}
	rec := doRequest(t, srv, http.MethodPut, "/api/v1/groves/"+grove.ID+"/settings", putBody)
	require.Equal(t, http.StatusOK, rec.Code)

	// Clear by sending empty values
	clearBody := hubclient.GroveSettings{}
	rec = doRequest(t, srv, http.MethodPut, "/api/v1/groves/"+grove.ID+"/settings", clearBody)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp hubclient.GroveSettings
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Empty(t, resp.DefaultTemplate)
	assert.Empty(t, resp.DefaultHarnessConfig)
	assert.Nil(t, resp.TelemetryEnabled)
}

func TestGroveSettings_DefaultLimits(t *testing.T) {
	srv, s := testServer(t)
	grove := createTestGroveForSettings(t, s)

	putBody := hubclient.GroveSettings{
		DefaultMaxTurns:      100,
		DefaultMaxModelCalls: 500,
		DefaultMaxDuration:   "2h",
		DefaultResources: &hubclient.GroveResourceSpec{
			Requests: &hubclient.GroveResourceList{CPU: "500m", Memory: "1Gi"},
			Limits:   &hubclient.GroveResourceList{CPU: "2", Memory: "4Gi"},
			Disk:     "10Gi",
		},
	}

	rec := doRequest(t, srv, http.MethodPut, "/api/v1/groves/"+grove.ID+"/settings", putBody)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var putResp hubclient.GroveSettings
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&putResp))
	assert.Equal(t, 100, putResp.DefaultMaxTurns)
	assert.Equal(t, 500, putResp.DefaultMaxModelCalls)
	assert.Equal(t, "2h", putResp.DefaultMaxDuration)
	require.NotNil(t, putResp.DefaultResources)
	require.NotNil(t, putResp.DefaultResources.Requests)
	assert.Equal(t, "500m", putResp.DefaultResources.Requests.CPU)
	assert.Equal(t, "1Gi", putResp.DefaultResources.Requests.Memory)
	require.NotNil(t, putResp.DefaultResources.Limits)
	assert.Equal(t, "2", putResp.DefaultResources.Limits.CPU)
	assert.Equal(t, "4Gi", putResp.DefaultResources.Limits.Memory)
	assert.Equal(t, "10Gi", putResp.DefaultResources.Disk)

	// GET should return persisted values
	rec = doRequest(t, srv, http.MethodGet, "/api/v1/groves/"+grove.ID+"/settings", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var getResp hubclient.GroveSettings
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&getResp))
	assert.Equal(t, 100, getResp.DefaultMaxTurns)
	assert.Equal(t, 500, getResp.DefaultMaxModelCalls)
	assert.Equal(t, "2h", getResp.DefaultMaxDuration)
	require.NotNil(t, getResp.DefaultResources)
	assert.Equal(t, "10Gi", getResp.DefaultResources.Disk)
}

func TestGroveSettings_ClearDefaultLimits(t *testing.T) {
	srv, s := testServer(t)
	grove := createTestGroveForSettings(t, s)

	// Set values first
	putBody := hubclient.GroveSettings{
		DefaultMaxTurns:      100,
		DefaultMaxModelCalls: 500,
		DefaultMaxDuration:   "2h",
	}
	rec := doRequest(t, srv, http.MethodPut, "/api/v1/groves/"+grove.ID+"/settings", putBody)
	require.Equal(t, http.StatusOK, rec.Code)

	// Clear by sending zero/empty values
	clearBody := hubclient.GroveSettings{}
	rec = doRequest(t, srv, http.MethodPut, "/api/v1/groves/"+grove.ID+"/settings", clearBody)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp hubclient.GroveSettings
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, 0, resp.DefaultMaxTurns)
	assert.Equal(t, 0, resp.DefaultMaxModelCalls)
	assert.Empty(t, resp.DefaultMaxDuration)
	assert.Nil(t, resp.DefaultResources)
}

func TestGroveSettings_NotFound(t *testing.T) {
	srv, _ := testServer(t)

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/groves/nonexistent/settings", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func createTestGroveForSettings(t *testing.T, s store.Store) *store.Grove {
	t.Helper()
	grove := &store.Grove{
		ID:         "test-grove-settings-" + t.Name(),
		Name:       "Test Grove",
		Slug:       "test-grove-settings",
		Visibility: "private",
	}
	require.NoError(t, s.CreateGrove(t.Context(), grove))
	return grove
}
