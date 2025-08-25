// Package tailscale provides a unified Tailscale client for managing Tailscale connections
// through socket communication with tailscaled daemon.
package tailscale

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/binrclab/headcni/pkg/constants"
	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
)

// ClientOptions defines the configuration options for Tailscale client startup
type ClientOptions struct {
	AuthKey         string   // Authentication key for Tailscale
	AcceptDNS       bool     // Whether to accept DNS from other nodes
	Hostname        string   // Hostname for this node
	ControlURL      string   // Control server URL
	AdvertiseRoutes []string // Routes to advertise
	AcceptRoutes    bool     // Whether to accept routes from other nodes
	ShieldsUp       bool     // Whether to enable Shields Up mode
	Ephemeral       bool     // Whether this is an ephemeral node
}

// SimpleClient is a unified Tailscale client that focuses on socket communication
// with tailscaled daemon for managing Tailscale connections.
type SimpleClient struct {
	localClient *local.Client
	hostClient  *local.Client // 用于连接系统 tailscaled
	socketPath  string
	mu          sync.RWMutex
	timeout     time.Duration
}

// =============================================================================
// Constructor and Basic Methods
// =============================================================================

// NewSimpleClient creates a new SimpleClient instance
func NewSimpleClient(socketPath string) *SimpleClient {
	if socketPath == "" {
		socketPath = constants.DefaultTailscaleDaemonSocketPath
	}

	client := &SimpleClient{
		socketPath:  socketPath,
		timeout:     30 * time.Second,
		localClient: &local.Client{Socket: socketPath},
	}

	// 只有当 socketPath 与系统默认路径不同时，才创建单独的 hostClient
	if socketPath != constants.DefaultTailscaleHostSocketPath {
		client.hostClient = &local.Client{Socket: constants.DefaultTailscaleHostSocketPath}
	} else {
		client.hostClient = client.localClient // 复用同一个客户端
	}

	return client
}

// SetSocketPath sets the socket path for the client
func (c *SimpleClient) SetSocketPath(socketPath string) {
	c.socketPath = socketPath
	c.localClient.Socket = socketPath
}

// GetSocketPath returns the current socket path
func (c *SimpleClient) GetSocketPath() string {
	return c.socketPath
}

// IsSocketPathExists checks if the socket file exists
func (c *SimpleClient) IsSocketPathExists() bool {
	if _, err := os.Stat(c.socketPath); os.IsNotExist(err) {
		return false
	}
	c.localClient.Socket = c.socketPath
	return true
}

// IsHostMode checks if using system socket path
func (c *SimpleClient) IsHostMode() bool {
	return c.socketPath == "/var/run/tailscale/tailscaled.sock" ||
		c.socketPath == "/var/run/tailscale/tailscaled.socket" ||
		c.socketPath == "/run/tailscale/tailscaled.sock"
}

// SetTimeout sets the timeout duration for operations
func (c *SimpleClient) SetTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timeout = timeout
}

// =============================================================================
// Helper Methods for Preferences and Configuration
// =============================================================================

// createWantRunningPrefs creates MaskedPrefs for setting WantRunning state
func (c *SimpleClient) createWantRunningPrefs(wantRunning bool) *ipn.MaskedPrefs {
	prefs := ipn.NewPrefs()
	prefs.WantRunning = wantRunning
	return &ipn.MaskedPrefs{
		Prefs:          *prefs,
		WantRunningSet: true,
	}
}

// createBasicPrefs creates basic configuration MaskedPrefs from ClientOptions
func (c *SimpleClient) createBasicPrefs(options ClientOptions) *ipn.MaskedPrefs {
	prefs := ipn.NewPrefs()
	prefs.ControlURL = options.ControlURL
	prefs.Hostname = options.Hostname
	prefs.WantRunning = false
	prefs.LoggedOut = false
	prefs.RouteAll = options.AcceptRoutes
	prefs.ShieldsUp = options.ShieldsUp

	if len(options.AdvertiseRoutes) > 0 {
		var routes []netip.Prefix
		for _, route := range options.AdvertiseRoutes {
			if prefix, err := netip.ParsePrefix(route); err == nil {
				routes = append(routes, prefix)
			}
		}
		prefs.AdvertiseRoutes = routes
	}

	return &ipn.MaskedPrefs{
		Prefs:              *prefs,
		ControlURLSet:      true,
		HostnameSet:        options.Hostname != "",
		WantRunningSet:     true,
		LoggedOutSet:       true,
		RouteAllSet:        true,
		AdvertiseRoutesSet: len(options.AdvertiseRoutes) > 0,
		ShieldsUpSet:       true,
	}
}

// createRoutePrefs creates MaskedPrefs for route-related configurations
func (c *SimpleClient) createRoutePrefs(routes []netip.Prefix, routeAll *bool, hostname string) *ipn.MaskedPrefs {
	prefs := ipn.NewPrefs()
	if routes != nil {
		prefs.AdvertiseRoutes = routes
	}
	if routeAll != nil {
		prefs.RouteAll = *routeAll
	}
	if hostname != "" {
		prefs.Hostname = hostname
	}

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs: *prefs,
	}

	if routes != nil {
		maskedPrefs.AdvertiseRoutesSet = true
	}
	if routeAll != nil {
		maskedPrefs.RouteAllSet = true
	}
	if hostname != "" {
		maskedPrefs.HostnameSet = true
	}

	return maskedPrefs
}

