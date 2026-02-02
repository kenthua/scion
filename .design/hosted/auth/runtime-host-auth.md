# Runtime Host Authentication Design

## Status
**Phase 2 Implemented** (Runtime Host Integration)

## 1. Overview

This document specifies how Runtime Hosts authenticate with the Scion Hub using HMAC-based request signing. Runtime Hosts are compute nodes that execute agents on behalf of the Hub, and require a secure bidirectional authentication mechanism distinct from user/agent authentication.

### 1.1 Relationship to Server Auth

The Hub's unified authentication middleware (see [server-auth-design.md](server-auth-design.md)) handles user, agent, and API key authentication. Runtime Host authentication is a **separate pathway** that:

- Uses HMAC signatures rather than bearer tokens
- Authenticates infrastructure components, not users or agents
- Enables bidirectional trust (Hub can push commands to hosts)
- Operates at the host level, not the request/session level

| Authentication Type | Token/Mechanism | Direction | Purpose |
|---------------------|-----------------|-----------|---------|
| User (Web/CLI) | JWT Bearer | Client → Hub | User API access |
| Agent (sciontool) | JWT Bearer | Agent → Hub | Agent status updates |
| **Runtime Host** | **HMAC Signature** | **Bidirectional** | **Host ↔ Hub trust** |

### 1.2 Goals

1. **Mutual Authentication** - Both Hub and Runtime Host can verify each other's identity
2. **Replay Prevention** - Timestamped, nonce-protected requests prevent replay attacks
3. **Secure Bootstrap** - One-time secret exchange with user authorization
4. **Minimal Exposure** - Shared secret never transmitted after initial registration
5. **Offline Verification** - No external service calls required for signature validation

### 1.3 Non-Goals

- Token-based authentication (use JWT for agents/users instead)
- Session management (each request is independently verified)
- Rate limiting (handled by separate middleware)

---

## 2. Architecture

### 2.1 Components

```
┌─────────────────┐                    ┌─────────────────┐
│   Scion Hub     │◄──── HTTPS ────────│  Runtime Host   │
│                 │      (HMAC)        │                 │
│  ┌───────────┐  │                    │  ┌───────────┐  │
│  │ Secret    │  │                    │  │ Secret    │  │
│  │ Store     │  │                    │  │ Store     │  │
│  │ (per host)│  │                    │  │ (local)   │  │
│  └───────────┘  │                    │  └───────────┘  │
│                 │                    │                 │
│  ┌───────────┐  │                    │  ┌───────────┐  │
│  │ HMAC      │  │                    │  │ HMAC      │  │
│  │ Verifier  │  │                    │  │ Signer    │  │
│  └───────────┘  │                    │  └───────────┘  │
└─────────────────┘                    └─────────────────┘
```

### 2.2 Header Specification

All HMAC-authenticated requests include these headers:

| Header | Format | Description |
|--------|--------|-------------|
| `X-Scion-Host-ID` | UUID or slug | Unique identifier for the Runtime Host |
| `X-Scion-Timestamp` | RFC 3339 | Request timestamp (e.g., `2025-01-30T12:00:00Z`) |
| `X-Scion-Nonce` | Base64 (16 bytes) | Random nonce for replay prevention |
| `X-Scion-Signature` | Base64 (32 bytes) | HMAC-SHA256 signature |

---

## 3. Phase 1: Initial Registration (Bootstrap)

Before HMAC authentication can work, both parties need a shared secret. This is the only phase where a credential is transmitted.

### 3.1 Registration Flow

```
┌──────────────┐        ┌──────────────┐        ┌─────────────┐
│  Admin User  │        │ Runtime Host │        │     Hub     │
│   (CLI/Web)  │        │              │        │             │
└──────────────┘        └──────────────┘        └─────────────┘
       │                       │                       │
       │ POST /api/v1/hosts    │                       │
       │ (with user token)     │                       │
       │──────────────────────────────────────────────►│
       │                       │                       │
       │                       │   { hostId, joinToken, expiry }
       │◄──────────────────────────────────────────────│
       │                       │                       │
       │ Provide joinToken     │                       │
       │──────────────────────►│                       │
       │                       │                       │
       │                       │ POST /api/v1/hosts/join
       │                       │ { hostId, joinToken, publicInfo }
       │                       │──────────────────────►│
       │                       │                       │
       │                       │   { secretKey, hubEndpoint }
       │                       │◄──────────────────────│
       │                       │                       │
       │                       │ [Store secret locally]│
       │                       │                       │
```

