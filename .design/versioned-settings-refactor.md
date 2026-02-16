# Versioned Settings Refactor: Design & Transition Plan

## Status: Draft (Revised — feedback incorporated)
## Date: 2026-02-16
## Supersedes: `.design/_archive/settings-refactor.md`

---

## 1. Motivation

The current settings system evolved organically and has several structural problems:

1. **Ambiguous grouping.** Settings like `active_profile`, `default_template`, `grove_id`, and `bucket` sit at the top level without a clear domain owner. Some are CLI concerns, some are profile concerns, some are hub concerns.

2. **No schema or versioning.** There is no machine-readable schema for settings. Typos, missing fields, and structural errors are only discovered at runtime (or never). There is no way to determine which features a given settings file supports.

3. **Two disjoint config systems.** The CLI/agent settings (`settings.yaml`) and the server config (`server.yaml`, `GlobalConfig`) use separate loading paths, separate structs, and separate env-var conventions, even though they share concepts like `brokerID`.

4. **Missing feature support.** Upcoming features (interactive mode, max agent duration, max turns, named harness configs) need settings support that doesn't exist in the current flat model.

5. **No deprecation path.** Changing the settings structure would silently break existing users. There is no mechanism to detect legacy vs modern settings, warn about deprecated fields, or guide migration.

6. **Inconsistent field naming.** The current code mixes camelCase koanf tags (`groveId`, `apiKey`, `brokerNickname`) with snake_case tags (`active_profile`, `grove_id`, `local_only`). Some env var overrides (e.g., `SCION_HUB_GROVE_ID`, `SCION_HUB_BROKER_NICKNAME`) do not work because the Koanf key mapping produces snake_case keys that don't match the camelCase struct tags. The versioned settings will standardize on snake_case everywhere.

---

## 2. Target Settings Groups

The new settings structure recognizes these primary domain groups:

### 2.1 `server` (global-only)

Server/broker process configuration. Only valid at the global level (`~/.scion/settings.yaml`), never in grove-level settings.

```yaml
server:
  env: prod                        # deployment environment label (new)
  hub:                             # hub API server settings (when running scion-server)
    port: 9810
    host: "0.0.0.0"
    # public_url is the externally-reachable URL for this Hub server.
    # Passed to agents so they can report status back. Distinct from
    # the hub CLIENT endpoint (Section 2.2) which is where clients connect.
    public_url: "https://hub.example.com"
    read_timeout: 30s
    write_timeout: 60s
    cors:
      enabled: true
      allowed_origins: ["*"]
      allowed_methods: ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"]
      allowed_headers: ["Authorization", "Content-Type", "X-Scion-Broker-Token", "X-Scion-Agent-Token", "X-API-Key"]
      max_age: 3600
    admin_emails: []
  broker:
    enabled: false
    port: 9800
    host: "0.0.0.0"
    read_timeout: 30s
    write_timeout: 120s
    hub_endpoint: ""               # Hub API endpoint for status reporting (when Hub is remote)
    broker_id: ""                  # unique broker identifier (UUID, auto-generated if empty)
    broker_name: ""                # human-readable broker name
    broker_nickname: ""            # human-readable display name (defaults to hostname)
    broker_token: ""               # token received when registering with Hub
    cors:
      enabled: true
      allowed_origins: ["*"]
      allowed_methods: ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"]
      allowed_headers: ["Authorization", "Content-Type", "X-Scion-Broker-Token", "X-API-Key"]
      max_age: 3600
  database:
    driver: sqlite
    url: ""
  auth:
    dev_mode: false
    dev_token: ""
    dev_token_file: ""
    authorized_domains: []
  oauth:
    web:
      google: { client_id: "", client_secret: "" }
      github: { client_id: "", client_secret: "" }
    cli:
      google: { client_id: "", client_secret: "" }
      github: { client_id: "", client_secret: "" }
    device:
      google: { client_id: "", client_secret: "" }
      github: { client_id: "", client_secret: "" }
  storage:
    provider: local
    bucket: ""
    local_path: ""
  secrets:
    backend: local
    gcp_project_id: ""
    gcp_credentials: ""
  log_level: info
  log_format: text
```

**Changes from original draft:**
- `broker_id` moved from server top-level into `server.broker` (where it already lives in the current `RuntimeBrokerConfig` struct).
- `broker_nickname` and `broker_token` moved from `hub` client section (Section 2.2) into `server.broker`. These fields describe this machine's identity as a broker and are inherently machine-scoped (global-only), not per-grove.
- `server.hub.endpoint` renamed to `server.hub.public_url` to distinguish from the hub CLIENT endpoint.
- `server.broker` now includes `read_timeout`, `write_timeout`, and full CORS settings to match the current `RuntimeBrokerConfig` struct.
- CORS `allowed_headers` defaults updated to match actual code (includes `X-Scion-Broker-Token`, `X-Scion-Agent-Token`, `X-API-Key`).
- **Renamed** `server.runtime_broker` → `server.broker` for brevity. The current Go struct `RuntimeBrokerConfig` will be renamed to `BrokerConfig` in Phase 4.

**Rationale:** This consolidates the current `GlobalConfig`/`server.yaml` system into the unified settings file. The separate `server.yaml` continues to work during the transition but the canonical location becomes `settings.yaml` under the `server` key.

### 2.2 `hub` (hub client)

Settings for connecting to a remote Scion Hub as a client. Valid at global or grove level (grove overrides global).

```yaml
hub:
  enabled: true
  endpoint: "https://hub.example.com"   # Hub API URL to connect to (as a client)
  grove_id: ""                           # grove identifier from Hub registration
  local_only: false                      # operate in local-only mode even when Hub is configured
```

**Changes from original draft:**
- `broker_id`, `broker_nickname`, `broker_token` moved to `server.broker` (Section 2.1). Broker identity is per-machine, not per-grove.
- `token` removed. Dev-mode authentication uses `server.auth.dev_token` / `server.auth.dev_token_file` (the same mechanism the server uses). In production, OAuth handles authentication. There is no need for a separate hub client token field.
- `api_key` removed. There is no current or planned support for API key authentication with the Hub.
- `last_synced_at` moved to `state.yaml` (see Section 2.9). Runtime-managed state should not live in the settings file.

**Note:** The `hub.endpoint` field is the URL this CLI/broker connects to as a Hub client. This is distinct from `server.hub.public_url`, which is the public-facing URL of a Hub server process (used for agent callback URLs). They often have the same value but serve different roles.

### 2.3 `cli`

Controls CLI behavior. Valid at global or grove level.

```yaml
cli:
  autohelp: true
  interactive_disabled: false      # new: disable interactive prompts
```