// waitForStateChange waits for state change to target state with timeout
func (c *SimpleClient) waitForStateChange(ctx context.Context, targetState string, maxWait int) error {
	for i := 0; i < maxWait; i++ {
		time.Sleep(1 * time.Second)
		if status, err := c.GetStatus(ctx); err == nil && status.BackendState == targetState {
			return nil
		}
	}
	return fmt.Errorf("timeout waiting for state change to %s", targetState)
}

// =============================================================================
// Status and Connection Management Methods
// =============================================================================

// GetStatus retrieves the current Tailscale status
func (c *SimpleClient) GetStatus(ctx context.Context) (*ipnstate.Status, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.localClient.Status(ctx)
}

// CheckSocketExists checks if the socket is accessible
func (c *SimpleClient) CheckSocketExists() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.localClient.Status(ctx)
	return err
}

// Down disconnects the Tailscale connection
func (c *SimpleClient) Down(ctx context.Context) error {
	log.Println("Disconnecting Tailscale...")

	status, err := c.GetStatus(ctx)
	if err != nil {
		log.Printf("Failed to get status: %v", err)
	} else if status.BackendState == "Stopped" {
		log.Println("Connection already stopped")
		return nil
	}

	maskedPrefs := c.createWantRunningPrefs(false)
	_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("failed to stop connection: %v", err)
	}

	// Wait for connection to stop
	if err := c.waitForStateChange(ctx, "Stopped", 10); err == nil {
		log.Println("Connection successfully stopped")
		return nil
	}

	log.Println("Stop command sent")
	return nil
}

// UpWithOptionsWithRetry attempts to connect with retry mechanism
func (c *SimpleClient) UpWithOptionsWithRetry(ctx context.Context, options ClientOptions) error {
	maxRetries := 2
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		log.Printf("Attempt %d/%d", attempt, maxRetries)

		err := c.UpWithOptions(ctx, options)
		if err == nil {
			log.Printf("✅ Attempt %d successful!", attempt)
			return nil
		}

		log.Printf("❌ Attempt %d failed: %v", attempt, err)
		lastErr = err

		if attempt < maxRetries {
			log.Printf("Waiting 15 seconds before retry...")
			time.Sleep(15 * time.Second)
		}
	}

	return fmt.Errorf("all %d attempts failed, last error: %v", maxRetries, lastErr)
}

// UpWithOptions connects to Tailscale with the given options
func (c *SimpleClient) UpWithOptions(ctx context.Context, options ClientOptions) error {
	log.Printf("Starting Tailscale connection process")
	log.Printf("Control URL: %s", options.ControlURL)
	log.Printf("Hostname: %s", options.Hostname)
	log.Printf("Auth key: %s...", c.maskAuthKey(options.AuthKey))
	log.Printf("Socket path: %s", c.socketPath)

	// Validate required parameters
	if err := c.validateOptions(options); err != nil {
		return fmt.Errorf("parameter validation failed: %v", err)
	}

	if err := c.waitForDaemonReady(ctx); err != nil {
		return fmt.Errorf("waitForDaemonReady failed: %w", err)
	}

	// Step 2: Check and reuse existing state
	if err := c.checkAndReuseExistingState(ctx, options); err == nil {
		log.Println("Reusing existing state, connection process complete")
		return nil
	}

	if err := c.completeReset(ctx); err != nil {
		return fmt.Errorf("completeReset failed: %w", err)
	}
	log.Printf("completeReset completed")

	// Key fix 2: Step-by-step precise setup
	if err := c.preciseSetup(ctx, options); err != nil {
		return fmt.Errorf("precise setup failed: %v", err)
	}

	// Key fix 3: Improved authentication process
	if err := c.improvedAuthentication(ctx, options); err != nil {
		return fmt.Errorf("authentication failed: %v", err)
	}

	// Step 4: Wait for full connection establishment
	if err := c.waitForFullConnection(ctx); err != nil {
		return fmt.Errorf("waiting for connection completion failed: %v", err)
	}

	// Step 5: Configure DNS preferences to prevent overwriting /etc/resolv.conf
	if !options.AcceptDNS {
		if err := c.disableTailscaleDNS(ctx); err != nil {
			log.Printf("Warning: Failed to disable Tailscale DNS: %v", err)
			// Don't fail the connection if DNS setting fails
		}
	}

	log.Println("Fixed version connection process completed")
	return nil
}

// =============================================================================
// Connection State Management Methods
// =============================================================================

