#!/usr/bin/env bash
# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# extras/scion-chat-app/install.sh — Install the chat app alongside a
# provisioned Scion Hub (via scripts/starter-hub/).
#
# Runs as the scion user without requiring sudo. System-level changes
# (Caddyfile) are printed as manual instructions for an admin to apply.
#
# Idempotent: safe to re-run after hub updates that overwrite the Caddyfile
# or settings.yaml.
#
# Usage:
#   make install          (builds first, then runs this script)
#   ./install.sh          (skip build, install only)

set -euo pipefail

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SCION_HOME="${HOME}"
SCION_DIR="${SCION_HOME}/.scion"
INSTALL_BIN="${SCION_HOME}/.local/bin"
CADDYFILE="/etc/caddy/Caddyfile"
SETTINGS_FILE="${SCION_DIR}/settings.yaml"
HUB_ENV="${SCION_DIR}/hub.env"
CHAT_ENV="${SCION_DIR}/chat-app.env"
CONFIG_FILE="${SCION_DIR}/scion-chat-app.yaml"
SYSTEMD_USER_DIR="${SCION_HOME}/.config/systemd/user"

LISTEN_PORT="${CHAT_APP_LISTEN_PORT:-8443}"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
step()    { echo "=> $*"; }
substep() { echo "   $*"; }

need_file() {
    if [[ ! -f "$1" ]]; then
        echo "ERROR: required file not found: $1" >&2
        echo "       $2" >&2
        exit 1
    fi
}

# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------
need_file "${HUB_ENV}" "Run scripts/starter-hub/gce-start-hub.sh --full first."
need_file "${CHAT_ENV}" "Copy extras/scion-chat-app/chat-app.env.sample to ${CHAT_ENV} and fill in values."

# Source env files (hub.env first, chat-app.env may reference hub vars).
set -a
# shellcheck source=/dev/null
source "${HUB_ENV}"
# shellcheck source=/dev/null
source "${CHAT_ENV}"
set +a

# Resolve the hub endpoint — prefer SCION_HUB_ENDPOINT, fall back to
# SCION_SERVER_BASE_URL which is set by the starter-hub provisioning.
SCION_HUB_ENDPOINT="${SCION_HUB_ENDPOINT:-${SCION_SERVER_BASE_URL:-}}"
if [[ -z "${SCION_HUB_ENDPOINT}" ]]; then
    echo "ERROR: neither SCION_HUB_ENDPOINT nor SCION_SERVER_BASE_URL is set in ${HUB_ENV}" >&2
    exit 1
fi

# Derive the external URL from the hub endpoint.
EXTERNAL_URL="${SCION_HUB_ENDPOINT}/chat/events"

step "Installing scion-chat-app"

# ---------------------------------------------------------------------------
# 1. Binary
# ---------------------------------------------------------------------------
BINARY="${SCRIPT_DIR}/scion-chat-app"
need_file "${BINARY}" "Run 'make build' first."

mkdir -p "${INSTALL_BIN}"
substep "Installing binary to ${INSTALL_BIN}"
install -m 755 "${BINARY}" "${INSTALL_BIN}/scion-chat-app"

# ---------------------------------------------------------------------------
# 2. Config file
# ---------------------------------------------------------------------------
substep "Writing config to ${CONFIG_FILE}"
cat > "${CONFIG_FILE}" <<EOF
hub:
  endpoint: "${SCION_HUB_ENDPOINT}"
  user: "${CHAT_APP_HUB_USER}"
  credentials: "${CHAT_APP_HUB_CREDENTIALS:-}"

plugin:
  listen_address: "localhost:9090"

platforms:
  google_chat:
    enabled: true
    project_id: "${CHAT_APP_PROJECT_ID}"
    credentials: "${CHAT_APP_CREDENTIALS:-}"
    listen_address: ":${LISTEN_PORT}"
    external_url: "${EXTERNAL_URL}"
    service_account_email: "${CHAT_APP_SERVICE_ACCOUNT_EMAIL:-}"
    command_id_map:
      "1": "scion"

state:
  database: "${SCION_DIR}/scion-chat-app.db"

notifications:
  trigger_activities:
    - COMPLETED
    - WAITING_FOR_INPUT
    - ERROR
    - STALLED
    - LIMITS_EXCEEDED

logging:
  level: "info"
  format: "json"
EOF
chmod 600 "${CONFIG_FILE}"

