# Scion Chat App

A standalone service that bridges Google Chat (and future Slack) with the Scion Hub, enabling users to manage agents and receive notifications directly from their chat workspace. Runs as both a message broker plugin for real-time agent communication and an API proxy for operational commands.

## Features

- Bidirectional messaging between chat users and Scion agents
- Agent management via slash commands (`/scion list`, `/scion start`, etc.)
- Automatic user identity mapping (chat user to Hub account)
- Space-to-grove linking for scoped interactions
- Real-time notification cards for agent status changes (`COMPLETED`, `ERROR`, `WAITING_FOR_INPUT`, etc.)
- Interactive `ask_user` response flow with inline reply fields
- Per-user notification subscriptions with activity-type filtering

## Prerequisites

- Go 1.25+
- A running Scion Hub instance
- A GCP project with the Google Chat API enabled
- A GCP service account with:
  - Google Chat API permissions (for sending/receiving messages)
  - Access to the Hub's signing key in GCP Secret Manager (for user impersonation)
- A Hub admin user account for the chat app to authenticate as

## GCP Setup

### 1. Create a GCP Project (or use existing)

```bash
gcloud projects create my-scion-chat --name="Scion Chat App"
gcloud config set project my-scion-chat
```

### 2. Enable Required APIs

```bash
gcloud services enable chat.googleapis.com
gcloud services enable secretmanager.googleapis.com
gcloud services enable artifactregistry.googleapis.com  # if deploying to Cloud Run
gcloud services enable run.googleapis.com                # if deploying to Cloud Run
```

### 3. Create a Service Account

```bash
# Create the service account
gcloud iam service-accounts create scion-chat-app \
  --display-name="Scion Chat App"

# Grant access to the Hub's signing key in Secret Manager
gcloud secrets add-iam-policy-binding <HUB_SIGNING_KEY_SECRET> \
  --member="serviceAccount:scion-chat-app@my-scion-chat.iam.gserviceaccount.com" \
  --role="roles/secretmanager.secretAccessor"

# Create and download a key file (for local development)
gcloud iam service-accounts keys create chat-sa-key.json \
  --iam-account=scion-chat-app@my-scion-chat.iam.gserviceaccount.com
```

### 4. Register the Google Chat App