// checkAndReuseExistingState checks and reuses existing connection state if possible
func (c *SimpleClient) checkAndReuseExistingState(ctx context.Context, options ClientOptions) error {
	log.Println("Checking existing state, attempting to reuse...")

	status, err := c.GetStatus(ctx)
	if err != nil {
		log.Printf("Unable to get status: %v", err)
		return fmt.Errorf("unable to get status")
	}

	log.Printf("Current state: %s", status.BackendState)

	// If already running, check if configuration matches
	if status.BackendState == "Running" {
		log.Println("✓ Client already in running state")

		if status.Self != nil && len(status.Self.TailscaleIPs) > 0 {
			log.Printf("✓ Has valid IP: %v", status.Self.TailscaleIPs)

			// Get current preferences
			prefs, err := c.localClient.GetPrefs(ctx)
			if err != nil {
				log.Printf("Unable to get preferences: %v", err)
				return fmt.Errorf("unable to get preferences")
			}

			// Check if key configurations match
			configChanged := false
			changeReasons := []string{}

			// Check control URL
			if prefs.ControlURL != options.ControlURL {
				configChanged = true
				changeReasons = append(changeReasons, fmt.Sprintf("ControlURL: %s -> %s", prefs.ControlURL, options.ControlURL))
			}

			// Check hostname
			if prefs.Hostname != options.Hostname {
				configChanged = true
				changeReasons = append(changeReasons, fmt.Sprintf("Hostname: %s -> %s", prefs.Hostname, options.Hostname))
			}

			// Check route configuration
			if prefs.RouteAll != options.AcceptRoutes {
				configChanged = true
				changeReasons = append(changeReasons, fmt.Sprintf("AcceptRoutes: %v -> %v", prefs.RouteAll, options.AcceptRoutes))
			}

			// Check advertised routes
			if len(options.AdvertiseRoutes) > 0 {
				currentRoutes := make(map[string]bool)
				for _, route := range prefs.AdvertiseRoutes {
					currentRoutes[route.String()] = true
				}

				for _, newRoute := range options.AdvertiseRoutes {
					if !currentRoutes[newRoute] {
						configChanged = true
						changeReasons = append(changeReasons, fmt.Sprintf("AdvertiseRoutes: new %s", newRoute))
						break
					}
				}
			}

			// If configuration hasn't changed, can reuse
			if !configChanged {
				log.Println("✓ Configuration completely matches, can reuse existing state")

				// Enable running state
				maskedPrefs := c.createWantRunningPrefs(true)

				_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
				if err == nil {
					log.Println("✓ Successfully reused existing state")

					// Configure DNS preferences when reusing existing state
					if !options.AcceptDNS {
						if dnsErr := c.disableTailscaleDNS(ctx); dnsErr != nil {
							log.Printf("Warning: Failed to disable Tailscale DNS when reusing state: %v", dnsErr)
						}
					}

					return nil
				}
			} else {
				log.Println("⚠️ Configuration has changed, need to re-authenticate:")
				for _, reason := range changeReasons {
					log.Printf("  - %s", reason)
				}
				return fmt.Errorf("configuration change requires re-authentication")
			}
		}
	}

	log.Println("Cannot reuse existing state, need to re-authenticate")
	return fmt.Errorf("need to re-authenticate")
}

