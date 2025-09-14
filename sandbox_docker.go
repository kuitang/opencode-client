package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	dockerImageName = "opencode-sandbox"
	containerPrefix = "opencode-sandbox-"
)

// LocalDockerSandbox implements the Sandbox interface using local Docker containers
type LocalDockerSandbox struct {
	containerID   string
	containerName string
	port          int
	gottyPort     int
	authFile      string
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewLocalDockerSandbox creates a new LocalDocker sandbox instance
func NewLocalDockerSandbox() *LocalDockerSandbox {
	return &LocalDockerSandbox{}
}

// Start initializes the Docker sandbox with the provided API keys
func (ld *LocalDockerSandbox) Start(apiKeys map[string]AuthConfig) error {
	// Create context for this sandbox instance
	ld.ctx, ld.cancel = context.WithCancel(context.Background())

	// Clean up any orphaned containers from previous runs
	if err := ld.cleanupOrphanedContainers(); err != nil {
		log.Printf("LocalDocker: Warning - failed to cleanup orphaned containers: %v", err)
		// Continue anyway - this is not a fatal error
	}

	// Find free ports for OpenCode and Gotty
	var err error
	ld.port, err = findFreePort()
	if err != nil {
		return fmt.Errorf("failed to find free port for OpenCode: %w", err)
	}
	log.Printf("LocalDocker: Allocated port %d for OpenCode", ld.port)

	ld.gottyPort, err = findFreePort()
	if err != nil {
		return fmt.Errorf("failed to find free port for Gotty: %w", err)
	}
	log.Printf("LocalDocker: Allocated port %d for Gotty", ld.gottyPort)

	// Create auth file for the container
	ld.authFile, err = createAuthFile(apiKeys)
	if err != nil {
		return fmt.Errorf("failed to create auth file: %w", err)
	}
	log.Printf("LocalDocker: Created auth file at %s", ld.authFile)

	// Build Docker image if it doesn't exist
	if err := ld.ensureImage(); err != nil {
		return fmt.Errorf("failed to ensure Docker image: %w", err)
	}

	// Create and start container
	if err := ld.createContainer(); err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	// Wait for OpenCode to be ready
	if err := ld.waitForReady(); err != nil {
		return fmt.Errorf("OpenCode not ready: %w", err)
	}

	log.Printf("LocalDocker: OpenCode sandbox ready on port %d", ld.port)
	return nil
}

// OpencodeURL returns the HTTP URL for accessing the OpenCode REST API
func (ld *LocalDockerSandbox) OpencodeURL() string {
	return fmt.Sprintf("http://localhost:%d", ld.port)
}

// GottyURL returns the HTTP URL for accessing the Gotty terminal interface
func (ld *LocalDockerSandbox) GottyURL() string {
	log.Printf("LocalDocker.GottyURL: gottyPort=%d", ld.gottyPort)
	if ld.gottyPort == 0 {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", ld.gottyPort)
}

// DownloadZip creates a zip archive of the sandbox working directory
func (ld *LocalDockerSandbox) DownloadZip() (io.ReadCloser, error) {
	if !ld.IsRunning() {
		return nil, fmt.Errorf("sandbox is not running")
	}

	// Use docker exec to create zip archive and stream it
	// Handle empty directories by creating a placeholder if needed
	cmd := exec.CommandContext(ld.ctx, "docker", "exec", ld.containerName,
		"sh", "-c", "cd /app && ([ -z \"$(ls -A .)\" ] && echo 'Empty workspace' > .placeholder || true) && zip -r - . 2>/dev/null")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start zip command: %w", err)
	}

	// Return a reader that closes the process when done
	return &processReader{stdout: stdout, cmd: cmd}, nil
}

// processReader wraps a process stdout with cleanup
type processReader struct {
	stdout io.ReadCloser
	cmd    *exec.Cmd
}

func (pr *processReader) Read(p []byte) (n int, err error) {
	return pr.stdout.Read(p)
}

func (pr *processReader) Close() error {
	if err := pr.stdout.Close(); err != nil {
		log.Printf("Error closing stdout: %v", err)
	}
	if err := pr.cmd.Wait(); err != nil {
		log.Printf("Process exit error: %v", err)
	}
	return nil
}

// Stop gracefully shuts down the sandbox and cleans up resources
// This method is idempotent and can be called multiple times safely
func (ld *LocalDockerSandbox) Stop() error {
	if ld.cancel != nil {
		ld.cancel()
		ld.cancel = nil // Prevent multiple cancellations
	}

	var errors []string

	// Stop and remove container
	if ld.containerName != "" {
		log.Printf("LocalDocker: Stopping container %s", ld.containerName)

		// Check if container still exists before attempting to stop it
		if ld.containerExists() {
			// Try graceful stop first
			stopCmd := exec.Command("docker", "stop", "--time=5", ld.containerName)
			if err := stopCmd.Run(); err != nil {
				log.Printf("LocalDocker: Graceful stop failed for %s: %v", ld.containerName, err)
				// Continue to force removal
			}

			// Force remove container (handles both stopped and running containers)
			rmCmd := exec.Command("docker", "rm", "-f", ld.containerName)
			if err := rmCmd.Run(); err != nil {
				// Retry once more with different approach
				rmRetryCmd := exec.Command("docker", "container", "rm", "-f", ld.containerName)
				if retryErr := rmRetryCmd.Run(); retryErr != nil {
					errors = append(errors, fmt.Sprintf("failed to remove container %s: %v (retry: %v)", ld.containerName, err, retryErr))
				} else {
					log.Printf("LocalDocker: Force removed container %s (retry succeeded)", ld.containerName)
				}
			} else {
				log.Printf("LocalDocker: Force removed container %s", ld.containerName)
			}
		} else {
			log.Printf("LocalDocker: Container %s already removed", ld.containerName)
		}

		ld.containerName = ""
		ld.containerID = ""
	}

	// Clean up auth file
	if ld.authFile != "" {
		if err := os.Remove(ld.authFile); err != nil && !os.IsNotExist(err) {
			errors = append(errors, fmt.Sprintf("failed to remove auth file: %v", err))
		} else if err == nil {
			log.Printf("LocalDocker: Removed auth file %s", ld.authFile)
		}
		ld.authFile = ""
	}

	// Reset ports
	ld.port = 0
	ld.gottyPort = 0

	if len(errors) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errors, "; "))
	}

	return nil
}