### 2.4 `runtimes` (named map)

Container runtime definitions. Valid at global or grove level.

The name of a runtime entry is an arbitrary label chosen by the user — it does **not** need to match the `type` field. This allows defining multiple runtimes of the same type with different configurations (e.g., `staging-docker` and `prod-docker` both with `type: docker`).

```yaml
runtimes:
  docker:                          # name matches type (conventional default)
    type: docker
    host: ""
    env: {}
    sync: ""
  container:
    type: container
    tmux: true
  kubernetes:
    type: kubernetes
    context: ""
    namespace: ""
  staging-docker:                  # name differs from type
    type: docker
    host: "tcp://staging.example.com:2376"
    env:
      DOCKER_TLS_VERIFY: "1"
```

**Change from current:** An explicit `type` field is added to each runtime. This was implicit before (the runtime name *was* the type). With the `type` field, users can define multiple runtimes of the same type with different configurations. The legacy names-as-types behavior is preserved for backward compatibility (if `type` is absent, the name is used as the type).

### 2.5 `harness_configs` (named map)

Named harness configurations. This replaces the current `harnesses` map. Multiple configs can exist for the same harness type.

```yaml
harness_configs:
  gemini:                          # default config for gemini harness
    harness: gemini
    image: "us-central1-docker.pkg.dev/.../scion-gemini:latest"
    user: scion
    model: ""
    args: []
    env: {}
    volumes: []
    auth_selected_type: ""         # e.g., "gemini-api-key", "vertex-ai", "oauth-personal"
  claude:                          # default config for claude harness
    harness: claude
    image: "us-central1-docker.pkg.dev/.../scion-claude:latest"
    user: scion
    model: ""
    args: []
    env: {}
    volumes: []
  opencode:                        # default config for opencode harness
    harness: opencode
    image: "us-central1-docker.pkg.dev/.../scion-opencode:latest"
    user: scion
  codex:                           # default config for codex harness
    harness: codex
    image: "us-central1-docker.pkg.dev/.../scion-codex:latest"
    user: scion
  gemini-high-security:            # named variant (arbitrary name)
    harness: gemini
    image: "us-central1-docker.pkg.dev/.../scion-gemini:hardened"
    user: scion
    model: "gemini-2.5-pro"
    args: ["--sandbox=strict"]
    env:
      GEMINI_SAFETY: "maximum"
```

**Change from current:** The `harnesses` map only allowed one entry per harness type (keyed by harness name). The new `harness_configs` map is keyed by an arbitrary config name, with an explicit `harness` field specifying the harness type. There is a convention that each harness has a "default" config whose name matches the harness (e.g., config named `gemini` with `harness: gemini`).

**New fields:** `model` and `args` are new additions to harness configs. The current `HarnessConfig` struct does not have these — they exist only in the agent-level `ScionConfig`. Adding them to `harness_configs` allows setting model and arguments as defaults at the settings level.

### 2.6 `profiles` (named map)

Named environment profiles. Valid at global or grove level.

```yaml
profiles:
  local:
    runtime: container
    default_template: gemini
    default_harness_config: gemini  # which harness_config to use by default
    tmux: true
    env: {}
    volumes: []
    resources: null
    harness_overrides:              # per-harness-config overrides
      gemini:
        image: "custom:dev"
  remote:
    runtime: kubernetes
    default_template: gemini
    default_harness_config: gemini
    tmux: false
```

**Change from current:** `default_template` and `default_harness_config` are added to profiles. The top-level `default_template` and `active_profile` remain for backward compatibility but profiles can now be self-describing.

### 2.7 `agent` (template configuration)

Agent/template-level settings. These live in `scion-agent.yaml` within template directories, not in `settings.yaml`.

```yaml
# In .scion/templates/<name>/scion-agent.yaml
harness_config: gemini             # references a key in harness_configs
env: {}
volumes: []
resources:
  requests:
    cpu: "500m"
    memory: "512Mi"
  limits:
    cpu: "2"
    memory: "2Gi"
  disk: "10Gi"
max_turns: 50                      # new
max_duration: "2h"                 # new
services:                          # sidecar services
  - name: browser
    command: ["chromium", "--headless"]
    restart: on-failure
    ready_check:
      type: tcp
      target: "localhost:9222"
      timeout: "10s"
```

**Change from original draft:** `harness` renamed to `harness_config` to clearly indicate that this field references a named harness configuration (a key in the `harness_configs` map), not a raw harness type. This avoids ambiguity when users define custom-named configs like `gemini-high-security`.

**Note:** For backward compatibility, if `harness_config` is not present but `harness` is, the value is treated as both the harness type and the config name (matching the legacy behavior where harness name = harness type = config key).

### 2.8 Top-level metadata

```yaml
$schema: "https://scion.dev/schemas/settings/v1.json"
schema_version: "1"

active_profile: local
default_template: gemini           # preserved for backward compatibility
```

### 2.9 `state.yaml` (grove-level runtime state)

Runtime-managed state that should not be mixed with user-editable configuration lives in a separate `state.yaml` file. This file is present only in non-global groves (i.e., project-level `.scion/` directories), not in `~/.scion/`.

```yaml
# .scion/state.yaml (managed by scion, not user-edited)
last_synced_at: "2026-02-16T10:30:00Z"   # RFC3339 timestamp of last successful Hub sync
```

**Rationale:** Mixing runtime state with configuration complicates validation, makes `settings.yaml` harder to edit safely, and creates spurious diffs in version-controlled `.scion/` directories. The `state.yaml` file is explicitly excluded from schema validation and is managed programmatically.

**Migration:** The current `hub.last_synced_at` field in `settings.yaml` will be read during migration and relocated to `state.yaml`. The legacy adapter handles this transparently.

---

## 3. JSON Schema

### 3.1 Schema Location and Naming

Schemas are stored in the repository at `pkg/config/schemas/` and embedded into the binary.

```
pkg/config/schemas/
  settings-v1.schema.json          # settings.yaml schema
  agent-v1.schema.json             # scion-agent.yaml schema
```

### 3.2 Schema Standard

JSON Schema Draft 2020-12 (`https://json-schema.org/draft/2020-12/schema`).

### 3.3 Custom Annotations

Each schema property that can be set via environment variable includes:

```json
{
  "x-env-var": "SCION_HUB_ENDPOINT",
  "x-env-var-prefix": "SCION_"
}
```

Each schema property includes scope metadata:

```json
{
  "x-scope": "global",          // "global" = global-only, "any" = global or grove
  "x-since": "1",               // schema version that introduced this field
  "x-deprecated-by": "2"        // schema version that deprecated this field (if applicable)
}
```

