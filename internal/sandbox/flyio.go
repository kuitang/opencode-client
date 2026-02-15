package sandbox

import (
	"fmt"
	"io"

	"opencode-chat/internal/models"
)

// FlyIOSandbox implements the Sandbox interface using Fly.io Machines
// This is currently a stub implementation
type FlyIOSandbox struct {
	machineID string
	appName   string
	url       string
	authToken string
	region    string
}

func NewFlyIOSandbox(authToken, region string) *FlyIOSandbox {
	return &FlyIOSandbox{
		authToken: authToken,
		region:    region,
	}
}

func (fs *FlyIOSandbox) Start(apiKeys map[string]models.AuthConfig) error {
	return fmt.Errorf("FlyIO sandbox not implemented yet - this is a stub")
}

func (fs *FlyIOSandbox) OpencodeURL() string {
	if fs.url == "" {
		return ""
	}
	return fs.url
}

func (fs *FlyIOSandbox) GottyURL() string    { return "" }
func (fs *FlyIOSandbox) IsRunning() bool      { return false }
func (fs *FlyIOSandbox) ContainerIP() string  { return "" }
func (fs *FlyIOSandbox) Stop() error          { return fmt.Errorf("FlyIO sandbox Stop not implemented yet") }
func (fs *FlyIOSandbox) DownloadZip() (io.ReadCloser, error) {
	return nil, fmt.Errorf("FlyIO sandbox DownloadZip not implemented yet")
}
