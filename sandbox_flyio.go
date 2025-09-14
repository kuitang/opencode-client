package main

import (
	"fmt"
	"io"
)

// FlyIOSandbox implements the Sandbox interface using Fly.io Machines
// This is currently a stub implementation with detailed comments on the planned architecture
type FlyIOSandbox struct {
	machineID string
	appName   string
	url       string
	authToken string
	region    string
}

// NewFlyIOSandbox creates a new FlyIO sandbox instance
func NewFlyIOSandbox(authToken, region string) *FlyIOSandbox {
	return &FlyIOSandbox{
		authToken: authToken,
		region:    region,
	}
}

// Start initializes the FlyIO sandbox with the provided API keys
func (fs *FlyIOSandbox) Start(apiKeys map[string]AuthConfig) error {
	// FUTURE IMPLEMENTATION PLAN:
	//
	// 1. AUTHENTICATION:
	//    - Use the provided authToken for Fly.io API authentication
	//    - Set Authorization header: "Bearer {authToken}"
	//    - All API requests go to https://api.machines.dev
	//
	// 2. APP CREATION (if needed):
	//    - Check if app exists: GET /v1/apps/{app-name}
	//    - If not exists, create: POST /v1/apps
	//    - App name format: "opencode-sandbox-{uuid}"
	//    - Store app name in fs.appName
	//
	// 3. MACHINE CONFIGURATION:
	//    - Use the same Docker image as LocalDocker: "opencode-sandbox:latest"
	//    - Push image to Fly.io registry or use public registry
	//    - Configure machine spec:
	//      {
	//        "name": "opencode-{timestamp}",
	//        "config": {
	//          "image": "opencode-sandbox:latest",
	//          "services": [{
	//            "ports": [{
	//              "port": 8080,
	//              "handlers": ["http"]
	//            }],
	//            "protocol": "tcp",
	//            "internal_port": 8080
	//          }],
	//          "env": {
	//            // Environment variables for OpenCode configuration
	//          },
	//          "files": [{
	//            // Mount auth.json as file
	//            "guest_path": "/home/opencode/.local/share/opencode/auth.json",
	//            "raw_value": base64_encode(auth_json_content)
	//          }]
	//        }
	//      }
	//
	// 4. MACHINE LIFECYCLE:
	//    - Create machine: POST /v1/apps/{app-name}/machines
	//    - Start machine: POST /v1/apps/{app-name}/machines/{machine-id}/start
	//    - Machine boots in ~300ms thanks to Firecracker VMs
	//    - Store machine ID in fs.machineID
	//    - Get machine info to extract public URL: GET /v1/apps/{app-name}/machines/{machine-id}
	//
	// 5. NETWORKING:
	//    - Fly.io automatically provides external connectivity
	//    - Public URL format: https://{app-name}.fly.dev
	//    - Internal networking for app-to-app communication available
	//    - IPv6 support for SSH access if needed
	//
	// 6. HEALTH MONITORING:
	//    - Use health checks defined in Dockerfile
	//    - Monitor machine status via: GET /v1/apps/{app-name}/machines/{machine-id}
	//    - Wait for machine state to be "started" and health check to pass
	//
	// 7. ERROR HANDLING:
	//    - Handle regional deployment failures (try different regions)
	//    - Implement exponential backoff for API rate limits
	//    - Monitor machine crashes and implement auto-restart logic
	//
	// IMPLEMENTATION PSEUDOCODE:
	//
	// func (fs *FlyIOSandbox) Start(apiKeys map[string]AuthConfig) error {
	//     // Create auth file content
	//     authJSON, _ := json.Marshal(apiKeys)
	//     authBase64 := base64.StdEncoding.EncodeToString(authJSON)
	//
	//     // Create machine config
	//     machineConfig := FlyMachineConfig{
	//         Name: fmt.Sprintf("opencode-%d", time.Now().Unix()),
	//         Config: FlyMachineSpec{
	//             Image: "opencode-sandbox:latest",
	//             Services: []FlyService{{
	//                 Ports: []FlyPort{{Port: 8080, Handlers: []string{"http"}}},
	//                 Protocol: "tcp",
	//                 InternalPort: 8080,
	//             }},
	//             Files: []FlyFile{{
	//                 GuestPath: "/home/opencode/.local/share/opencode/auth.json",
	//                 RawValue: authBase64,
	//             }},
	//         },
	//     }
	//
	//     // Create machine via Fly.io API
	//     resp, err := fs.createMachine(machineConfig)
	//     if err != nil {
	//         return err
	//     }
	//
	//     fs.machineID = resp.ID
	//     fs.url = fmt.Sprintf("https://%s.fly.dev", fs.appName)
	//
	//     // Wait for machine to be ready
	//     return fs.waitForMachineReady()
	// }

	return fmt.Errorf("FlyIO sandbox not implemented yet - this is a stub")
}