### 3.4 Versioning Strategy

- The schema version is a simple monotonic integer (`"1"`, `"2"`, `"3"`, ...).
- The `schema_version` field in settings.yaml declares which schema version the file conforms to.
- The binary embeds all supported schema versions and validates against the declared version.
- Feature gates can check `schema_version >= N` to determine if a feature's settings are available.

### 3.5 Schema Sketch (v1)

main settings schema kept in './settings-schema.md'

A separate agent schema (`agent-v1.schema.json`) will be defined for `scion-agent.yaml` files. Its structure mirrors the existing `ScionConfig` with additions:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://scion.dev/schemas/agent/v1.json",
  "title": "Scion Agent Configuration",
  "type": "object",
  "properties": {
    "schema_version": { "type": "string", "const": "1" },
    "harness_config": {
      "type": "string",
      "description": "Name of the harness config entry to use (key in harness_configs map). Falls back to 'harness' field for legacy compat."
    },
    "harness": {
      "type": "string",
      "description": "Legacy: harness type name. Deprecated in favor of harness_config.",
      "x-deprecated-by": "1"
    },
    "env": { "type": "object", "additionalProperties": { "type": "string" } },
    "volumes": { "type": "array", "items": { "$ref": "#/$defs/volumeMount" } },
    "resources": { "$ref": "#/$defs/resourceSpec" },
    "max_turns": {
      "type": "integer",
      "minimum": 1,
      "description": "Maximum number of LLM turns before the agent is stopped.",
      "x-since": "1"
    },
    "max_duration": {
      "type": "string",
      "pattern": "^[0-9]+(s|m|h)$",
      "description": "Maximum wall-clock duration before the agent is stopped (e.g., '2h', '30m').",
      "x-since": "1"
    },
    "services": {
      "type": "array",
      "items": { "$ref": "#/$defs/serviceSpec" }
    },
    "image": { "type": "string", "description": "Override container image." },
    "user": { "type": "string", "description": "Override unix user." },
    "model": { "type": "string", "description": "LLM model identifier." },
    "args": { "type": "array", "items": { "type": "string" } },
    "detached": { "type": "boolean", "description": "Run agent in detached (background) mode. Defaults to true." },
    "config_dir": { "type": "string", "description": "Agent config directory." },
    "command_args": { "type": "array", "items": { "type": "string" }, "description": "Additional command arguments." },
    "gemini": {
      "type": "object",
      "description": "Gemini-specific configuration.",
      "properties": {
        "auth_selected_type": { "type": "string" }
      }
    },
    "kubernetes": {
      "type": "object",
      "description": "Kubernetes-specific configuration.",
      "properties": {
        "context": { "type": "string" },
        "namespace": { "type": "string" },
        "runtime_class_name": { "type": "string" },
        "service_account_name": { "type": "string" },
        "resources": {
          "type": "object",
          "properties": {
            "requests": { "type": "object", "additionalProperties": { "type": "string" } },
            "limits": { "type": "object", "additionalProperties": { "type": "string" } }
          }
        }
      }
    }
  }
}
```

---

## 4. Environment Variable Mapping

### 4.1 Convention

All environment variables use the `SCION_` prefix. Nesting is represented by underscores. The schema's `x-env-var` annotation is the canonical source of truth.

**Important:** The new versioned settings will use snake_case consistently for all koanf struct tags. This fixes the current inconsistency where some tags use camelCase (`groveId`, `apiKey`, `brokerNickname`) while env var mappings produce snake_case keys, causing silent mismatches.

### 4.2 Settings Env Vars

These override values in `settings.yaml`. Prefix: `SCION_`.

| Settings Path | Env Var | Type |
|---|---|---|
| `active_profile` | `SCION_ACTIVE_PROFILE` | string |
| `default_template` | `SCION_DEFAULT_TEMPLATE` | string |
| `grove_id` | `SCION_GROVE_ID` | string |
| `hub.enabled` | `SCION_HUB_ENABLED` | bool |
| `hub.endpoint` | `SCION_HUB_ENDPOINT` | string |
| `hub.grove_id` | `SCION_HUB_GROVE_ID` | string |
| `hub.local_only` | `SCION_HUB_LOCAL_ONLY` | bool |
| `cli.autohelp` | `SCION_CLI_AUTOHELP` | bool |
| `cli.interactive_disabled` | `SCION_CLI_INTERACTIVE_DISABLED` | bool |

**Note:** `cli.*` and `hub.grove_id` env var mappings are new — they don't work in the current (legacy) Koanf loader due to missing key transformations. The versioned loader must implement these.

### 4.3 Server Env Vars

These override values in `server.yaml` / `settings.yaml` `server` section. Prefix: `SCION_SERVER_`.

| Settings Path | Env Var | Type |
|---|---|---|
| **Hub Server** | | |
| `server.hub.port` | `SCION_SERVER_HUB_PORT` | int |
| `server.hub.host` | `SCION_SERVER_HUB_HOST` | string |
| `server.hub.public_url` | `SCION_SERVER_HUB_ENDPOINT` | string |
| `server.hub.read_timeout` | `SCION_SERVER_HUB_READTIMEOUT` | duration |
| `server.hub.write_timeout` | `SCION_SERVER_HUB_WRITETIMEOUT` | duration |
| `server.hub.cors_enabled` | `SCION_SERVER_HUB_CORSENABLED` | bool |
| `server.hub.admin_emails` | `SCION_SERVER_HUB_ADMINEMAIL` | string (CSV) |
| **Broker** | | |
| `server.broker.enabled` | `SCION_SERVER_BROKER_ENABLED` | bool |
| `server.broker.port` | `SCION_SERVER_BROKER_PORT` | int |
| `server.broker.host` | `SCION_SERVER_BROKER_HOST` | string |
| `server.broker.read_timeout` | `SCION_SERVER_BROKER_READTIMEOUT` | duration |
| `server.broker.write_timeout` | `SCION_SERVER_BROKER_WRITETIMEOUT` | duration |
| `server.broker.hub_endpoint` | `SCION_SERVER_BROKER_HUBENDPOINT` | string |
| `server.broker.broker_id` | `SCION_SERVER_BROKER_BROKERID` | string |
| `server.broker.broker_name` | `SCION_SERVER_BROKER_BROKERNAME` | string |
| `server.broker.broker_nickname` | `SCION_SERVER_BROKER_BROKERNICKNAME` | string |
| `server.broker.broker_token` | `SCION_SERVER_BROKER_BROKERTOKEN` | string |
| **Database** | | |
| `server.database.driver` | `SCION_SERVER_DATABASE_DRIVER` | string |
| `server.database.url` | `SCION_SERVER_DATABASE_URL` | string |
| **Auth** | | |
| `server.auth.dev_mode` | `SCION_SERVER_AUTH_DEVMODE` | bool |
| `server.auth.dev_token` | `SCION_SERVER_AUTH_DEVTOKEN` | string |
| `server.auth.dev_token_file` | `SCION_SERVER_AUTH_DEVTOKENFILE` | string |
| `server.auth.authorized_domains` | `SCION_SERVER_AUTH_AUTHORIZEDDOMAINS` | string (CSV) |
| **OAuth — Web** | | |
| `server.oauth.web.google.client_id` | `SCION_SERVER_OAUTH_WEB_GOOGLE_CLIENTID` | string |
| `server.oauth.web.google.client_secret` | `SCION_SERVER_OAUTH_WEB_GOOGLE_CLIENTSECRET` | string |
| `server.oauth.web.github.client_id` | `SCION_SERVER_OAUTH_WEB_GITHUB_CLIENTID` | string |
| `server.oauth.web.github.client_secret` | `SCION_SERVER_OAUTH_WEB_GITHUB_CLIENTSECRET` | string |
| **OAuth — CLI** | | |
| `server.oauth.cli.google.client_id` | `SCION_SERVER_OAUTH_CLI_GOOGLE_CLIENTID` | string |
| `server.oauth.cli.google.client_secret` | `SCION_SERVER_OAUTH_CLI_GOOGLE_CLIENTSECRET` | string |
| `server.oauth.cli.github.client_id` | `SCION_SERVER_OAUTH_CLI_GITHUB_CLIENTID` | string |
| `server.oauth.cli.github.client_secret` | `SCION_SERVER_OAUTH_CLI_GITHUB_CLIENTSECRET` | string |
| **OAuth — Device** | | |
| `server.oauth.device.google.client_id` | `SCION_SERVER_OAUTH_DEVICE_GOOGLE_CLIENTID` | string |
| `server.oauth.device.google.client_secret` | `SCION_SERVER_OAUTH_DEVICE_GOOGLE_CLIENTSECRET` | string |
| `server.oauth.device.github.client_id` | `SCION_SERVER_OAUTH_DEVICE_GITHUB_CLIENTID` | string |
| `server.oauth.device.github.client_secret` | `SCION_SERVER_OAUTH_DEVICE_GITHUB_CLIENTSECRET` | string |
| **Storage** | | |
| `server.storage.provider` | `SCION_SERVER_STORAGE_PROVIDER` | string |
| `server.storage.bucket` | `SCION_SERVER_STORAGE_BUCKET` | string |
| `server.storage.local_path` | `SCION_SERVER_STORAGE_LOCALPATH` | string |
| **Secrets** | | |
| `server.secrets.backend` | `SCION_SERVER_SECRETS_BACKEND` | string |
| `server.secrets.gcp_project_id` | `SCION_SERVER_SECRETS_GCPPROJECTID` | string |
| `server.secrets.gcp_credentials` | `SCION_SERVER_SECRETS_GCPCREDENTIALS` | string |
| **Logging** | | |
| `server.log_level` | `SCION_SERVER_LOG_LEVEL` | string |
| `server.log_format` | `SCION_SERVER_LOG_FORMAT` | string |

**Note on CORS env vars:** The current `envKeyToConfigKey()` function in `hub_config.go` does not have camelCase mappings for CORS-related fields (`corsEnabled`, `corsAllowedOrigins`, etc.). This means CORS settings cannot currently be overridden via environment variables. The versioned settings will switch to snake_case koanf tags throughout, which resolves this issue (Phase 4 deliverable).

### 4.4 Agent Container Env Vars (runtime-injected)

These are environment variables injected into agent containers by the runtime/broker at startup. They are **not** settings — they are set programmatically and are documented here for completeness.

| Env Var | Set By | Description |
|---|---|---|
| `SCION_AGENT_NAME` | `agent/run.go`, `harness/generic.go` | Agent display name |
| `SCION_AGENT_ID` | `runtimebroker/handlers.go` | Hub UUID for the agent (hosted mode) |
| `SCION_TEMPLATE_NAME` | `agent/run.go` | Template used to create the agent |
| `SCION_BROKER_NAME` | `agent/run.go`, `runtimebroker/handlers.go` | Broker name (defaults to "local") |
| `SCION_CREATOR` | `agent/run.go`, `runtimebroker/handlers.go` | User who created the agent (OS user or Hub email) |
| `SCION_HOST_UID` | `runtime/common.go` | Host user ID (for file ownership mapping) |
| `SCION_HOST_GID` | `runtime/common.go` | Host group ID (for file ownership mapping) |
| `SCION_HUB_ENDPOINT` | `runtimebroker/handlers.go` | Hub API endpoint for agent status reporting (hosted mode). Standardized from former `SCION_HUB_URL`. |
| `SCION_HUB_TOKEN` | `runtimebroker/handlers.go` | Hub auth token for agent callbacks (hosted mode) |
| `SCION_HOOKS_DIR` | (user-set or default) | Override path for agent lifecycle hooks directory |
| `SCION_GRACE_PERIOD` | (user-set) | Override container shutdown grace period |
| `SCION_MODEL` | Harness-specific | Model identifier used by the harness |
| `SCION_HARNESS` | Harness-specific | Harness type identifier |

**Note on `SCION_HUB_URL` → `SCION_HUB_ENDPOINT` rename:** The former `SCION_HUB_URL` injected into agent containers serves the same purpose as the host-side `SCION_HUB_ENDPOINT` settings override — both identify the Hub API endpoint. The name is standardized to `SCION_HUB_ENDPOINT` everywhere. Code changes required: `runtimebroker/handlers.go` (injection), `sciontool/hub/client.go` (reading), and associated tests.

### 4.5 Utility / Debug Env Vars

These are standalone environment variables used by the CLI and server binaries for debugging and operational purposes. They are not part of the settings schema.

| Env Var | Description |
|---|---|
| `SCION_DEBUG` | Enable debug logging when set to "1" |
| `SCION_LOG_LEVEL` | Override log level at runtime ("debug", "info", etc.) |
| `SCION_LOG_GCP` | Enable GCP Cloud Logging when set to "true" |
| `SCION_CLOUD_LOGGING` | Enable Cloud Logging integration |
| `SCION_CLOUD_LOGGING_LOG_ID` | Log ID for Cloud Logging |
| `SCION_GCP_PROJECT_ID` | GCP project ID (for Cloud Logging; priority over `GOOGLE_CLOUD_PROJECT`) |
| `SCION_HEADLESS` | Force headless mode when set to "1" (skips browser-open operations) |
| `SCION_GIT_BINARY` | Override path to the git binary |
| `SCION_DEV_TOKEN` | Dev auth token for Hub/Broker API client authentication |
| `SCION_DEV_TOKEN_FILE` | Path to file containing dev auth token (fallback: `~/.scion/dev-token`) |
| `SCION_HUB_STORAGE_BUCKET` | Override Hub storage bucket (used in `cmd/server.go`) |

### 4.5.1 Web Frontend Env Vars

These are environment variables used by the web frontend server (`web/src/server/`). They are not part of the settings schema.

| Env Var | Description |
|---|---|
| `SCION_WEB_HUB_API_URL` | Hub API endpoint for the web server's reverse proxy. Defaults to `http://localhost:9810`. Renamed from former `HUB_API_URL` to follow the `SCION_` prefix convention. |