### 3.2 Registration Steps

1. **Host Creation (User-Initiated)**
   - An authorized user (admin or host manager) calls the Hub to register a new host
   - Hub generates a unique `hostId` and a short-lived `joinToken`
   - User receives the join token to provide to the host operator

2. **Host Join (Host-Initiated)**
   - The Runtime Host sends its `joinToken` to the Hub's bootstrap endpoint
   - This must occur over HTTPS
   - Host includes public metadata (hostname, capabilities, version)

3. **Secret Exchange**
   - Hub generates a high-entropy secret key ($K_s$) using `crypto/rand`
   - Hub stores `hash($K_s$)` associated with the `hostId`
   - Hub returns $K_s$ to the Runtime Host (one-time transmission)
   - Runtime Host stores $K_s$ in local secure storage

### 3.3 Simplified CLI Registration

In the common case where a user is logged into the Runtime Host machine and already authenticated with the Hub, the registration flow can be streamlined via the CLI:

```bash
# User runs this on the Runtime Host machine
scion hub hosts join --name "production-host-1" --capabilities docker,kubernetes
```

This command orchestrates the full registration flow:

1. **Creates Host Record** - Calls `POST /api/v1/hosts` using the user's existing Hub credentials
2. **Extracts Join Token** - Receives the `joinToken` from the response
3. **Completes Join** - Immediately calls `POST /api/v1/hosts/join` with the token
4. **Stores Credentials** - Saves the returned secret to local credential storage

The Runtime Host API exposes a `JoinHub` method that performs steps 3-4:

```go
// pkg/runtimehost/api.go
func (h *RuntimeHost) JoinHub(ctx context.Context, hostID, joinToken string) error
```

For remote or headless hosts where the user cannot run commands directly, the manual two-step flow (user creates host, operator provides join token) remains available.

### 3.4 Hub Endpoints

```
POST /api/v1/hosts
Authorization: Bearer <user-token>
Request:
{
  "name": "production-host-1",
  "capabilities": ["docker", "kubernetes"],
  "labels": { "region": "us-west-2" }
}

Response:
{
  "hostId": "host-uuid-123",
  "joinToken": "scion_join_AbCdEf123456...",
  "expiresAt": "2025-01-30T13:00:00Z"
}
```

```
POST /api/v1/hosts/join
Request:
{
  "hostId": "host-uuid-123",
  "joinToken": "scion_join_AbCdEf123456...",
  "hostname": "prod-host-1.example.com",
  "version": "1.0.0",
  "capabilities": ["docker"]
}

Response:
{
  "secretKey": "base64-encoded-256-bit-key",
  "hubEndpoint": "https://hub.scion.example.com",
  "hostId": "host-uuid-123"
}
```

### 3.5 Secret Storage

**Hub Side:**
- Initial storage in filesystem at ~/.scion/hub-secrets.json
- Future implementation: Store secret hash in database: `host_secrets(host_id, secret_hash, created_at, rotated_at)`
- Keep plaintext secret only in memory during validation
- Support secret rotation with grace period

**Runtime Host Side:**
- Initial: JSON file at `~/.scion/host-credentials.json` (mode 0600)
- Production: Google Secret Manager or HashiCorp Vault
- Structure:
  ```json
  {
    "hostId": "host-uuid-123",
    "secretKey": "base64-encoded-key",
    "hubEndpoint": "https://hub.scion.example.com",
    "registeredAt": "2025-01-30T12:00:00Z"
  }
  ```

---

## 4. Phase 2: Ongoing Authentication (Request Signing)

Once registered, all requests between Runtime Host and Hub are HMAC-signed.

### 4.1 Signing Process (Sender)

1. **Prepare Metadata**
   - Generate timestamp $T$ (current UTC time, RFC 3339)
   - Generate random nonce $N$ (16 bytes, base64-encoded)

2. **Build Canonical String**
   - Concatenate request elements in strict order:
   ```
   S = METHOD + "\n" +
       PATH + "\n" +
       QUERY (sorted) + "\n" +
       TIMESTAMP + "\n" +
       NONCE + "\n" +
       CONTENT_HASH
   ```
   - `CONTENT_HASH` = SHA-256 of request body (empty string hash if no body)