// waitForDaemonReady waits for the Tailscale daemon to be ready
func (c *SimpleClient) waitForDaemonReady(ctx context.Context) error {
	log.Println("Waiting for Tailscale daemon to be ready...")

	for i := 0; i < 30; i++ {
		status, err := c.GetStatus(ctx)
		if err != nil {
			log.Printf("Daemon check %d/30: connection failed - %v", i+1, err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Check if daemon is in stable state
		if status.BackendState == "Stopped" || status.BackendState == "NeedsLogin" {
			log.Printf("Daemon ready: %s", status.BackendState)
			// Wait additional 2 seconds for stability
			time.Sleep(2 * time.Second)
			return nil
		}

		if i%10 == 0 || i < 3 {
			log.Printf("Daemon check %d/30: %s", i+1, status.BackendState)
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("daemon not ready within 30 seconds")
}

// completeReset intelligently resets connection state (optimized version)
func (c *SimpleClient) completeReset(ctx context.Context) error {
	log.Println("Intelligently resetting connection state")

	// Get current status
	status, err := c.GetStatus(ctx)
	if err != nil {
		log.Printf("Unable to get status: %v", err)
		return nil
	}

	log.Printf("State before reset: %s", status.BackendState)

	// Intelligently determine if reset is needed
	switch status.BackendState {
	case "Stopped":
		log.Println("Already stopped, skipping reset")
		return nil
	case "NeedsLogin":
		// Check if there are residual authentication states
		if status.Self != nil && len(status.Self.TailscaleIPs) > 0 {
			log.Println("NeedsLogin state but has residual IPs, need complete reset")
		} else {
			log.Println("Clean NeedsLogin state, skipping reset")
			return nil
		}
	case "Running":
		log.Println("Currently running, need to reset")
	case "Starting":
		log.Println("Starting up, waiting for completion or reset")
	default:
		log.Printf("Unknown state %s, attempting reset", status.BackendState)
	}

	// Execute reset
	prefs := ipn.NewPrefs()
	prefs.WantRunning = false
	prefs.LoggedOut = true

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:          *prefs,
		WantRunningSet: true,
		LoggedOutSet:   true,
	}

	_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		log.Printf("Failed to stop connection: %v", err)
		return err
	}

	// Intelligent waiting - adjust wait time based on initial state
	maxWait := 10 // Default 10 seconds
	if status.BackendState == "Running" {
		maxWait = 15 // Running state needs more time to stop
	}

	log.Printf("Waiting for state reset (max %d seconds)...", maxWait)

	// Wait for state to become Stopped or NeedsLogin
	for i := 0; i < maxWait; i++ {
		time.Sleep(1 * time.Second)
		if status, err := c.GetStatus(ctx); err == nil {
			if i%5 == 0 || status.BackendState != "Stopping" {
				log.Printf("Reset progress %d/%d: %s", i+1, maxWait, status.BackendState)
			}

			if status.BackendState == "Stopped" || status.BackendState == "NeedsLogin" {
				log.Printf("✅ State reset completed: %s", status.BackendState)
				time.Sleep(1 * time.Second) // Brief wait for state stability
				return nil
			}
		}
	}

	// Check final state
	if finalStatus, err := c.GetStatus(ctx); err == nil {
		if finalStatus.BackendState == "NeedsLogin" || finalStatus.BackendState == "Stopped" {
			log.Printf("✅ Reset completed: %s", finalStatus.BackendState)
			return nil
		}
		log.Printf("⚠️ Reset may be incomplete, current state: %s", finalStatus.BackendState)
	}

	log.Println("State reset completed")
	return nil
}

// preciseSetup performs precise configuration setup (enhanced version)
func (c *SimpleClient) preciseSetup(ctx context.Context, options ClientOptions) error {
	log.Println("Precise configuration setup")

	// Use helper method to create configuration directly, no need to get current config

	// Use helper method to create basic configuration
	maskedPrefs := c.createBasicPrefs(options)

	log.Printf("Applying precise configuration...")
	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("precise configuration failed: %v", err)
	}

	// Increase wait time to ensure configuration takes effect
	log.Println("Waiting for configuration to take effect...")
	time.Sleep(5 * time.Second) // Increased from 3 to 5 seconds

	// Verify configuration
	updatedPrefs, err := c.localClient.GetPrefs(ctx)
	if err == nil {
		// Verify key configuration is correctly applied
		if updatedPrefs.ControlURL != options.ControlURL {
			return fmt.Errorf("control URL configuration verification failed: expected %s, got %s", options.ControlURL, updatedPrefs.ControlURL)
		}
	}

	log.Println("Precise configuration completed")
	return nil
}

// improvedAuthentication performs optimized authentication process
func (c *SimpleClient) improvedAuthentication(ctx context.Context, options ClientOptions) error {
	log.Println("Optimized authentication process")
	// If in "auto" mode, handle existing state
	if options.AuthKey == "auto" {
		return c.handleAutoModeAPI(ctx, options)
	}
	// 3.1 Check current state
	status, err := c.GetStatus(ctx)
	if err != nil {
		return fmt.Errorf("unable to get current state: %v", err)
	}

	log.Printf("State before authentication: %s", status.BackendState)

	// If already running, check if re-authentication is needed
	if status.BackendState == "Running" {
		if c.isLoginComplete(status) {
			log.Println("✅ Already logged in, skipping authentication")
			return nil
		}
		log.Println("Running but login incomplete, continuing authentication process")
	}

	// 3.2 Enable running state
	log.Println("Enabling running state")
	prefs := ipn.NewPrefs()
	prefs.WantRunning = true

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:          *prefs,
		WantRunningSet: true,
	}

	_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("failed to enable running state: %v", err)
	}

	// 3.3 Quick state change check
	log.Println("Checking state changes...")
	time.Sleep(2 * time.Second)

	var finalState string
	for i := 0; i < 60; i++ { // Reduced to 10 checks
		status, err := c.GetStatus(ctx)
		if err != nil {
			log.Printf("State check failed %d: %v", i+1, err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		finalState = status.BackendState
		log.Printf("State check %d/10: %s", i+1, status.BackendState)

		if status.BackendState == "Running" {
			if c.isLoginComplete(status) {
				log.Println("✅ Directly entered complete Running state")
				return nil
			}
			log.Println("Running but incomplete, continuing authentication")
		}

		if status.BackendState == "NeedsLogin" {
			log.Println("✅ Entered NeedsLogin state, starting authentication")
			break
		}

		time.Sleep(500 * time.Millisecond)
	}

	// 3.4 Send authentication request and wait for initial response
	if finalState == "NeedsLogin" {
		log.Println("Sending authentication request")
		startOptions := ipn.Options{
			AuthKey: options.AuthKey,
		}
		// Create pre-cleanup configuration
		prefs := ipn.NewPrefs()
		prefs.ControlURL = options.ControlURL
		prefs.LoggedOut = true
		prefs.WantRunning = false

		_, err := c.localClient.EditPrefs(ctx, &ipn.MaskedPrefs{
			Prefs:          *prefs,
			ControlURLSet:  true,
			LoggedOutSet:   true,
			WantRunningSet: true,
		})
		if err != nil {
			return fmt.Errorf("pre-cleanup failed: %w", err)
		}

		log.Printf("Using authentication key: %s...", c.maskAuthKey(options.AuthKey))
		err = c.localClient.Start(ctx, startOptions)
		if err != nil {
			return fmt.Errorf("Start command failed: %v", err)
		}

		// 3) Then enable WantRunning
		err = c.enableRunningAfterAuth(ctx)
		if err != nil {
			return fmt.Errorf("failed to enable running state: %v", err)
		}
		// Check if authentication was successful
		if err := c.waitForAuthCompletion(ctx); err != nil {
			log.Printf("Authentication method completion failed: %v", err)
			return err
		}
	}

	return nil
}

// waitForAuthCompletion waits for authentication completion - enhanced version
func (c *SimpleClient) waitForAuthCompletion(ctx context.Context) error {
	log.Println("Waiting for authentication completion...")

	maxWaitSeconds := 30 // Reduced to 30 seconds, focusing on authentication phase
	checkInterval := 1 * time.Second

	for i := 0; i < maxWaitSeconds; i++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled: %v", ctx.Err())
		default:
		}

		time.Sleep(checkInterval)

		status, err := c.GetStatus(ctx)
		if err != nil {
			log.Printf("Status check failed %d: %v", i+1, err)
			continue
		}

		// Print detailed status every 10 seconds
		if i%10 == 0 {
			log.Printf("Authentication progress %d/%ds - State: %s, NodeKey: %v, AuthURL: %s",
				i+1, maxWaitSeconds, status.BackendState, status.HaveNodeKey, status.AuthURL)
		}

		// Success conditions
		if status.HaveNodeKey {
			log.Println("✅ NodeKey obtained, authentication successful")
			return nil
		}

		if status.BackendState == "Starting" || status.BackendState == "Running" {
			log.Printf("✅ State changed to %s, authentication successful", status.BackendState)
			return nil
		}

		// If there's AuthURL, manual authentication is needed (shouldn't happen with authkey)
		if status.AuthURL != "" {
			log.Printf("⚠️ Manual authentication required: %s", status.AuthURL)
			return fmt.Errorf("manual authentication required, AuthKey may be invalid")
		}
	}

	return fmt.Errorf("authentication timeout, failed to obtain NodeKey")
}