**Note on `HUB_API_URL` rename:** The web server uses this env var to configure its proxy to the Hub API. It is being renamed to `SCION_WEB_HUB_API_URL` to follow the `SCION_` prefix convention and to clarify that this is a web-server-specific setting, distinct from `SCION_HUB_ENDPOINT` (which is the client/agent-side Hub API endpoint). Code change required: `web/src/server/config.ts`.

### 4.6 LLM Provider Env Vars (pass-through)

These are discovered by harness implementations and passed into agent containers. They are not part of the settings schema but affect agent behavior.

| Env Var | Harness | Description |
|---|---|---|
| `GEMINI_API_KEY` | gemini | Gemini API key |
| `GOOGLE_API_KEY` | gemini | Alternative Google API key |
| `GOOGLE_APPLICATION_CREDENTIALS` | gemini | Path to GCP service account JSON |
| `GOOGLE_CLOUD_PROJECT` / `GCP_PROJECT` | gemini | GCP project ID |
| `GOOGLE_CLOUD_LOCATION` | gemini | GCP location for Vertex AI |
| `VERTEX_API_KEY` | gemini | Vertex AI API key |
| `ANTHROPIC_API_KEY` | claude, opencode, generic | Anthropic API key |
| `OPENAI_API_KEY` | opencode, codex, generic | OpenAI API key |
| `CODEX_API_KEY` | codex | Codex-specific API key |