3. **Compute Signature**
   ```
   Signature = HMAC-SHA256(K_s, S)
   ```

4. **Attach Headers**
   ```
   X-Scion-Host-ID: host-uuid-123
   X-Scion-Timestamp: 2025-01-30T12:00:00Z
   X-Scion-Nonce: random-base64-nonce
   X-Scion-Signature: computed-signature-base64
   ```

### 4.2 Verification Process (Receiver)

1. **Extract Headers**
   - Parse all `X-Scion-*` headers
   - Reject if any required header is missing

2. **Clock Skew Check**
   - Parse timestamp and compare to current time
   - Reject if difference > 5 minutes (configurable)

3. **Nonce Validation** (Optional, for strict replay prevention)
   - Check nonce against recent-nonce cache
   - Reject if nonce was seen within the clock skew window
   - Store nonce with expiry

4. **Secret Retrieval**
   - Look up secret by `X-Scion-Host-ID`
   - Reject if host not found or deactivated

5. **Signature Verification**
   - Rebuild canonical string from received request
   - Compute expected signature
   - Use `hmac.Equal()` for constant-time comparison
   - Reject if signatures don't match

### 4.3 Go Implementation

```go
// pkg/hub/hostauth.go

type HostAuthConfig struct {
    MaxClockSkew time.Duration // Default: 5 minutes
    EnableNonceCache bool      // Enable strict replay prevention
}

type HostAuthMiddleware struct {
    secrets HostSecretStore
    config  HostAuthConfig
    nonces  *NonceCache // Optional
}

func (m *HostAuthMiddleware) Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        hostID := r.Header.Get("X-Scion-Host-ID")
        timestamp := r.Header.Get("X-Scion-Timestamp")
        nonce := r.Header.Get("X-Scion-Nonce")
        signature := r.Header.Get("X-Scion-Signature")

        // Validate presence
        if hostID == "" || timestamp == "" || signature == "" {
            writeHostAuthError(w, "missing required authentication headers")
            return
        }

        // Parse and validate timestamp
        ts, err := time.Parse(time.RFC3339, timestamp)
        if err != nil {
            writeHostAuthError(w, "invalid timestamp format")
            return
        }
        if time.Since(ts).Abs() > m.config.MaxClockSkew {
            writeHostAuthError(w, "timestamp outside acceptable window")
            return
        }

        // Optional: Check nonce
        if m.config.EnableNonceCache && nonce != "" {
            if m.nonces.Seen(nonce) {
                writeHostAuthError(w, "duplicate nonce (possible replay)")
                return
            }
            m.nonces.Add(nonce, m.config.MaxClockSkew)
        }

        // Get secret
        secret, err := m.secrets.GetSecret(r.Context(), hostID)
        if err != nil {
            writeHostAuthError(w, "unknown host")
            return
        }

        // Build canonical string and verify
        canonical := buildCanonicalString(r, timestamp, nonce)
        expected := computeHMAC(secret, canonical)

        providedSig, err := base64.StdEncoding.DecodeString(signature)
        if err != nil || !hmac.Equal(expected, providedSig) {
            writeHostAuthError(w, "invalid signature")
            return
        }

        // Add host context
        ctx := context.WithValue(r.Context(), hostContextKey{}, &HostIdentity{
            ID: hostID,
        })
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

func buildCanonicalString(r *http.Request, timestamp, nonce string) []byte {
    var buf bytes.Buffer
    buf.WriteString(r.Method)
    buf.WriteByte('\n')
    buf.WriteString(r.URL.Path)
    buf.WriteByte('\n')
    buf.WriteString(canonicalQuery(r.URL.Query()))
    buf.WriteByte('\n')
    buf.WriteString(timestamp)
    buf.WriteByte('\n')
    buf.WriteString(nonce)
    buf.WriteByte('\n')
    buf.WriteString(contentHash(r))
    return buf.Bytes()
}

func computeHMAC(secret, data []byte) []byte {
    h := hmac.New(sha256.New, secret)
    h.Write(data)
    return h.Sum(nil)
}
```

---

## 5. Bidirectional Authentication

Because both the Hub and Runtime Host possess $K_s$, either party can initiate authenticated requests.

### 5.1 Runtime Host → Hub

The Runtime Host communicates with the Hub for host-level operations:
- Host heartbeat and health status
- Host resource availability (CPU, memory, disk)
- Agent lifecycle events (started, stopped, crashed)
- Grove registration updates

