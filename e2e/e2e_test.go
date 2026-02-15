package e2e

import (
	"os"
	"testing"

	"github.com/playwright-community/playwright-go"
)

var (
	pw      *playwright.Playwright
	browser playwright.Browser
	baseURL string
)

func TestMain(m *testing.M) {
	baseURL = os.Getenv("E2E_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	if err := playwright.Install(); err != nil {
		panic(err)
	}

	var err error
	pw, err = playwright.Run()
	if err != nil {
		panic(err)
	}

	launchOpts := playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	}
	// Allow using system Chrome via env var (avoids needing Playwright's bundled Chromium deps).
	if execPath := os.Getenv("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH"); execPath != "" {
		launchOpts.ExecutablePath = playwright.String(execPath)
	}
	browser, err = pw.Chromium.Launch(launchOpts)
	if err != nil {
		panic(err)
	}

	code := m.Run()

	browser.Close()
	pw.Stop()
	os.Exit(code)
}

func newPage(t *testing.T) playwright.Page {
	t.Helper()
	page, err := browser.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { page.Close() })
	return page
}