---

## 5. Detection & Transition Strategy

### 5.1 How Legacy vs Versioned Settings Are Detected

```
if file contains "schema_version" key:
    → versioned settings: validate against declared schema, use new loader
else if file contains top-level "harnesses" key:
    → legacy settings (current format): load via legacy path, emit deprecation warning
else if file is empty or missing:
    → no settings: use embedded defaults (versioned format)
```

### 5.2 Legacy Compatibility Layer

A `LegacySettingsAdapter` converts legacy `Settings` into the new versioned structure:

```go
func AdaptLegacySettings(legacy *LegacySettings) (*VersionedSettings, []string) {
    // Returns adapted settings + list of deprecation warnings
    // Mapping:
    //   legacy.Harnesses → versioned.HarnessConfigs (name = harness type, harness = name)
    //   legacy.Bucket → versioned.Server.Storage (unified bucket; see Section 8.7)
    //   legacy.GroveID → preserved as-is
    //   legacy.DefaultTemplate → preserved as-is
    //   legacy.Hub.Token → dropped (dev auth uses server.auth.dev_token)
    //   legacy.Hub.BrokerID → moved to server.broker.broker_id (with warning)
    //   legacy.Hub.BrokerNickname → moved to server.broker.broker_nickname
    //   legacy.Hub.BrokerToken → moved to server.broker.broker_token
    //   legacy.Hub.LastSyncedAt → moved to state.yaml (grove-level)
    //   All other fields map 1:1
}
```

### 5.3 Deprecation Warning Format

```
WARNING: Legacy settings format detected in /path/to/settings.yaml
  The following fields are deprecated and will be removed in a future version:
    - "harnesses" → use "harness_configs" with explicit "harness" field
    - "bucket" → consolidated into "server.storage" (run 'scion config migrate')
    - "hub.token" → dev auth uses "server.auth.dev_token" / SCION_DEV_TOKEN
    - "hub.last_synced_at" → moved to state.yaml (grove-level)
    - "hub.broker_id", "hub.broker_nickname", "hub.broker_token"
        → moved to "server.broker" (global settings only)
  Run 'scion config migrate' to automatically update your settings.
```

---

## 6. Phased Implementation Plan

### Phase 1: Schema Foundation ✅ COMPLETE

**Goal:** Introduce the JSON Schema, versioned settings struct, and detection/validation infrastructure without changing any runtime behavior.

**Deliverables:**
1. ✅ Create `pkg/config/schemas/settings-v1.schema.json` (the full schema from Section 3.5).
2. ✅ Create `pkg/config/schemas/agent-v1.schema.json`.
3. ✅ Embed schemas via `//go:embed` in a new `pkg/config/schema.go`.
4. ✅ Implement `DetectSettingsFormat(data []byte) (version string, isLegacy bool)` — inspects a settings file to determine if it's versioned or legacy.
5. ✅ Implement `ValidateSettings(data []byte, schemaVersion string) []ValidationError` — validates a settings file against its declared schema using an embedded JSON Schema validator.
6. ✅ Add a `scion config validate` command that validates the current effective settings and reports errors.
7. ✅ Write tests for schema validation with valid, invalid, and legacy input.

**No behavior changes.** Existing settings loading continues to use the legacy path.

**Implementation notes:**
- Uses `github.com/santhosh-tekuri/jsonschema/v6` for JSON Schema Draft 2020-12 validation.
- Also implements `ValidateAgentConfig()` for agent schema validation and `GetSettingsSchemaJSON()`/`GetAgentSchemaJSON()` for schema retrieval.
- 33 tests in `pkg/config/schema_test.go` covering detection, validation, and edge cases.

### Phase 2: New Settings Structs & Loader

**Goal:** Implement the new Go structs and a parallel loading path that can load versioned settings files.

**Deliverables:**
1. Define `VersionedSettings` struct in `pkg/config/settings_v1.go` with all new groups (`Server`, `Hub`, `CLI`, `Runtimes`, `HarnessConfigs`, `Profiles`). **Use snake_case koanf tags consistently.**
2. Define `HarnessConfigEntry` struct (the `harness_configs` value type with its explicit `harness` field, plus new `model` and `args` fields).
3. Implement `LoadVersionedSettings(grovePath string) (*VersionedSettings, error)` using Koanf, loading with the same hierarchy (defaults → global → grove → env vars).
4. Implement `AdaptLegacySettings(legacy *Settings) (*VersionedSettings, []string)` that converts the current `Settings` struct to `VersionedSettings`, returning deprecation warnings.
5. Create a unified `LoadEffectiveSettings(grovePath string) (*VersionedSettings, []string, error)` that:
   - Detects format.
   - If versioned: validates and loads via the new path.
   - If legacy: loads via old path, adapts, emits warnings.