> **Note:** Agent-level status updates (thinking, executing, waiting for input) are sent directly by sciontool running *inside* the agent container using agent JWT authentication, not by the Runtime Host. See [sciontool-auth.md](sciontool-auth.md) for agent authentication.

### 5.2 Hub → Runtime Host

The Hub can push commands to registered hosts:
- Agent provisioning requests
- Agent termination commands
- Configuration updates
- Secret rotation notifications

**Runtime Host Endpoints:**
```
POST /api/v1/agents/provision   # Start a new agent
DELETE /api/v1/agents/{id}      # Stop an agent
POST /api/v1/config/reload      # Reload configuration
POST /api/v1/secrets/rotate     # Accept new secret
```

### 5.3 Host Endpoint Security

Runtime Hosts must:
1. Bind to localhost or private network only (not public internet)
2. Validate Hub signatures using the same HMAC mechanism
3. Verify the requesting Hub matches the registered `hubEndpoint`

---

## 6. Secret Rotation

Secrets should be rotated periodically or on security events.

### 6.1 Rotation Flow

```
┌─────────────┐                    ┌──────────────┐
│     Hub     │                    │ Runtime Host │
└─────────────┘                    └──────────────┘
       │                                  │
       │ POST /api/v1/secrets/rotate      │
       │ (signed with current secret)     │
       │ { newSecret: "base64..." }       │
       │─────────────────────────────────►│
       │                                  │
       │                                  │ [Store new secret]
       │                                  │ [Keep old for grace period]
       │                                  │
       │           200 OK                 │
       │◄─────────────────────────────────│
       │                                  │
       │ [Mark old secret deprecated]     │
       │                                  │
       │ ... grace period (e.g., 1 hour) ...
       │                                  │
       │ [Remove old secret]              │
       │                                  │
```

### 6.2 Dual-Secret Validation

During the grace period, the Hub accepts signatures from either secret:

```go
func (m *HostAuthMiddleware) verifyWithRotation(hostID string, canonical, signature []byte) bool {
    secrets, _ := m.secrets.GetSecrets(hostID) // Returns current + deprecated
    for _, secret := range secrets {
        expected := computeHMAC(secret.Key, canonical)
        if hmac.Equal(expected, signature) {
            return true
        }
    }
    return false
}
```

---

## 7. Configuration

### 7.1 Hub Configuration

```yaml
server:
  hostAuth:
    enabled: true
    maxClockSkew: 5m
    enableNonceCache: true
    nonceCacheTTL: 10m
    secretRotation:
      gracePeriod: 1h
      autoRotateInterval: 30d  # 0 to disable
    joinToken:
      expiry: 1h
      length: 32  # bytes
```

### 7.2 Runtime Host Configuration

```yaml
host:
  hub:
    endpoint: "https://hub.scion.example.com"
  credentials:
    file: "~/.scion/host-credentials.json"
    # OR for production:
    # secretManager: "projects/my-project/secrets/scion-host-secret"
  api:
    listenAddr: "127.0.0.1:9815"  # For Hub callbacks
```

---

## 8. Security Considerations

### 8.1 Transport Security

- All communication MUST use HTTPS
- Minimum TLS 1.2, prefer TLS 1.3
- Certificate validation required (no `InsecureSkipVerify`)

### 8.2 Secret Entropy

- Secrets MUST be 256 bits (32 bytes) minimum
- Generated using `crypto/rand`
- Base64-encoded for storage/transmission

### 8.3 Clock Synchronization

- Both Hub and Runtime Hosts should use NTP
- 5-minute skew tolerance accommodates minor drift
- Larger skew may indicate MITM or misconfiguration

### 8.4 Nonce Cache Considerations

- Nonce cache prevents replay within clock skew window
- Memory cost: ~50 bytes per nonce × requests per window
- Optional for lower-security deployments

### 8.5 Audit Logging

Log all host authentication events:
```go
type HostAuthEvent struct {
    EventType  string    `json:"eventType"`  // register, join, auth_success, auth_failure
    HostID     string    `json:"hostId"`
    IPAddress  string    `json:"ipAddress"`
    Success    bool      `json:"success"`
    FailReason string    `json:"failReason,omitempty"`
    Timestamp  time.Time `json:"timestamp"`
}
```

---

## 9. Open Questions

None pending.

---

## 10. Resolved Questions

