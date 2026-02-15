package sandbox

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"opencode-chat/internal/models"
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
func (ld *LocalDockerSandbox) Start(apiKeys map[string]models.AuthConfig) error {
	ld.ctx, ld.cancel = context.WithCancel(context.Background())

	if err := ld.cleanupOrphanedContainers(); err != nil {
		log.Printf("LocalDocker: Warning - failed to cleanup orphaned containers: %v", err)
	}

	var err error
	ld.port, err = FindFreePort()
	if err != nil {
		return fmt.Errorf("failed to find free port for OpenCode: %w", err)
	}
	log.Printf("LocalDocker: Allocated port %d for OpenCode", ld.port)

	ld.gottyPort, err = FindFreePort()
	if err != nil {
		return fmt.Errorf("failed to find free port for Gotty: %w", err)
	}
	log.Printf("LocalDocker: Allocated port %d for Gotty", ld.gottyPort)

	ld.authFile, err = CreateAuthFile(apiKeys)
	if err != nil {
		return fmt.Errorf("failed to create auth file: %w", err)
	}
	log.Printf("LocalDocker: Created auth file at %s", ld.authFile)

	if err := ld.ensureImage(); err != nil {
		return fmt.Errorf("failed to ensure Docker image: %w", err)
	}

	if err := ld.createContainer(); err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	if err := ld.waitForReady(); err != nil {
		return fmt.Errorf("OpenCode not ready: %w", err)
	}

	log.Printf("LocalDocker: OpenCode sandbox ready on port %d", ld.port)
	return nil
}

func (ld *LocalDockerSandbox) OpencodeURL() string {
	return fmt.Sprintf("http://localhost:%d", ld.port)
}

func (ld *LocalDockerSandbox) GottyURL() string {
	log.Printf("LocalDocker.GottyURL: gottyPort=%d", ld.gottyPort)
	if ld.gottyPort == 0 {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", ld.gottyPort)
}

func (ld *LocalDockerSandbox) DownloadZip() (io.ReadCloser, error) {
	if !ld.IsRunning() {
		return nil, fmt.Errorf("sandbox is not running")
	}

	cmd := exec.CommandContext(ld.ctx, "docker", "exec", ld.containerName,
		"sh", "-c", "cd /app && ([ -z \"$(ls -A .)\" ] && echo 'Empty workspace' > .placeholder || true) && zip -r - . 2>/dev/null")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start zip command: %w", err)
	}

	return &processReader{stdout: stdout, cmd: cmd}, nil
}

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

func (ld *LocalDockerSandbox) Stop() error {
	if ld.cancel != nil {
		ld.cancel()
		ld.cancel = nil
	}

	var errors []string

	if ld.containerName != "" {
		log.Printf("LocalDocker: Stopping container %s", ld.containerName)

		if ld.containerExists() {
			stopCmd := exec.Command("docker", "stop", "--time=5", ld.containerName)
			if err := stopCmd.Run(); err != nil {
				log.Printf("LocalDocker: Graceful stop failed for %s: %v", ld.containerName, err)
			}

			rmCmd := exec.Command("docker", "rm", "-f", ld.containerName)
			if err := rmCmd.Run(); err != nil {
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

	if ld.authFile != "" {
		if err := os.Remove(ld.authFile); err != nil && !os.IsNotExist(err) {
			errors = append(errors, fmt.Sprintf("failed to remove auth file: %v", err))
		} else if err == nil {
			log.Printf("LocalDocker: Removed auth file %s", ld.authFile)
		}
		ld.authFile = ""
	}

	ld.port = 0
	ld.gottyPort = 0

	if len(errors) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errors, "; "))
	}

	return nil
}

func (ld *LocalDockerSandbox) containerExists() bool {
	if ld.containerName == "" {
		return false
	}
	cmd := exec.Command("docker", "inspect", ld.containerName)
	return cmd.Run() == nil
}

func (ld *LocalDockerSandbox) IsRunning() bool {
	if ld.containerName == "" {
		return false
	}
	cmd := exec.Command("docker", "ps", "-q", "-f", fmt.Sprintf("name=%s", ld.containerName))
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) != ""
}

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

func (ld *LocalDockerSandbox) ensureImage() error {
	cmd := exec.Command("docker", "images", "-q", dockerImageName+":latest")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check image: %w", err)
	}
	if strings.TrimSpace(string(output)) != "" {
		log.Printf("LocalDocker: Image %s already exists", dockerImageName)
		return nil
	}
	log.Printf("LocalDocker: Building image %s", dockerImageName)
	return ld.buildImage()
}

func (ld *LocalDockerSandbox) buildImage() error {
	cmd := exec.CommandContext(ld.ctx, "docker", "build",
		"-t", dockerImageName+":latest",
		"-f", "sandbox/Dockerfile",
		".")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Docker build output: %s", string(output))
		return fmt.Errorf("failed to build Docker image: %w", err)
	}
	log.Printf("LocalDocker: Build completed successfully")
	return nil
}

func (ld *LocalDockerSandbox) createContainer() error {
	ld.containerName = fmt.Sprintf("%s%d", containerPrefix, time.Now().Unix())

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

func (ld *LocalDockerSandbox) waitForReady() error {
	return WaitForOpencodeReady(ld.port, 30*time.Second)
}

func (ld *LocalDockerSandbox) cleanupOrphanedContainers() error {
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

	for _, containerID := range containerIDs {
		nameCmd := exec.Command("docker", "inspect", "--format", "{{.Name}}", containerID)
		nameOutput, _ := nameCmd.Output()
		containerName := strings.TrimSpace(strings.TrimPrefix(string(nameOutput), "/"))

		stopCmd := exec.Command("docker", "rm", "-f", containerID)
		if err := stopCmd.Run(); err != nil {
			log.Printf("LocalDocker: Warning - failed to remove orphaned container %s (%s): %v", containerName, containerID[:12], err)
		} else {
			log.Printf("LocalDocker: Removed orphaned container %s (%s)", containerName, containerID[:12])
		}
	}

	return nil
}