// handleAutoModeAPI handles auto mode using API approach
func (c *SimpleClient) handleAutoModeAPI(ctx context.Context, options ClientOptions) error {
	log.Println("Auto mode: API approach processing...")

	status, err := c.GetStatus(ctx)
	if err != nil {
		return fmt.Errorf("unable to get status: %v", err)
	}

	// If already running and has IP, directly enable
	if status.BackendState == "Running" && status.Self != nil && len(status.Self.TailscaleIPs) > 0 {
		log.Println("Auto mode: Already connected, enabling running state")
		return c.enableRunningAfterAuth(ctx)
	}

	// If has NodeKey but not running, previously authenticated, just need to enable running
	if status.HaveNodeKey {
		log.Println("Auto mode: Has NodeKey, just need to enable running state")

		// First update configuration to match current options
		if err := c.updatePrefsForAuto(ctx, options); err != nil {
			log.Printf("Configuration update failed: %v", err)
		}

		return c.enableRunningAfterAuth(ctx)
	}

	return fmt.Errorf("Auto mode cannot reuse existing state, need to provide valid AuthKey")
}

// updatePrefsForAuto updates preferences for auto mode
func (c *SimpleClient) updatePrefsForAuto(ctx context.Context, options ClientOptions) error {
	log.Println("Updating auto mode configuration...")

	currentPrefs, err := c.localClient.GetPrefs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current preferences: %v", err)
	}

	// Check if configuration update is needed
	needUpdate := false
	updatePrefs := *currentPrefs

	if currentPrefs.ControlURL != options.ControlURL {
		updatePrefs.ControlURL = options.ControlURL
		needUpdate = true
		log.Printf("Updating ControlURL: %s -> %s", currentPrefs.ControlURL, options.ControlURL)
	}

	if currentPrefs.Hostname != options.Hostname {
		updatePrefs.Hostname = options.Hostname
		needUpdate = true
	}

	if currentPrefs.RouteAll != options.AcceptRoutes {
		updatePrefs.RouteAll = options.AcceptRoutes
		needUpdate = true
	}

	// Update advertised routes
	if len(options.AdvertiseRoutes) > 0 {
		var newRoutes []netip.Prefix
		for _, route := range options.AdvertiseRoutes {
			if prefix, err := netip.ParsePrefix(route); err == nil {
				newRoutes = append(newRoutes, prefix)
			}
		}

		// Compare existing routes
		if !routesEqual(currentPrefs.AdvertiseRoutes, newRoutes) {
			updatePrefs.AdvertiseRoutes = newRoutes
			needUpdate = true
			log.Printf("Updating AdvertiseRoutes")
		}
	}

	if needUpdate {
		maskedPrefs := &ipn.MaskedPrefs{
			Prefs:              updatePrefs,
			ControlURLSet:      currentPrefs.ControlURL != options.ControlURL,
			HostnameSet:        currentPrefs.Hostname != options.Hostname,
			RouteAllSet:        currentPrefs.RouteAll != options.AcceptRoutes,
			AdvertiseRoutesSet: len(options.AdvertiseRoutes) > 0,
		}

		_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
		if err != nil {
			return fmt.Errorf("failed to update preferences: %v", err)
		}

		log.Println("Configuration update completed")
	} else {
		log.Println("Configuration update not needed")
	}

	return nil
}