// OpencodeURL returns the HTTP URL for accessing the OpenCode REST API
func (fs *FlyIOSandbox) OpencodeURL() string {
	// FUTURE IMPLEMENTATION:
	// Return the public Fly.io URL for this machine
	// Format: https://{app-name}.fly.dev
	//
	// The URL will be determined during Start() when the machine is created
	// and stored in fs.url

	if fs.url == "" {
		return ""
	}
	return fs.url
}

// GottyURL returns the HTTP URL for accessing the Gotty terminal interface
func (fs *FlyIOSandbox) GottyURL() string {
	// FUTURE IMPLEMENTATION:
	// Gotty terminal would be available at a separate path on the same domain
	// Format: https://{app-name}.fly.dev/gotty
	// This requires configuring routing in the Fly.io machine
	return ""
}

// DownloadZip creates a zip archive of the sandbox working directory
func (fs *FlyIOSandbox) DownloadZip() (io.ReadCloser, error) {
	// FUTURE IMPLEMENTATION PLAN:
	//
	// 1. REMOTE EXECUTION:
	//    - Use Fly.io's machine exec API (if available)
	//    - Alternative: HTTP endpoint in OpenCode container for zip creation
	//    - Command: cd /app && zip -r - .
	//
	// 2. STREAMING APPROACH:
	//    - Create zip endpoint in OpenCode container: GET /download-zip
	//    - Proxy the zip stream through this Go service
	//    - Or: Direct download link from Fly.io machine
	//
	// 3. IMPLEMENTATION OPTIONS:
	//
	//    Option A - HTTP Proxy:
	//    - Make HTTP request to https://{app-name}.fly.dev/download-zip
	//    - Return response body as ReadCloser
	//    - Let OpenCode container handle zip creation
	//
	//    Option B - Machine Exec (if supported):
	//    - POST /v1/apps/{app-name}/machines/{machine-id}/exec
	//    - Execute zip command remotely
	//    - Stream result back to client
	//
	//    Option C - Volume Export:
	//    - Create volume snapshot
	//    - Download volume data as tar/zip
	//    - Requires persistent volumes setup
	//
	// RECOMMENDED: Option A with HTTP proxy
	// - Simpler implementation
	// - Better error handling
	// - Leverages existing OpenCode capabilities
	//
	// PSEUDOCODE:
	// func (fs *FlyIOSandbox) DownloadZip() (io.ReadCloser, error) {
	//     zipURL := fmt.Sprintf("%s/download-zip", fs.url)
	//
	//     resp, err := http.Get(zipURL)
	//     if err != nil {
	//         return nil, fmt.Errorf("failed to download zip: %w", err)
	//     }
	//
	//     if resp.StatusCode != 200 {
	//         resp.Body.Close()
	//         return nil, fmt.Errorf("zip download failed: %d", resp.StatusCode)
	//     }
	//
	//     return resp.Body, nil
	// }

	return nil, fmt.Errorf("FlyIO sandbox DownloadZip not implemented yet")
}