6. Update `pkg/config/embeds/default_settings.yaml` to use the versioned format (with `schema_version: "1"`).
7. Write comprehensive tests for both loading paths and the adapter.

**No consumer changes yet.** All existing code still uses the legacy `Settings` struct. The new loader exists but is not wired in.

### Phase 3: Consumer Migration — Core Resolution

**Goal:** Wire the new settings into the core resolution and provisioning paths.

**Deliverables:**
1. Add `ResolveHarnessConfig(profileName, harnessConfigName string) (HarnessConfigEntry, error)` to `VersionedSettings` — replaces `ResolveHarness` with support for named configs.
2. Add `ResolveRuntime(profileName string) (RuntimeConfig, string, error)` to `VersionedSettings` — same semantics, now uses `type` field.
3. Update `pkg/agent/provision.go` to accept `*VersionedSettings`. The function receives a `*VersionedSettings` and uses `ResolveHarnessConfig` instead of `ResolveHarness`.
4. Update `pkg/agent/run.go` to use `*VersionedSettings` for image, user, tmux resolution.
5. Update `cmd/create.go`, `cmd/start.go`, and other commands to call `LoadEffectiveSettings` and pass the result through.
6. Introduce a `--harness-config` flag to `scion create` (in addition to existing `--harness`) to select a named harness config.
7. Wire deprecation warnings to stderr output when legacy settings are detected.
8. Test that existing settings files (legacy format) produce identical behavior.

### Phase 4: Server Config Consolidation

**Goal:** Merge `server.yaml` / `GlobalConfig` into the unified settings under the `server` key.

**Deliverables:**
1. Update `LoadGlobalConfig` to check for `server` key in `settings.yaml` first, falling back to `server.yaml` for backward compatibility.
2. Add `ServerConfig` struct (mirrors current `GlobalConfig`) to `VersionedSettings`. Rename `RuntimeBrokerConfig` → `BrokerConfig`, update koanf tags to `broker` (from `runtimeBroker`/`runtime_broker`).
3. Map `SCION_SERVER_*` env vars to `server.*` paths in the unified Koanf loader. Support both `SCION_SERVER_RUNTIMEBROKER_*` (legacy) and `SCION_SERVER_BROKER_*` (new) during transition.
4. When both `server.yaml` and `settings.yaml.server` exist, emit a warning and prefer `settings.yaml`.
5. Add `scion config migrate --server` to merge `server.yaml` into `settings.yaml`.
6. Update `scion server` and `scion broker` commands to read from the unified config.
7. Document that `server.yaml` is deprecated in favor of `settings.yaml` `server` section.
8. Switch all `GlobalConfig` koanf tags to snake_case. This fixes CORS env var mappings and aligns with the versioned settings convention.
9. Implement `state.yaml` read/write logic for grove-level runtime state. Migrate `hub.last_synced_at` from `settings.yaml` to `state.yaml` during the migration step.
10. Remove `hub.token` and `hub.api_key` from settings loading. Update hub client auth to read dev token from `server.auth.dev_token` / `SCION_DEV_TOKEN` / `~/.scion/dev-token` (already partially implemented). In production, OAuth handles auth.

### Phase 5: New Feature Gates & Env Var Standardization

**Goal:** Implement features gated on versioned settings and standardize env var naming.

**Deliverables:**
1. **`max_turns`**: In the agent runner, check `scionConfig.MaxTurns`. Only available when `schema_version >= 1` in the agent template. If the agent's harness supports turn counting (requires harness-level support), enforce the limit by sending a stop signal.
2. **`max_duration`**: In the agent runner, start a timer based on `scionConfig.MaxDuration`. Terminate the agent container after the duration elapses. Only available when `schema_version >= 1`.
3. **`cli.interactive_disabled`**: Check this setting in interactive prompts (attach, confirmations). When `true`, skip prompts and use defaults or fail with an error.
4. **Named harness configs**: With `harness_configs` fully wired, users can create agents with `scion create --harness-config gemini-high-security myagent`.
5. **Runtime type field**: Runtimes with explicit `type` fields resolve correctly through the factory.
6. **Standardize `SCION_HUB_URL` → `SCION_HUB_ENDPOINT`**: Update `runtimebroker/handlers.go` to inject `SCION_HUB_ENDPOINT` instead of `SCION_HUB_URL`. Update `sciontool/hub/client.go` to read `SCION_HUB_ENDPOINT`. Support reading the legacy `SCION_HUB_URL` as fallback during transition.
7. **Rename `HUB_API_URL` → `SCION_WEB_HUB_API_URL`**: Update `web/src/server/config.ts` to read `SCION_WEB_HUB_API_URL` (falling back to `HUB_API_URL` for backward compat). This aligns the web server env var with the `SCION_` prefix convention.

### Phase 6: Migration Tooling & Cleanup

**Goal:** Provide automated migration and remove legacy code paths.

**Deliverables:**
1. Implement `scion config migrate` command:
   - Reads legacy settings file.
   - Produces versioned settings file.
   - Backs up the original as `settings.yaml.bak`.
   - Reports changes made.
2. Implement `scion config migrate --server` to fold `server.yaml` into `settings.yaml`.
3. Implement `scion config migrate --dry-run` for preview.
4. Update documentation (`docs-site/`) with new settings reference.
5. After a release cycle, remove the `LegacySettingsAdapter` and legacy loading path (Phase 2 code), making versioned settings the only supported format.

### Phase 7: Unified Storage Consolidation

**Goal:** Consolidate all GCS storage usage (templates, workspaces, volumes) into a single bucket with path-based namespacing.

**Context:** Currently, `Settings.Bucket` (client-side workspace persistence) and `GlobalConfig.Storage` (server-side template/asset storage) are separate configs pointing at potentially different buckets. The desired end state is a single bucket with all storage namespaced under different path prefixes.

**Deliverables:**
1. Design the unified path layout within a single GCS bucket:
   ```
   <bucket>/
     templates/               # template assets (currently server.storage)
     workspaces/<grove>/<agent>/  # agent workspace persistence (currently Settings.Bucket)
     volumes/                 # volume data
   ```