# ---------------------------------------------------------------------------
# 3. Systemd user unit
# ---------------------------------------------------------------------------
substep "Installing systemd user unit"
mkdir -p "${SYSTEMD_USER_DIR}"
cat > "${SYSTEMD_USER_DIR}/scion-chat-app.service" <<EOF
[Unit]
Description=Scion Chat App
After=network.target

[Service]
Environment="HOME=${SCION_HOME}"
StandardOutput=journal
StandardError=journal
ExecStart=${INSTALL_BIN}/scion-chat-app -config ${CONFIG_FILE}
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
EOF

systemctl --user daemon-reload
systemctl --user enable scion-chat-app

# Enable lingering so the user service survives after logout.
if ! loginctl show-user "$(whoami)" --property=Linger 2>/dev/null | grep -q 'Linger=yes'; then
    substep "Enabling loginctl linger (may require sudo if not already enabled)"
    loginctl enable-linger "$(whoami)" 2>/dev/null || \
        echo "   NOTICE: Could not enable linger. Ask an admin to run: sudo loginctl enable-linger $(whoami)"
fi

# ---------------------------------------------------------------------------
# 4. Patch Caddyfile
# ---------------------------------------------------------------------------
step "Checking Caddyfile"

if [[ ! -f "${CADDYFILE}" ]]; then
    substep "No Caddyfile found at ${CADDYFILE}, skipping"
elif ! head -1 "${CADDYFILE}" >/dev/null 2>&1; then
    substep "Cannot read ${CADDYFILE}, skipping"
else
    # Check if the chat route is already present.
    if grep -q '/chat/\*' "${CADDYFILE}" 2>/dev/null; then
        substep "Caddyfile already has chat route"
    else
        DOMAIN="$(head -1 "${CADDYFILE}" | sed 's/ *{$//')"
        TLS_LINE="$(grep '^\s*tls ' "${CADDYFILE}" || true)"

        echo ""
        echo "   ┌─────────────────────────────────────────────────────────────┐"
        echo "   │ ACTION REQUIRED: Update the Caddyfile to route /chat/*     │"
        echo "   │ An admin with sudo access needs to run:                    │"
        echo "   └─────────────────────────────────────────────────────────────┘"
        echo ""
        echo "   sudo tee ${CADDYFILE} <<'CADDY_EOF'"
        echo "${DOMAIN} {"
        echo "    handle /chat/* {"
        echo "        reverse_proxy localhost:${LISTEN_PORT}"
        echo "    }"
        echo "    handle {"
        echo "        reverse_proxy localhost:8080"
        echo "    }"
        echo "    ${TLS_LINE}"
        echo "}"
        echo "CADDY_EOF"
        echo "   sudo systemctl reload caddy"
        echo ""
    fi
fi

# ---------------------------------------------------------------------------
# 5. Patch Hub settings.yaml — add broker plugin entry
# ---------------------------------------------------------------------------
step "Patching Hub settings.yaml"

if [[ ! -f "${SETTINGS_FILE}" ]]; then
    substep "No settings.yaml found at ${SETTINGS_FILE}, skipping"
elif grep -q 'googlechat' "${SETTINGS_FILE}"; then
    substep "settings.yaml already has googlechat plugin config"
else
    # The starter-hub settings.yaml doesn't include a plugins section.
    # If a future version adds one, we handle both cases.
    if grep -q '^plugins:' "${SETTINGS_FILE}"; then
        if grep -q '^\s*broker:' "${SETTINGS_FILE}"; then
            sed -i '/^\s*broker:/a\    googlechat:\n      self_managed: true\n      address: "localhost:9090"' "${SETTINGS_FILE}"
        else
            sed -i '/^plugins:/a\  broker:\n    googlechat:\n      self_managed: true\n      address: "localhost:9090"' "${SETTINGS_FILE}"
        fi
    else
        printf '\nplugins:\n  broker:\n    googlechat:\n      self_managed: true\n      address: "localhost:9090"\n' >> "${SETTINGS_FILE}"
    fi
    substep "settings.yaml updated with googlechat plugin config"
fi

# ---------------------------------------------------------------------------
# 6. Start / restart
# ---------------------------------------------------------------------------
step "Restarting scion-chat-app"
systemctl --user restart scion-chat-app
substep "Done — check status with: journalctl --user -u scion-chat-app -f"