### 10.1 Registration Authorization

**Question:** What permission level should be required to register a new Runtime Host?

- **Option A:** Admin-only (restrictive)
- **Option B:** New "host-manager" role
- **Option C:** Any authenticated user (permissive, for dev/testing)

**Decision:** Admin-only for initial implementation. Future RBAC system may introduce a dedicated "host-manager" role for delegated host administration.

### 10.2 Host Identity Verification

**Question:** Should we verify host identity beyond the join token during registration?

Considerations:
- Should hosts provide a CSR for mutual TLS?
- Should hosts prove control of a hostname/IP?
- Is the join token sufficient for trust establishment?

**Decision:** Admin-issued join token is sufficient for trust establishment. The join token is short-lived and requires admin authorization to create, establishing a chain of trust. mTLS may be considered as a future enhancement for high-security deployments.

### 10.3 Nonce Storage Backend

**Question:** What backend should store nonces for replay prevention?

- **Option A:** In-memory (simple, lost on restart)
- **Option B:** Filesystem (`~/.scion/`) for persistence
- **Option C:** Redis/Memcached (distributed, for HA)

**Decision:** In-memory storage is sufficient for nonce tracking. Nonces only need to be tracked within the clock skew window (5 minutes), so persistence across restarts is not required. If a restart occurs, the timestamp validation alone provides adequate replay protection for the brief window where old nonces might be reused.

### 10.4 Hub-to-Host Communication Model

**Question:** Should the Hub push commands to hosts, or should hosts poll?

| Approach | Pros | Cons |
|----------|------|------|
| Push (HTTP to host) | Low latency, immediate commands | Requires host to expose endpoint |
| Poll (host queries Hub) | No inbound firewall rules needed | Latency, polling overhead |
| WebSocket (persistent) | Real-time, single connection | Connection management complexity |

**Decision:** WebSocket-based persistent connection, initiated by the host. This overcomes firewall restrictions (hosts behind NAT can still receive commands) while providing real-time bidirectional communication. See the WebSocket design documentation for connection management details.

### 10.5 WebSocket Message Authentication

**Question:** Once a WebSocket connection is established with HMAC authentication, should individual messages over that connection require per-message authentication?

| Approach | Security | Performance | Complexity |
|----------|----------|-------------|------------|
| **Per-message HMAC** | Highest - each command independently verified | Overhead per message | Higher - must sign/verify each message |
| **Session-based trust** | Connection authenticated once, messages trusted | Minimal overhead | Lower - no per-message crypto |
| **Hybrid** | Critical commands signed, routine messages trusted | Moderate | Medium - classify message criticality |

Arguments for session-based trust:
- WebSocket runs over TLS, providing transport-level integrity
- Initial connection is HMAC-authenticated, establishing host identity
- Connection hijacking would require TLS compromise
- Similar to how SSH trusts commands after key exchange

Arguments for per-message signing:
- Defense in depth against potential TLS vulnerabilities
- Audit trail with cryptographic proof per command
- Protects against compromised Hub process memory

**Decision:** Session-based trust for Hub→Host commands over the established WebSocket connection. The WebSocket is strictly for Hub-initiated commands to hosts (agent provisioning, termination, config updates). Host→Hub requests that require authorization (status updates, API calls) must use standard HMAC-authenticated HTTP requests and should **not** flow over this WebSocket channel. This maintains proper authorization semantics while avoiding per-message overhead for trusted command delivery.

### 10.6 Secret Rotation Trigger

**Question:** What should trigger automatic secret rotation?

- Time-based (every N days)?
- Event-based (security incident, personnel change)?
- Manual only?

**Decision:** Manual rotation with optional time-based auto-rotation. Administrators can trigger rotation on-demand (security incidents, personnel changes), with optional configuration for automatic rotation at defined intervals (e.g., 30 days).

### 10.7 Multi-Hub Support

**Question:** Can a Runtime Host be registered with multiple Hubs?

- If yes, how are secrets managed per-Hub?
- What prevents Hub A from impersonating Hub B?

**Decision:** Single Hub per host for initial implementation, with multi-Hub support planned for the future.

Future multi-Hub design considerations:
- Host ID remains consistent across Hubs (same host identity)
- Each Hub relationship has a unique shared secret
- Credential storage must be keyed by Hub endpoint: `~/.scion/host-credentials/{hub-id}.json`
- Hub impersonation prevented by unique secrets and endpoint verification