// containerExists checks if the container still exists in Docker
func (ld *LocalDockerSandbox) containerExists() bool {
	if ld.containerName == "" {
		return false
	}

	cmd := exec.Command("docker", "inspect", ld.containerName)
	err := cmd.Run()
	return err == nil
}

// IsRunning returns true if the sandbox container is currently running
func (ld *LocalDockerSandbox) IsRunning() bool {
	if ld.containerName == "" {
		return false
	}

	// Check if container is running using docker ps
	cmd := exec.Command("docker", "ps", "-q", "-f", fmt.Sprintf("name=%s", ld.containerName))
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	// If output is not empty, container is running
	return strings.TrimSpace(string(output)) != ""
}

// ContainerIP returns the IP address of the running container
func (ld *LocalDockerSandbox) ContainerIP() string {
	if ld.containerName == "" || !ld.IsRunning() {
		return ""
	}

	cmd := exec.Command("docker", "inspect", "--format", "{{.NetworkSettings.IPAddress}}", ld.containerName)
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Failed to get container IP: %v", err)
		return ""
	}

	return strings.TrimSpace(string(output))
}

// ensureImage builds the Docker image if it doesn't exist
func (ld *LocalDockerSandbox) ensureImage() error {
	// Check if image already exists
	cmd := exec.Command("docker", "images", "-q", dockerImageName+":latest")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check image: %w", err)
	}

	if strings.TrimSpace(string(output)) != "" {
		log.Printf("LocalDocker: Image %s already exists", dockerImageName)
		return nil
	}

	// Build the image
	log.Printf("LocalDocker: Building image %s", dockerImageName)
	return ld.buildImage()
}

// buildImage builds the Docker image from the Dockerfile
func (ld *LocalDockerSandbox) buildImage() error {
	// Build the image using docker build command
	cmd := exec.CommandContext(ld.ctx, "docker", "build",
		"-t", dockerImageName+":latest",
		"-f", "sandbox/Dockerfile",
		".")

	// Capture output for debugging
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Docker build output: %s", string(output))
		return fmt.Errorf("failed to build Docker image: %w", err)
	}

	log.Printf("LocalDocker: Build completed successfully")
	return nil
}

// createContainer creates and starts the Docker container
func (ld *LocalDockerSandbox) createContainer() error {
	// Generate unique container name
	ld.containerName = fmt.Sprintf("%s%d", containerPrefix, time.Now().Unix())

	// Run container with port mapping for both OpenCode and Gotty, and auth file mount
	// Using --init flag for proper signal handling
	cmd := exec.CommandContext(ld.ctx, "docker", "run", "-d",
		"--init",
		"--name", ld.containerName,
		"-p", fmt.Sprintf("127.0.0.1:%d:8080", ld.port),
		"-p", fmt.Sprintf("127.0.0.1:%d:8081", ld.gottyPort),
		"-v", fmt.Sprintf("%s:/home/opencode/.local/share/opencode/auth.json:ro", ld.authFile),
		dockerImageName+":latest")

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	ld.containerID = strings.TrimSpace(string(output))
	log.Printf("LocalDocker: Started container %s (%s) on port %d", ld.containerName, ld.containerID[:12], ld.port)
	return nil
}

// waitForReady waits for OpenCode to be ready to accept requests
func (ld *LocalDockerSandbox) waitForReady() error {
	return waitForOpencodeReady(ld.port, 30*time.Second)
}

// cleanupOrphanedContainers removes any existing opencode-sandbox containers
func (ld *LocalDockerSandbox) cleanupOrphanedContainers() error {
	// Find all opencode-sandbox containers
	cmd := exec.Command("docker", "ps", "-a", "-q", "--filter", "name="+containerPrefix)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list orphaned containers: %w", err)
	}

	containerIDs := strings.Fields(strings.TrimSpace(string(output)))
	if len(containerIDs) == 0 {
		log.Printf("LocalDocker: No orphaned containers to clean up")
		return nil
	}

	log.Printf("LocalDocker: Found %d orphaned containers, cleaning up...", len(containerIDs))

	// Stop and remove each container
	for _, containerID := range containerIDs {
		// Get container name for logging
		nameCmd := exec.Command("docker", "inspect", "--format", "{{.Name}}", containerID)
		nameOutput, _ := nameCmd.Output()
		containerName := strings.TrimSpace(strings.TrimPrefix(string(nameOutput), "/"))

		// Force stop and remove container
		stopCmd := exec.Command("docker", "rm", "-f", containerID)
		if err := stopCmd.Run(); err != nil {
			log.Printf("LocalDocker: Warning - failed to remove orphaned container %s (%s): %v", containerName, containerID[:12], err)
			// Continue with other containers
		} else {
			log.Printf("LocalDocker: Removed orphaned container %s (%s)", containerName, containerID[:12])
		}
	}

	return nil
}