2. Extend `server.storage` to support the unified layout with configurable path prefixes.
3. Refactor workspace persistence code (`pkg/agent/`) to use `server.storage` instead of `Settings.Bucket`.
4. Refactor template storage code (`pkg/hub/`) to use path-prefixed storage within the same bucket.
5. Deprecate the top-level `bucket` setting. The migration adapter maps `legacy.Bucket` into the unified `server.storage` config.
6. Update GCS client code to support path-prefix-based namespacing within a single bucket.

**Note:** This is a more substantial code refactor than other phases. It affects the storage layer across both client and server code paths. The settings structure should be defined in earlier phases even if the underlying code refactor happens here.

---

## 7. File Layout Changes

### Before (legacy)
```
~/.scion/
  settings.yaml              # flat Settings struct
  server.yaml                # separate GlobalConfig
.scion/
  settings.yaml              # grove-level Settings
  templates/
    gemini/
      scion-agent.json       # agent config (no schema)
```

### After (versioned)
```
~/.scion/
  settings.yaml              # VersionedSettings with schema_version, includes server section
.scion/
  settings.yaml              # grove-level VersionedSettings (no server section)
  state.yaml                 # grove-level runtime state (last_synced_at, etc.)
  templates/
    gemini/
      scion-agent.yaml       # agent config with schema_version
```

---

## 8. Key Decisions

### 8.1 Why not separate files per group?

A single `settings.yaml` with clear top-level groups is simpler to manage than multiple files. The Koanf merge hierarchy (defaults → global → grove → env) already handles layering. Splitting into `hub.yaml`, `runtimes.yaml`, etc. would multiply the number of files users must manage and complicate the merge logic.

### 8.2 Why absorb `server.yaml`?

The server config shares infrastructure with settings (Koanf loading, env vars, YAML format). Having two separate files with two separate loading paths is a maintenance burden. The `server` key is scoped to global-only, so there is no ambiguity about where it can appear.

### 8.3 Why `harness_configs` instead of extending `harnesses`?

The current `harnesses` map is keyed by harness type name, enforcing a 1:1 relationship between name and type. The new `harness_configs` map breaks this constraint, allowing multiple configurations for the same harness type. This is a semantic change that warrants a new key name to avoid confusion during the transition.

### 8.4 Why integer versioning?

Semantic versioning (major.minor.patch) is overkill for a settings schema. A simple monotonic integer is sufficient. Each increment represents a set of additive changes. The schema itself uses `x-since` annotations to track which version introduced each field, and `x-deprecated-by` to track removals.

### 8.5 Why JSON Schema instead of Go-only validation?

JSON Schema is language-neutral and can be used by IDEs (via `$schema` in YAML) for autocompletion and validation. It serves as documentation, validation specification, and tooling integration in one artifact. Go code validates against it at runtime using an embedded validator library.

### 8.6 Why move broker identity from `hub` to `server.broker`?

Broker identity (`broker_id`, `broker_nickname`, `broker_token`) describes **this machine's** role as a compute broker. It is inherently per-machine, not per-grove. Placing it under `server.broker` (which is global-only) correctly scopes it. The previous placement under `hub` (which allows grove-level overrides) was a historical artifact of the broker registering through the hub client.

### 8.7 Why consolidate all storage into one bucket?

A server should only have one GCS bucket configured. Templates, workspaces, volumes, and other stored data are all namespaced under different path prefixes within that bucket. This avoids the current split between `Settings.Bucket` (client-side workspace persistence) and `GlobalConfig.Storage` (server-side templates). The unified approach is simpler to configure, easier to secure (one set of permissions), and reduces the number of env vars and settings fields. See Phase 7 for the implementation plan.

### 8.8 Why remove `hub.token` and consolidate into `server.auth.dev_token`?

The `hub.token` field stored a bearer token used for Hub API authentication. In practice, this was always a dev token — the same token configured on the server side via `server.auth.dev_token`. Having two separate token fields (`hub.token` on the client, `server.auth.dev_token` on the server) for what is semantically the same value creates confusion. In production, OAuth handles authentication and no static token is needed. Consolidating to a single `server.auth.dev_token` / `SCION_DEV_TOKEN` makes it clear this mechanism is for development only.

### 8.9 Why separate runtime state into `state.yaml`?

Runtime-managed state (like `last_synced_at`) is written programmatically and should not be mixed with user-editable configuration. Keeping it in `settings.yaml` complicates schema validation, creates noisy diffs in version-controlled directories, and makes it unclear which fields are safe to edit. A separate `state.yaml` (grove-level only, not in `~/.scion/`) cleanly separates concerns.

---

## 9. Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Legacy adapter produces different behavior than direct legacy loading | Agents behave differently after upgrade | Comprehensive comparison tests: load legacy file both ways, diff the resolved configs |
| Schema validation rejects valid-but-unusual settings | Blocks users on upgrade | `additionalProperties: false` is strict by design but the migrate command preserves all known fields. Unknown fields are reported as warnings, not errors, during the transition period |
| `server.yaml` users don't notice the deprecation | Two config files drift out of sync | Emit a deprecation warning on every server start when `server.yaml` exists |
| Named harness configs break profile override resolution | Wrong harness config selected | Profile `harness_overrides` keys match harness-config names, not harness types. Document this clearly |
| Koanf deep merge behavior changes between legacy and versioned structs | Subtle config differences | Test merge behavior exhaustively with multi-layer configs |
| Moving broker identity from `hub` to `server.broker` breaks save logic | Broker registration writes to wrong location | Migration adapter must detect and relocate these fields; `SaveSettings` must handle the new location |
| CORS env vars don't work in current code | Server CORS can't be configured via env | Switch to snake_case koanf tags (Phase 4 deliverable) |
| Removing `hub.token` breaks existing dev setups | Users can't authenticate with Hub after upgrade | Migration emits clear warning; `SCION_DEV_TOKEN` env var and `~/.scion/dev-token` file continue to work as before |
| Unified storage refactor touches multiple code paths | Risk of data loss or broken storage | Phase 7 is independent; can be deferred without blocking other phases. Thorough integration tests required |
| `SCION_HUB_URL` → `SCION_HUB_ENDPOINT` rename breaks running agents | Agents in containers can't reach Hub | Support reading both env vars with fallback during transition (Phase 5 deliverable) |

---

## 10. Testing Strategy