1. Go to the [Google Cloud Console](https://console.cloud.google.com) > **APIs & Services** > **Google Chat API** > **Configuration**
2. Set **App name** and **Avatar URL** (e.g., "Scion")
3. Under **Functionality**, enable:
   - Receive 1:1 messages
   - Join spaces and group conversations
4. Under **Connection settings**, select **HTTP endpoint URL** and enter:
   ```
   https://<YOUR_CHAT_APP_URL>/events
   ```
5. Under **Slash commands**, add:
   | Command ID | Command  | Description                |
   |------------|----------|----------------------------|
   | 1          | `/scion` | Scion agent management     |
6. Under **Permissions**, configure which users/OUs can install the app
7. Note the **Project number** (used as the `audience` in configuration)

## Configuration

Create a YAML configuration file (e.g., `scion-chat-app.yaml`):

```yaml
hub:
  # Scion Hub endpoint
  endpoint: "https://hub.example.com"
  # Hub admin user for system-level operations
  user: "chat-app@example.com"
  # Path to a file containing the Hub bearer token.
  # If omitted, the app falls back to device authorization flow.
  credentials: "/path/to/hub-token"

# Broker plugin RPC server settings.
# The Hub connects to this address as a self-managed plugin.
plugin:
  listen_address: "localhost:9090"

platforms:
  google_chat:
    enabled: true
    # GCP project ID where the Chat app is registered
    project_id: "my-scion-chat"
    # Service account key for Google Chat API calls
    credentials: "/path/to/chat-sa-key.json"
    # HTTP endpoint for receiving Google Chat webhook events
    listen_address: ":8443"
    # Verification audience (GCP project number, from Chat API config)
    audience: "1234567890"

  slack:
    enabled: false
    # bot_token: "${SLACK_BOT_TOKEN}"
    # signing_secret: "${SLACK_SIGNING_SECRET}"
    # listen_address: ":8444"

state:
  # SQLite database for user mappings, space links, and subscriptions
  database: "/var/lib/scion-chat-app/state.db"

notifications:
  # Which agent activities to relay to chat spaces
  trigger_activities:
    - COMPLETED
    - WAITING_FOR_INPUT
    - ERROR
    - STALLED
    - LIMITS_EXCEEDED

logging:
  level: "info"    # debug, info, warn, error
  format: "json"   # json or text
```

Environment variables in the form `${VAR}` or `$VAR` are expanded in the config file before parsing.

### Hub-Side Plugin Configuration

Register the chat app as a self-managed broker plugin in the Hub's settings:

```yaml
# In Hub settings (scion-settings.yaml or equivalent)
plugins:
  broker:
    googlechat:
      self_managed: true
      address: "localhost:9090"
      config:
        hub_endpoint: "https://hub.example.com"
        project_id: "my-scion-chat"
```

## Local Development

```bash
cd extras/scion-chat-app

# Download dependencies
go mod download

# Create a minimal config for local development
cat > dev-config.yaml <<'EOF'
hub:
  endpoint: "http://localhost:8080"
plugin:
  listen_address: "localhost:9090"
platforms:
  google_chat:
    enabled: true
    project_id: "my-gcp-project"
    credentials: "./chat-sa-key.json"
    listen_address: ":8443"
    audience: "1234567890"
state:
  database: "./scion-chat-app.db"
logging:
  level: "debug"
  format: "text"
EOF

# Run the server
go run ./cmd/scion-chat-app/ --config dev-config.yaml
```

The app starts two servers:
- **Port 8443** - Google Chat webhook endpoint (receives events from Google Chat)
- **Port 9090** - Broker plugin RPC server (receives messages from the Hub)

### Testing Webhooks Locally

Google Chat sends events to the configured HTTP endpoint. For local development, use a tunnel service (e.g., `ngrok`, `cloudflared`) to expose port 8443:

```bash
ngrok http 8443
```

Then update the Google Chat API configuration in the GCP Console with the tunnel URL (e.g., `https://abc123.ngrok.io/events`).

## Testing

```bash
cd extras/scion-chat-app
go test ./...
```

## Docker Build

The Dockerfile uses a multi-stage build. It must be built from the repo root because the chat app module has a `replace` directive pointing to the parent Scion module:

```bash
docker build -t scion-chat-app -f extras/scion-chat-app/Dockerfile .
```

Run the container:

```bash
docker run -p 8443:8443 -p 9090:9090 \
  -v /path/to/config.yaml:/etc/scion-chat-app/config.yaml \
  -v /path/to/chat-sa-key.json:/etc/scion-chat-app/chat-sa-key.json \
  scion-chat-app
```

## Deploy to Cloud Run

The included `cloudbuild.yaml` builds, pushes, and deploys the app to Cloud Run.

```bash
gcloud builds submit \
  --config=extras/scion-chat-app/cloudbuild.yaml \
  --substitutions=_GIT_SHA=$(git rev-parse --short HEAD)
```

Override defaults with substitutions:
- `_REGISTRY` - Artifact Registry path (default: `us-central1-docker.pkg.dev/$PROJECT_ID/scion`)
- `_SERVICE_NAME` - Cloud Run service name (default: `scion-chat-app`)
- `_REGION` - Deployment region (default: `us-central1`)

The deployment configures:
- 512 MiB memory, 1 vCPU
- Min 1 / max 3 instances (keeps at least one warm for webhook responsiveness)
- 300s request timeout
- Authentication required (configure IAM for Google Chat to invoke)

### Cloud Run Configuration

After deploying, mount the config file and service account key as secrets or volumes:

```bash
# Store config as a secret
gcloud secrets create scion-chat-app-config \
  --data-file=config.yaml

# Update the Cloud Run service to mount it
gcloud run services update scion-chat-app \
  --region=us-central1 \
  --update-secrets=/etc/scion-chat-app/config.yaml=scion-chat-app-config:latest
```

Update the Google Chat API HTTP endpoint URL in the GCP Console to point to the Cloud Run service URL (e.g., `https://scion-chat-app-xxxxx.run.app/events`).

## Slash Commands

Once the app is running and connected, users interact via `/scion` in Google Chat:

| Command | Description |
|---------|-------------|
| `/scion help` | Show available commands |
| `/scion register` | Link your chat account to your Hub user (auto-matches by email, falls back to device auth) |
| `/scion unregister` | Remove your chat-to-Hub account link |
| `/scion link <grove-slug>` | Link the current space to a grove (admin only) |
| `/scion unlink` | Unlink the current space from its grove (admin only) |
| `/scion list` | List agents in the linked grove |
| `/scion status <agent>` | Show agent status card with action buttons |
| `/scion create <agent>` | Create a new agent |
| `/scion start <agent>` | Start an agent |
| `/scion stop <agent>` | Stop an agent |
| `/scion delete <agent>` | Delete an agent (with confirmation) |
| `/scion logs <agent>` | Show recent agent logs |
| `/scion message <agent> <text>` | Send a message to an agent (supports `--thread <id>`) |
| `/scion subscribe <agent>` | Subscribe to agent notifications (with activity filter dialog) |
| `/scion unsubscribe <agent>` | Unsubscribe from agent notifications |

You can also @mention the bot to send messages to agents:

```
@Scion tell deploy-agent to check the staging cluster
```

## Architecture

```
Google Chat ──webhooks──> scion-chat-app ──Hub API──> Scion Hub
                              │                          │
                              │◄──broker plugin (RPC)────┘
                              │
                          SQLite (local state)
```

The chat app operates under three identity contexts:

1. **Hub admin user** - System-level Hub operations (notification subscriptions, grove lookups)
2. **GCP service account** - Infrastructure access (Secret Manager for signing keys, Google Chat API)
3. **Impersonated chat users** - User-initiated commands are executed as the linked Hub user via short-lived scoped tokens

## Ports

| Port | Purpose |
|------|---------|
| 8443 | Google Chat webhook endpoint |
| 9090 | Broker plugin RPC server |

## Health Check

The app exposes a `/healthz` endpoint on the webhook server (port 8443) that checks Hub API reachability, broker plugin connection, and database accessibility.