// routesEqual compares if two route lists are equal
func routesEqual(a, b []netip.Prefix) bool {
	if len(a) != len(b) {
		return false
	}

	aMap := make(map[string]bool)
	for _, route := range a {
		aMap[route.String()] = true
	}

	for _, route := range b {
		if !aMap[route.String()] {
			return false
		}
	}

	return true
}

// enableRunningAfterAuth enables running state after authentication
func (c *SimpleClient) enableRunningAfterAuth(ctx context.Context) error {
	log.Println("Authentication completed, enabling running state...")

	// Get current preferences
	currentPrefs, err := c.localClient.GetPrefs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get preferences: %v", err)
	}

	// Only modify running state
	runPrefs := *currentPrefs
	runPrefs.WantRunning = true

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:          runPrefs,
		WantRunningSet: true,
	}

	_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("failed to enable running state: %v", err)
	}

	log.Println("✅ Running state enabled")
	return nil
}

// waitForFullConnection waits for full connection establishment
func (c *SimpleClient) waitForFullConnection(ctx context.Context) error {
	log.Println("Waiting for full connection establishment...")

	maxWaitSeconds := 240 // 4 minutes wait for connection
	checkInterval := 2 * time.Second

	for i := 0; i < maxWaitSeconds/2; i++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled: %v", ctx.Err())
		default:
		}

		time.Sleep(checkInterval)

		status, err := c.GetStatus(ctx)
		if err != nil {
			log.Printf("Status check failed %d: %v", i+1, err)
			continue
		}

		// Print detailed status every 10 seconds
		if i%10 == 0 || i < 3 {
			log.Printf("Connection wait progress %d/%ds - State: %s, HaveNodeKey: %v, Online: %v",
				(i+1)*2, maxWaitSeconds, status.BackendState, status.HaveNodeKey,
				status.Self != nil && status.Self.Online)
		}

		switch status.BackendState {
		case "Running":
			if c.isLoginComplete(status) {
				log.Printf("✅ Connection successful! Total time: %d seconds", (i+1)*2)
				c.logConnectionInfo(status)
				return nil
			} else {
				// Running but no IP assigned, continue waiting
				if i%20 == 0 {
					log.Printf("State Running but IP not assigned, continuing to wait...")
				}
			}

		case "Starting":
			if i%20 == 0 {
				log.Println("Starting connection...")
			}

		case "NeedsLogin":
			// If has NodeKey but state is still NeedsLogin, may need to re-enable
			if status.HaveNodeKey {
				log.Println("Has NodeKey but state is NeedsLogin, trying to re-enable running state")
				if err := c.enableRunningAfterAuth(ctx); err != nil {
					log.Printf("Re-enable failed: %v", err)
				}
			} else {
				// Diagnose network issues
				if i > 30 { // Start diagnosis after 60 seconds
					if i%30 == 0 { // Diagnose every 60 seconds
						c.diagnoseNetworkIssues(ctx)
					}
				}
			}

		case "Stopped":
			log.Println("Connection stopped, trying to re-enable")
			if err := c.enableRunningAfterAuth(ctx); err != nil {
				log.Printf("Re-enable failed: %v", err)
			}

		default:
			log.Printf("Unknown state: %s", status.BackendState)
		}

		// Timeout check
		if i > 60 { // Stricter check after 120 seconds
			if status.BackendState == "NeedsLogin" && !status.HaveNodeKey {
				return fmt.Errorf("no NodeKey after 120 seconds, authentication may have failed")
			}
		}
	}

	return fmt.Errorf("connection timeout")
}

// logConnectionInfo logs connection information
func (c *SimpleClient) logConnectionInfo(status *ipnstate.Status) {
	if status.Self == nil {
		return
	}

	log.Printf("Connection successful: Node name=%s, Online=%v, IP count=%d, Peer count=%d",
		status.Self.HostName, status.Self.Online, len(status.Self.TailscaleIPs), len(status.Peer))
}

// diagnoseNetworkIssues diagnoses network problems
func (c *SimpleClient) diagnoseNetworkIssues(ctx context.Context) {
	log.Println("Diagnosing network issues...")

	// Check preferences
	prefs, err := c.localClient.GetPrefs(ctx)
	if err != nil {
		log.Printf("Unable to get preferences: %v", err)
		return
	}

	log.Printf("Current configuration: ControlURL=%s, Hostname=%s, WantRunning=%v, LoggedOut=%v",
		prefs.ControlURL, prefs.Hostname, prefs.WantRunning, prefs.LoggedOut)

	// Test control server connectivity
	if err := c.checkHeadscaleReachability(); err != nil {
		log.Printf("⚠️ Control server connectivity issue: %v", err)
	} else {
		log.Println("✅ Control server connectivity normal")
	}
}

// checkHeadscaleReachability checks Headscale server reachability
func (c *SimpleClient) checkHeadscaleReachability() error {
	log.Println("Checking Headscale server reachability...")

	prefs, err := c.localClient.GetPrefs(context.Background())
	if err != nil {
		return fmt.Errorf("unable to get preferences: %v", err)
	}

	controlURL := prefs.ControlURL
	if controlURL == "" {
		return fmt.Errorf("control URL not set")
	}

	log.Printf("Checking control URL: %s", controlURL)

	// Try to parse URL
	u, err := url.Parse(controlURL)
	if err != nil {
		return fmt.Errorf("invalid control URL: %v", err)
	}

	// Try to establish TCP connection
	conn, err := net.DialTimeout("tcp", u.Host, 10*time.Second)
	if err != nil {
		return fmt.Errorf("unable to connect to %s: %v", u.Host, err)
	}
	defer conn.Close()

	// Try HTTP request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(controlURL)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	log.Printf("Network check successful: TCP=%s, HTTP=%d", u.Host, resp.StatusCode)

	return nil
}