### Unit Tests
- Schema validation: valid v1 file passes, missing required fields fail, unknown fields fail.
- Legacy detection: files with/without `schema_version` classified correctly.
- Legacy adapter: every field in `Settings` maps correctly to `VersionedSettings`.
- Resolution: `ResolveHarnessConfig` with default names, named variants, profile overrides.
- Env var mapping: every `x-env-var` in the schema is honored by the Koanf loader.
- Broker identity migration: fields move from `hub` to `server.broker` correctly.
- State file: `last_synced_at` reads from and writes to `state.yaml`, not `settings.yaml`.
- Dev token consolidation: hub client auth reads `server.auth.dev_token` / `SCION_DEV_TOKEN`.

### Integration Tests
- Round-trip: write a `VersionedSettings` to YAML, reload it, compare.
- Migration: take a legacy `settings.yaml`, run the adapter, validate the output against the schema.
- Feature gates: `max_duration` and `max_turns` are only active when `schema_version >= 1`.
- Server consolidation: `server` key in `settings.yaml` produces the same `GlobalConfig` as a standalone `server.yaml`.

### Compatibility Tests
- The default embedded settings (upgraded to versioned format) produce the same resolved configs as the current embedded defaults.
- Existing grove-level settings (legacy format) work without modification and emit a deprecation warning.

---

## 11. Resolved Questions

The following questions were raised during design review and have been resolved.

### RQ-1: `hub.token` consolidated into `server.auth.dev_token`

**Decision:** Remove `hub.token` from the hub client settings. All dev-mode authentication uses `server.auth.dev_token` / `server.auth.dev_token_file` / `SCION_DEV_TOKEN` / `~/.scion/dev-token`. This token should only ever be used in development; production deployments use OAuth.

**Code impact:** Hub client auth code must be updated to read dev token from `server.auth.dev_token` instead of `hub.token`. The `SCION_HUB_TOKEN` env var remains supported for container injection (Section 4.4) where the broker passes a token to agents, but is no longer a settings override. See Phase 4, item 10.

### RQ-2: Unified storage — one bucket, path-namespaced

**Decision:** A server should have a single GCS bucket for all storage needs (templates, workspaces, volumes). Different data types are namespaced under path prefixes within that bucket. The current split between `Settings.Bucket` (client workspace persistence) and `GlobalConfig.Storage` (server template storage) will be unified.

**Code impact:** This is a substantial refactor affecting storage code across both client and server. Deferred to Phase 7 as an independent workstream. The settings structure captures the desired end state; the legacy `bucket` field is preserved during transition.

### RQ-3: Runtime state moved to `state.yaml`

**Decision:** `hub.last_synced_at` and future runtime-managed state moves to a separate `state.yaml` file, present only in non-global groves (project-level `.scion/` directories). See Section 2.9.

**Code impact:** Sync logic must be updated to read/write `state.yaml` instead of `settings.yaml`. See Phase 4, item 9.

### RQ-4: `server.hub.public_url` env var — support both names

**Decision:** Support both `SCION_SERVER_HUB_ENDPOINT` (legacy) and `SCION_SERVER_HUB_PUBLIC_URL` (new) during the transition period.

**Note:** The purpose of storing the Hub's own endpoint on the server configuration may need further investigation and cleanup. Currently `server.hub.public_url` is used to construct agent callback URLs (so agents know where to report status). Whether this should be auto-detected or configured differently is a future consideration.

### RQ-5: `server.runtime_broker` shortened to `server.broker`

**Decision:** Rename to `server.broker`. The Go struct `RuntimeBrokerConfig` will be renamed to `BrokerConfig` in Phase 4. The shorter name matches common usage.

### RQ-6: CORS env vars fixed via snake_case migration

**Decision:** Switch all `GlobalConfig` koanf tags to snake_case as part of Phase 4. This fixes the CORS env var mapping bug and aligns with the versioned settings convention. No camelCase shim needed since the env var names themselves don't change (they map to lowercase anyway).

### RQ-7: `SCION_HUB_URL` standardized to `SCION_HUB_ENDPOINT`

**Decision:** Standardize on `SCION_HUB_ENDPOINT` everywhere. The container-injected env var (formerly `SCION_HUB_URL`) serves the same purpose as the host-side settings override — both identify the Hub API endpoint. Code changes in `runtimebroker/handlers.go` and `sciontool/hub/client.go` (Phase 5, items 6-7). `SCION_HUB_URL` is supported as a fallback during transition.

**Additional:** The web frontend env var `HUB_API_URL` (used for the web server's reverse proxy to the Hub API) is renamed to `SCION_WEB_HUB_API_URL` to follow the `SCION_` prefix convention and distinguish from `SCION_HUB_ENDPOINT`. See Section 4.5.1.

---

## 12. Code Refactors Required

This section summarizes code changes implied by the design decisions, organized by the phase in which they should be addressed.

### Phase 4 Code Changes (Server Config Consolidation)
| Change | Files Affected | Scope |
|---|---|---|
| Rename `RuntimeBrokerConfig` → `BrokerConfig` | `pkg/config/`, `pkg/runtimebroker/`, `cmd/broker.go` | Medium — struct rename + tag updates |
| Switch all `GlobalConfig` koanf tags to snake_case | `pkg/config/hub_config.go`, `pkg/config/global_config.go` | Medium — fixes CORS env vars |
| Remove `hub.token` from settings; update hub client auth | `pkg/config/settings.go`, `pkg/hubclient/`, `pkg/config/koanf.go` | Medium — auth flow change |
| Implement `state.yaml` read/write | `pkg/config/` (new file), `pkg/hubclient/sync.go` | Medium — new file handling |
| Support legacy `SCION_SERVER_RUNTIMEBROKER_*` env vars as fallback | `pkg/config/koanf.go` or new loader | Small — env var aliasing |

### Phase 5 Code Changes (Feature Gates & Env Var Standardization)
| Change | Files Affected | Scope |
|---|---|---|
| `SCION_HUB_URL` → `SCION_HUB_ENDPOINT` in container injection | `pkg/runtimebroker/handlers.go`, tests | Small |
| `SCION_HUB_URL` → `SCION_HUB_ENDPOINT` in sciontool reader | `pkg/sciontool/hub/client.go`, tests | Small |
| `HUB_API_URL` → `SCION_WEB_HUB_API_URL` in web server | `web/src/server/config.ts` | Small |

### Phase 7 Code Changes (Unified Storage — substantial refactor)
| Change | Files Affected | Scope |
|---|---|---|
| Consolidate `BucketConfig` and `StorageConfig` | `pkg/config/settings.go`, `pkg/config/global_config.go` | Large |
| Refactor workspace persistence to use unified storage | `pkg/agent/`, `pkg/runtime/` | Large |
| Refactor template storage to use path-prefixed storage | `pkg/hub/storage/` | Large |
| Add path-prefix namespacing to GCS client | `pkg/hub/storage/gcs.go` (or equivalent) | Medium |