// Stop gracefully shuts down the sandbox and cleans up resources
func (fs *FlyIOSandbox) Stop() error {
	// FUTURE IMPLEMENTATION PLAN:
	//
	// 1. GRACEFUL SHUTDOWN:
	//    - POST /v1/apps/{app-name}/machines/{machine-id}/stop
	//    - Wait for machine to stop gracefully (timeout: 30s)
	//    - OpenCode container should handle SIGTERM properly
	//
	// 2. MACHINE CLEANUP:
	//    - DELETE /v1/apps/{app-name}/machines/{machine-id}
	//    - Removes the machine completely
	//    - Firecracker VM is destroyed
	//
	// 3. APP CLEANUP (optional):
	//    - For ephemeral sandboxes, consider keeping app for reuse
	//    - For complete cleanup: DELETE /v1/apps/{app-name}
	//    - Check if other machines exist in the app first
	//
	// 4. ERROR HANDLING:
	//    - Continue cleanup even if some steps fail
	//    - Log errors but don't fail the entire cleanup
	//    - Implement idempotent cleanup (safe to call multiple times)
	//
	// 5. BILLING CONSIDERATIONS:
	//    - Fly.io charges per machine-second
	//    - Ensure machines are properly stopped to avoid charges
	//    - Consider machine state monitoring
	//
	// IMPLEMENTATION:
	// func (fs *FlyIOSandbox) Stop() error {
	//     var errors []string
	//
	//     // Stop machine gracefully
	//     if err := fs.stopMachine(); err != nil {
	//         errors = append(errors, fmt.Sprintf("stop failed: %v", err))
	//     }
	//
	//     // Wait for shutdown with timeout
	//     if err := fs.waitForMachineStop(30 * time.Second); err != nil {
	//         // Force delete if graceful stop fails
	//         log.Printf("Graceful stop failed, force deleting machine")
	//     }
	//
	//     // Delete machine
	//     if err := fs.deleteMachine(); err != nil {
	//         errors = append(errors, fmt.Sprintf("delete failed: %v", err))
	//     }
	//
	//     if len(errors) > 0 {
	//         return fmt.Errorf("cleanup errors: %s", strings.Join(errors, "; "))
	//     }
	//
	//     return nil
	// }

	return fmt.Errorf("FlyIO sandbox Stop not implemented yet")
}

// IsRunning returns true if the sandbox machine is currently running
func (fs *FlyIOSandbox) IsRunning() bool {
	// FUTURE IMPLEMENTATION:
	//
	// Query machine status via Fly.io API:
	// GET /v1/apps/{app-name}/machines/{machine-id}
	//
	// Response includes:
	// {
	//   "id": "machine-id",
	//   "state": "started|stopped|destroying|...",
	//   "config": {...},
	//   "checks": [
	//     {
	//       "name": "health",
	//       "status": "passing|failing|critical",
	//       ...
	//     }
	//   ]
	// }
	//
	// Return true if:
	// - state == "started"
	// - health checks are passing
	//
	// PSEUDOCODE:
	// func (fs *FlyIOSandbox) IsRunning() bool {
	//     if fs.machineID == "" {
	//         return false
	//     }
	//
	//     machine, err := fs.getMachine()
	//     if err != nil {
	//         return false
	//     }
	//
	//     if machine.State != "started" {
	//         return false
	//     }
	//
	//     // Check health status
	//     for _, check := range machine.Checks {
	//         if check.Name == "health" && check.Status != "passing" {
	//             return false
	//         }
	//     }
	//
	//     return true
	// }

	return false
}

// ContainerIP returns empty string for Fly.io sandbox (not applicable)
func (fs *FlyIOSandbox) ContainerIP() string {
	// Fly.io doesn't use local containers, return empty
	return ""
}

// HELPER TYPES FOR FUTURE IMPLEMENTATION:
//
// type FlyMachineConfig struct {
//     Name   string         `json:"name"`
//     Config FlyMachineSpec `json:"config"`
// }
//
// type FlyMachineSpec struct {
//     Image    string     `json:"image"`
//     Services []FlyService `json:"services"`
//     Files    []FlyFile   `json:"files,omitempty"`
//     Env      map[string]string `json:"env,omitempty"`
// }
//
// type FlyService struct {
//     Ports        []FlyPort `json:"ports"`
//     Protocol     string    `json:"protocol"`
//     InternalPort int       `json:"internal_port"`
// }
//
// type FlyPort struct {
//     Port     int      `json:"port"`
//     Handlers []string `json:"handlers"`
// }
//
// type FlyFile struct {
//     GuestPath string `json:"guest_path"`
//     RawValue  string `json:"raw_value"` // base64 encoded content
// }
//
// type FlyMachine struct {
//     ID     string            `json:"id"`
//     State  string            `json:"state"`
//     Config FlyMachineSpec    `json:"config"`
//     Checks []FlyHealthCheck  `json:"checks"`
// }
//
// type FlyHealthCheck struct {
//     Name   string `json:"name"`
//     Status string `json:"status"`
// }
