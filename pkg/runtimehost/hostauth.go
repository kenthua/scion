// Package runtimehost provides the Scion Runtime Host API server.
package runtimehost

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ptone/scion-agent/pkg/apiclient"
)

// HostAuthConfig configures host-side HMAC authentication.
type HostAuthConfig struct {
	// Enabled controls whether authentication is enforced.
	Enabled bool
	// MaxClockSkew is the maximum allowed time difference between client and server.
	MaxClockSkew time.Duration
	// SecretKey is the shared secret for HMAC verification.
	SecretKey []byte
	// AllowUnauthenticated allows requests without HMAC headers to pass through.
	// This is useful for development or when mixing authenticated and unauthenticated endpoints.
	AllowUnauthenticated bool
}

// DefaultHostAuthConfig returns the default host authentication configuration.
func DefaultHostAuthConfig() HostAuthConfig {
	return HostAuthConfig{
		Enabled:              false,
		MaxClockSkew:         5 * time.Minute,
		AllowUnauthenticated: true,
	}
}

// HostAuthMiddleware provides HMAC-based authentication for incoming requests.
// This verifies that requests from the Hub are properly signed.
type HostAuthMiddleware struct {
	config HostAuthConfig
}

// NewHostAuthMiddleware creates a new host authentication middleware.
func NewHostAuthMiddleware(cfg HostAuthConfig) *HostAuthMiddleware {
	return &HostAuthMiddleware{config: cfg}
}

// Middleware returns an HTTP middleware handler that validates HMAC signatures.
func (m *HostAuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.config.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Extract HMAC headers
		hostID := r.Header.Get(apiclient.HeaderHostID)
		timestamp := r.Header.Get(apiclient.HeaderTimestamp)
		nonce := r.Header.Get(apiclient.HeaderNonce)
		signature := r.Header.Get(apiclient.HeaderSignature)

		// If no HMAC headers present, check if unauthenticated requests are allowed
		if hostID == "" && timestamp == "" && signature == "" {
			if m.config.AllowUnauthenticated {
				next.ServeHTTP(w, r)
				return
			}
			m.writeError(w, "missing authentication headers")
			return
		}

		// Validate required headers are all present
		if hostID == "" {
			m.writeError(w, "missing X-Scion-Host-ID header")
			return
		}
		if timestamp == "" {
			m.writeError(w, "missing X-Scion-Timestamp header")
			return
		}
		if signature == "" {
			m.writeError(w, "missing X-Scion-Signature header")
			return
		}

		// Validate timestamp
		ts, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			m.writeError(w, "invalid timestamp format")
			return
		}

		requestTime := time.Unix(ts, 0)
		clockSkew := time.Since(requestTime)
		if clockSkew < 0 {
			clockSkew = -clockSkew
		}
		if clockSkew > m.config.MaxClockSkew {
			m.writeError(w, fmt.Sprintf("timestamp outside acceptable range (skew: %v)", clockSkew))
			return
		}

		// Decode the signature
		sigBytes, err := base64.StdEncoding.DecodeString(signature)
		if err != nil {
			m.writeError(w, "invalid signature encoding")
			return
		}

		// Build canonical string and verify signature
		canonical := apiclient.BuildCanonicalString(r, timestamp, nonce)
		if !apiclient.VerifyHMAC(m.config.SecretKey, canonical, sigBytes) {
			m.writeError(w, "invalid signature")
			return
		}

		// Signature valid, continue to handler
		next.ServeHTTP(w, r)
	})
}

// writeError writes an authentication error response.
func (m *HostAuthMiddleware) writeError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprintf(w, `{"error":{"code":"host_auth_failed","message":%q}}`, message)
}

// UpdateSecretKey updates the secret key used for verification.
// This can be used when credentials are rotated.
func (m *HostAuthMiddleware) UpdateSecretKey(key []byte) {
	m.config.SecretKey = key
}

// SetEnabled enables or disables authentication.
func (m *HostAuthMiddleware) SetEnabled(enabled bool) {
	m.config.Enabled = enabled
}