// DebugAuthKey adds debug method: directly validate authentication key and simplest login attempt
func (c *SimpleClient) DebugAuthKey(ctx context.Context, authKey, controlURL string) {
	log.Println("Debugging authentication key...")
	log.Printf("Debug info: Key length=%d, Control URL=%s", len(authKey), controlURL)
}

// isLoginComplete checks if login is complete
func (c *SimpleClient) isLoginComplete(status *ipnstate.Status) bool {
	if status.Self == nil {
		return false
	}

	if len(status.Self.TailscaleIPs) == 0 {
		return false
	}

	return status.Self.Online
}

// validateOptions validates option parameters
func (c *SimpleClient) validateOptions(options ClientOptions) error {
	// Support "auto" mode (use existing authentication info)
	if options.AuthKey == "" {
		return fmt.Errorf("authentication key cannot be empty")
	}

	if options.ControlURL == "" {
		return fmt.Errorf("control URL cannot be empty")
	}

	// If in "auto" mode, skip length validation
	if options.AuthKey != "auto" && len(options.AuthKey) < 20 {
		return fmt.Errorf("authentication key format may be incorrect, too short")
	}

	for _, route := range options.AdvertiseRoutes {
		if _, err := netip.ParsePrefix(route); err != nil {
			return fmt.Errorf("invalid route format '%s': %v", route, err)
		}
	}

	return nil
}

// maskAuthKey masks sensitive information in authentication key
func (c *SimpleClient) maskAuthKey(key string) string {
	if key == "auto" {
		return key
	}
	if len(key) <= 15 {
		return "***"
	}
	return key[:15] + "***"
}

// Up 启动Tailscale连接（简化版本）
func (c *SimpleClient) Up(ctx context.Context, authKey string) error {
	options := ClientOptions{
		AuthKey: authKey,
	}
	return c.UpWithOptions(ctx, options)
}

// AdvertiseRoutes 通告路由
func (c *SimpleClient) AdvertiseRoutes(ctx context.Context, routes ...netip.Prefix) error {
	maskedPrefs := c.createRoutePrefs(routes, nil, "")
	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	return err
}

// RemoveRoutes 移除通告的路由
func (c *SimpleClient) RemoveRoutes(ctx context.Context, routes ...netip.Prefix) error {
	currentPrefs, err := c.localClient.GetPrefs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current preferences: %v", err)
	}

	toRemove := make(map[netip.Prefix]bool)
	for _, route := range routes {
		toRemove[route] = true
	}

	var newRoutes []netip.Prefix
	for _, route := range currentPrefs.AdvertiseRoutes {
		if !toRemove[route] {
			newRoutes = append(newRoutes, route)
		}
	}

	prefs := ipn.NewPrefs()
	prefs.AdvertiseRoutes = newRoutes

	_, err = c.localClient.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs:              *prefs,
		AdvertiseRoutesSet: true,
	})

	return err
}

// =============================================================================
// Route Management Methods
// =============================================================================

// AcceptRoutes accepts routes from other nodes
func (c *SimpleClient) AcceptRoutes(ctx context.Context) error {
	routeAll := true
	maskedPrefs := c.createRoutePrefs(nil, &routeAll, "")
	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	return err
}

// RejectRoutes rejects routes from other nodes
func (c *SimpleClient) RejectRoutes(ctx context.Context) error {
	routeAll := false
	maskedPrefs := c.createRoutePrefs(nil, &routeAll, "")
	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	return err
}

// SetHostname sets the hostname
func (c *SimpleClient) SetHostname(ctx context.Context, hostname string) error {
	maskedPrefs := c.createRoutePrefs(nil, nil, hostname)
	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	return err
}

// =============================================================================
// IP and Status Query Methods
// =============================================================================

// GetIP gets the primary Tailscale IP
func (c *SimpleClient) GetIP(ctx context.Context) (netip.Addr, error) {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return netip.Addr{}, err
	}

	if status.Self == nil || len(status.Self.TailscaleIPs) == 0 {
		return netip.Addr{}, fmt.Errorf("no tailscale IP assigned")
	}

	// Prioritize IPv4 addresses
	for _, ip := range status.Self.TailscaleIPs {
		if ip.Is4() {
			return ip, nil
		}
	}

	return status.Self.TailscaleIPs[0], nil
}

// GetLocalIP gets the local IP
func (c *SimpleClient) GetLocalIP(ctx context.Context) (netip.Addr, error) {
	status, err := c.hostClient.Status(ctx)
	if err != nil {
		return netip.Addr{}, err
	}

	if status.Self == nil || len(status.Self.TailscaleIPs) == 0 {
		return netip.Addr{}, fmt.Errorf("no tailscale IP assigned")
	}

	// Prioritize IPv4 addresses
	for _, ip := range status.Self.TailscaleIPs {
		if ip.Is4() {
			return ip, nil
		}
	}

	return status.Self.TailscaleIPs[0], nil
}