Storage systems should be designed to accommodate per-Hub credentials from the start.

### 10.8 Host Deactivation and Cleanup

**Question:** What happens when a host is deactivated?

- Should running agents be terminated immediately?
- How long should the secret be retained for audit purposes?
- Should there be a "quarantine" state before full deletion?

**Decision:** On host deactivation:
1. Running agents are terminated immediately (Hub sends termination commands if host is reachable)
2. Host secret is marked inactive (authentication attempts rejected)
3. Secret hash retained for 30 days for audit trail purposes
4. Full deletion after retention period

### 10.9 Integration with Agent Auth

**Question:** How does host authentication relate to agent token issuance?

When the Hub provisions an agent on a host:
1. Hub authenticates to host via HMAC (or over authenticated WebSocket)
2. Host starts agent container with... what token?

Options:
- Hub sends pre-signed agent JWT in provision request
- Host requests agent JWT from Hub after container starts
- Agent bootstraps its own token via Hub API

**Decision:** Hub includes pre-signed agent JWT in the provision request payload.

Rationale:
- Runtime Hosts operate at a higher trust level than individual agents
- The trust model assumes hosts will not abuse access to agent credentials
- Pre-signed tokens eliminate a round-trip and simplify agent startup
- Agent tokens are scoped to specific agent IDs, limiting blast radius if compromised
- Host compromise is a more serious security event that would be addressed separately

---

## 11. Implementation Checklist

### Phase 1: Core Infrastructure ✓
- [x] Define `HostSecretStore` interface (`pkg/store/store.go`)
- [x] ~~Implement in-memory secret store (dev/testing)~~ (skipped - SQLite with `:memory:` sufficient for testing)
- [x] Implement SQLite secret store (`pkg/store/sqlite/hostsecret.go`)
- [x] Create host registration endpoints (`POST /hosts`, `POST /hosts/join`) (`pkg/hub/handlers_hosts.go`)
- [x] Implement `HostAuthMiddleware` for Hub (`pkg/hub/hostauth.go`)

**Implementation Notes (Phase 1):**
- Timestamp format uses Unix epoch (seconds) rather than RFC 3339 for simpler parsing
- `HostAuthService` provides both middleware and `SignRequest()` helper for clients
- Join tokens use `scion_join_` prefix with base64-encoded random bytes
- Nonce cache is optional and disabled by default (`EnableNonceCache: false`)
- Secret keys are 256-bit (32 bytes) generated via `crypto/rand`
- Database migration V8 adds `host_secrets` and `host_join_tokens` tables with FK cascade delete

### Phase 2: Runtime Host Integration ✓
- [x] Add HMAC signing to `hubclient` package (`pkg/apiclient/hmac.go`, `hubclient.WithHMACAuth()`)
- [x] Implement local credential storage (`pkg/hostcredentials/store.go`)
- [x] Add host-side signature verification (`pkg/runtimehost/hostauth.go`)
- [x] Implement heartbeat/status reporting (`pkg/runtimehost/heartbeat.go`)

**Implementation Notes (Phase 2):**
- `HMACAuth` implements `apiclient.Authenticator` for signing outgoing requests
- `BuildCanonicalString` and `ComputeHMAC` are exported for use by both client and server
- `HostCredentials` stored in `~/.scion/host-credentials.json` with 0600 permissions
- `HeartbeatService` runs background goroutine sending heartbeats at configurable interval (default 30s)
- `HostAuthMiddleware` verifies incoming Hub requests using shared secret
- Server integration loads credentials on startup and configures HMAC auth automatically

### Phase 3: Bidirectional Communication
- [ ] Add Hub→Host HTTP client with HMAC signing
- [ ] Implement host callback endpoints
- [ ] Add agent provisioning flow

### Phase 4: Production Hardening
- [ ] Add nonce cache for replay prevention
- [ ] Implement secret rotation flow
- [ ] Add Google Secret Manager integration
- [ ] Add comprehensive audit logging
- [ ] Add metrics (auth successes/failures, latency)

---

## 12. Related Documents

- [Server Auth Design](server-auth-design.md) - Hub authentication for users and agents
- [Auth Overview](auth-overview.md) - Identity model and token types
- [Agent Authentication](sciontool-auth.md) - Agent-to-Hub JWT
- [Hosted Architecture](../hosted-architecture.md) - System context
