package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/credentials"
	"github.com/ptone/scion-agent/pkg/hub/auth"
	"github.com/ptone/scion-agent/pkg/hubclient"
	"github.com/ptone/scion-agent/pkg/util"
	"github.com/spf13/cobra"
)

var (
	hubAuthHubURL string
)

// hubAuthCmd represents the auth subcommand under hub
var hubAuthCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage Hub authentication",
	Long: `Manage authentication with a Scion Hub.

Commands for logging in, logging out, and checking authentication status.`,
}

// hubAuthLoginCmd authenticates with the Hub
var hubAuthLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Hub server",
	Long: `Authenticate with a Scion Hub server using browser-based OAuth.

This command will:
1. Start a local callback server
2. Open your browser to authenticate with the Hub
3. Wait for the OAuth callback
4. Store credentials locally

Example:
  scion hub auth login
  scion hub auth login --hub-url https://hub.example.com`,
	RunE: runHubAuthLogin,
}

// hubAuthStatusCmd shows authentication status
var hubAuthStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show authentication status",
	Long:  `Show the current authentication status with the Hub.`,
	RunE:  runHubAuthStatus,
}

// hubAuthLogoutCmd clears stored credentials
var hubAuthLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear stored credentials",
	Long:  `Log out from the Hub by clearing locally stored credentials.`,
	RunE:  runHubAuthLogout,
}

func init() {
	hubCmd.AddCommand(hubAuthCmd)
	hubAuthCmd.AddCommand(hubAuthLoginCmd)
	hubAuthCmd.AddCommand(hubAuthStatusCmd)
	hubAuthCmd.AddCommand(hubAuthLogoutCmd)

	// Flags for login command
	hubAuthLoginCmd.Flags().StringVar(&hubAuthHubURL, "hub-url", "", "Hub server URL (defaults to configured endpoint)")
}

func runHubAuthLogin(cmd *cobra.Command, args []string) error {
	// Resolve hub URL
	hubURL := hubAuthHubURL
	if hubURL == "" {
		hubURL = getDefaultHubURL()
	}
	if hubURL == "" {
		return fmt.Errorf("Hub URL not specified. Use --hub-url or configure hub.endpoint in settings")
	}

	fmt.Printf("Authenticating with Hub at %s\n", hubURL)

	// Create hub client (unauthenticated for initial OAuth)
	client, err := hubclient.New(hubURL, hubclient.WithTimeout(30*time.Second))
	if err != nil {
		return fmt.Errorf("failed to create Hub client: %w", err)
	}

	// Start localhost callback server
	authServer := auth.NewLocalhostAuthServer()
	callbackURL, state, err := authServer.Start(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to start auth server: %w", err)
	}
	defer authServer.Shutdown()

	// Get OAuth URL from Hub
	authResp, err := client.Auth().GetAuthURL(cmd.Context(), callbackURL, state)
	if err != nil {
		return fmt.Errorf("failed to get auth URL: %w", err)
	}

	// Open browser
	fmt.Println("Opening browser for authentication...")
	if err := util.OpenBrowser(authResp.URL); err != nil {
		fmt.Printf("\nCould not open browser automatically.\n")
		fmt.Printf("Please open this URL in your browser:\n\n  %s\n\n", authResp.URL)
	}

	// Wait for callback
	fmt.Println("Waiting for authentication...")
	code, err := authServer.WaitForCode(cmd.Context())
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Exchange code for token
	tokenResp, err := client.Auth().ExchangeCode(cmd.Context(), code, callbackURL)
	if err != nil {
		return fmt.Errorf("failed to get token: %w", err)
	}

	// Convert to credentials format
	credToken := &credentials.TokenResponse{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    time.Duration(tokenResp.ExpiresIn) * time.Second,
	}
	if tokenResp.User != nil {
		credToken.User = &credentials.User{
			ID:          tokenResp.User.ID,
			Email:       tokenResp.User.Email,
			DisplayName: tokenResp.User.DisplayName,
		}
	}

	// Store credentials
	if err := credentials.Store(hubURL, credToken); err != nil {
		return fmt.Errorf("failed to store credentials: %w", err)
	}

	fmt.Println("\nAuthentication successful!")
	if credToken.User != nil {
		fmt.Printf("Logged in as: %s (%s)\n", credToken.User.DisplayName, credToken.User.Email)
	}

	return nil
}

func runHubAuthStatus(cmd *cobra.Command, args []string) error {
	hubURL := getDefaultHubURL()
	if hubURL == "" {
		fmt.Println("Hub URL not configured.")
		fmt.Println("Use 'scion hub auth login --hub-url <url>' to authenticate.")
		return nil
	}

	creds, err := credentials.Load(hubURL)
	if err != nil {
		if err == credentials.ErrNotAuthenticated {
			fmt.Println("Not authenticated.")
			fmt.Printf("Run 'scion hub auth login' to authenticate with %s\n", hubURL)
			return nil
		}
		if err == credentials.ErrTokenExpired {
			fmt.Println("Authentication expired.")
			fmt.Printf("Run 'scion hub auth login' to re-authenticate with %s\n", hubURL)
			return nil
		}
		return fmt.Errorf("failed to load credentials: %w", err)
	}

	// Verify token is still valid by making a request
	client, err := hubclient.New(hubURL,
		hubclient.WithBearerToken(creds.AccessToken),
		hubclient.WithTimeout(10*time.Second),
	)
	if err != nil {
		return fmt.Errorf("failed to create Hub client: %w", err)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	user, err := client.Auth().Me(ctx)
	if err != nil {
		fmt.Println("Authentication may have expired or is invalid.")
		fmt.Printf("Run 'scion hub auth login' to re-authenticate.\n")
		return nil
	}

	fmt.Printf("Authenticated as: %s (%s)\n", user.DisplayName, user.Email)
	fmt.Printf("Hub: %s\n", hubURL)
	if !creds.ExpiresAt.IsZero() {
		fmt.Printf("Token expires: %s\n", creds.ExpiresAt.Format(time.RFC3339))
	}

	return nil
}

func runHubAuthLogout(cmd *cobra.Command, args []string) error {
	hubURL := getDefaultHubURL()
	if hubURL == "" {
		fmt.Println("Hub URL not configured. Nothing to logout from.")
		return nil
	}

	if err := credentials.Remove(hubURL); err != nil {
		return fmt.Errorf("failed to remove credentials: %w", err)
	}

	fmt.Println("Logged out successfully.")
	return nil
}

// getDefaultHubURL returns the default Hub URL from settings or environment.
func getDefaultHubURL() string {
	// Check environment first
	if env := os.Getenv("SCION_HUB_ENDPOINT"); env != "" {
		return env
	}

	// Try to load from settings
	grovePath, _, err := config.ResolveGrovePath("")
	if err != nil {
		return ""
	}

	settings, err := config.LoadSettings(grovePath)
	if err != nil {
		return ""
	}

	return settings.GetHubEndpoint()
}