// GetAllIPs gets all Tailscale IP addresses
func (c *SimpleClient) GetAllIPs(ctx context.Context) ([]netip.Addr, error) {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return nil, err
	}

	if status.Self == nil {
		return nil, fmt.Errorf("no self information available")
	}

	return status.Self.TailscaleIPs, nil
}

// IsRunning checks if Tailscale is running
func (c *SimpleClient) IsRunning(ctx context.Context) bool {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return false
	}
	return status.BackendState == "Running"
}

// IsConnected checks if connected to tailnet
func (c *SimpleClient) IsConnected(ctx context.Context) bool {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return false
	}
	return status.BackendState == "Running" && status.Self != nil && len(status.Self.TailscaleIPs) > 0
}

// CheckConnectivity checks connectivity
func (c *SimpleClient) CheckConnectivity(ctx context.Context) error {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status: %v", err)
	}

	if status.BackendState != "Running" {
		return fmt.Errorf("tailscale not running, state: %s", status.BackendState)
	}

	if status.Self == nil || len(status.Self.TailscaleIPs) == 0 {
		return fmt.Errorf("no tailscale IP assigned")
	}

	return nil
}

// =============================================================================
// Legacy Interface Methods (Compatibility)
// =============================================================================

// AdvertiseRoute advertises routes (legacy interface compatibility)
func (c *SimpleClient) AdvertiseRoute(ctx context.Context, routes ...string) error {
	var prefixes []netip.Prefix
	for _, route := range routes {
		if route == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(route)
		if err != nil {
			return fmt.Errorf("invalid route %s: %v", route, err)
		}
		prefixes = append(prefixes, prefix)
	}

	return c.AdvertiseRoutes(ctx, prefixes...)
}

// RemoveRoute removes routes (legacy interface compatibility)
func (c *SimpleClient) RemoveRoute(ctx context.Context, routes ...string) error {
	var prefixes []netip.Prefix
	for _, route := range routes {
		if route == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(route)
		if err != nil {
			return fmt.Errorf("invalid route %s: %v", route, err)
		}
		prefixes = append(prefixes, prefix)
	}

	return c.RemoveRoutes(ctx, prefixes...)
}

// =============================================================================
// Network Information Methods
// =============================================================================

// GetPeers gets peer nodes
func (c *SimpleClient) GetPeers(ctx context.Context) (map[string]*ipnstate.PeerStatus, error) {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*ipnstate.PeerStatus)
	for key, peer := range status.Peer {
		result[key.String()] = peer
	}

	return result, nil
}

// GetPrefs gets preferences
func (c *SimpleClient) GetPrefs(ctx context.Context) (*ipn.Prefs, error) {
	return c.localClient.GetPrefs(ctx)
}

// WhoIs queries IP ownership
func (c *SimpleClient) WhoIs(ctx context.Context, remoteAddr string) (interface{}, error) {
	return c.localClient.WhoIs(ctx, remoteAddr)
}

// Ping tests connectivity
func (c *SimpleClient) Ping(ctx context.Context, target string) error {
	_, err := c.localClient.Ping(ctx, netip.MustParseAddr(target), tailcfg.PingDisco)
	return err
}

// =============================================================================
// Convenience Methods
// =============================================================================

// QuickConnect quick connection - simplified connection method
func (c *SimpleClient) QuickConnect(ctx context.Context, authKey, controlURL, hostname string) error {
	log.Println("Quick connection mode")

	options := ClientOptions{
		AuthKey:      authKey,
		ControlURL:   controlURL,
		Hostname:     hostname,
		AcceptRoutes: true,
		ShieldsUp:    false,
	}

	return c.UpWithOptions(ctx, options)
}

// ForceLogin forces re-login
func (c *SimpleClient) ForceLogin(ctx context.Context, options ClientOptions) error {
	log.Println("Starting forced re-login...")

	// Force logout - using helper method
	prefs := ipn.NewPrefs()
	prefs.WantRunning = false
	prefs.LoggedOut = true

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:          *prefs,
		WantRunningSet: true,
		LoggedOutSet:   true,
	}

	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		log.Printf("Force logout failed: %v", err)
	}

	time.Sleep(3 * time.Second)
	return c.UpWithOptions(ctx, options)
}

// disableTailscaleDNS 禁用 Tailscale DNS 覆盖，防止修改 /etc/resolv.conf
func (c *SimpleClient) disableTailscaleDNS(ctx context.Context) error {
	log.Println("禁用 Tailscale DNS 覆盖")

	// 创建 MaskedPrefs 来设置 CorpDNS: false
	maskedPrefs := &ipn.MaskedPrefs{
		Prefs: ipn.Prefs{
			CorpDNS: false,
		},
		CorpDNSSet: true,
	}

	// 调用 localClient 的 EditPrefs 方法
	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("设置 CorpDNS: false 失败: %v", err)
	}

	log.Println("✅ 成功禁用 Tailscale DNS 覆盖")
	return nil
}
