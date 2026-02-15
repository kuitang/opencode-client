package e2e

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

func previewBeforeEach(t *testing.T, page playwright.Page, timeout float64) {
	t.Helper()
	_, err := page.Goto(baseURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	})
	require.NoError(t, err)
	_, err = page.WaitForSelector("#message-input", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(timeout),
	})
	require.NoError(t, err)
}

func TestPreview(t *testing.T) {
	baseTimeout := 60000.0
	if v := os.Getenv("PREVIEW_TEST_TIMEOUT"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			baseTimeout = parsed
		}
	}

	t.Run("renders preview content on first load", func(t *testing.T) {
		page := newPage(t)
		previewBeforeEach(t, page, baseTimeout)

		mainContent := page.Locator("#main-content")
		visible, err := mainContent.IsVisible()
		require.NoError(t, err)
		require.True(t, visible)

		text, err := mainContent.TextContent()
		require.NoError(t, err)
		require.NotContains(t, text, "Welcome to VibeCoding")

		iframeCount, err := page.Locator("#preview-iframe").Count()
		require.NoError(t, err)
		if iframeCount == 0 {
			noApp := page.GetByText("No Application Running", playwright.PageGetByTextOptions{
				Exact: playwright.Bool(false),
			})
			visible, err := noApp.IsVisible()
			require.NoError(t, err)
			require.True(t, visible)
		}
	})

	t.Run("switching between Code and Preview preserves preview state", func(t *testing.T) {
		page := newPage(t)
		previewBeforeEach(t, page, baseTimeout)

		codeBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name: "Code",
		})
		err := codeBtn.Click()
		require.NoError(t, err)
		time.Sleep(500 * time.Millisecond)

		previewBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name: "Preview",
		})
		err = previewBtn.Click()
		require.NoError(t, err)
		time.Sleep(500 * time.Millisecond)

		hasIframe, err := page.Locator("#preview-iframe").Count()
		require.NoError(t, err)
		hasFallback, err := page.Locator("#main-content").Locator("text=No Application Running").Count()
		require.NoError(t, err)
		require.True(t, hasIframe > 0 || hasFallback > 0)
	})

	t.Run("shows and uses Kill button when an external server is detected", func(t *testing.T) {
		testPort := 5555
		if v := os.Getenv("PREVIEW_TEST_SERVER_PORT"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil {
				testPort = parsed
			}
		}

		page := newPage(t)
		page.SetDefaultTimeout(baseTimeout + 15000)
		previewBeforeEach(t, page, baseTimeout)

		startCommand := fmt.Sprintf(
			"python3 -m http.server %d --bind 0.0.0.0 >/tmp/playwright-preview-%d.log 2>&1 &",
			testPort, testPort,
		)
		killCommand := fmt.Sprintf(
			"kill $(lsof -t -i:%d) 2>/dev/null || true",
			testPort,
		)

		cleanupNeeded := true
		defer func() {
			if !page.IsClosed() {
				termBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{
					Name: "Terminal",
				})
				_ = termBtn.Click()
				runCommandInTerminal(t, page, killCommand)
				if cleanupNeeded {
					time.Sleep(1000 * time.Millisecond)
				} else {
					time.Sleep(500 * time.Millisecond)
				}
			}
		}()

		terminalBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name: "Terminal",
		})
		err := terminalBtn.Click()
		require.NoError(t, err)

		runCommandInTerminal(t, page, startCommand)
		time.Sleep(2000 * time.Millisecond)

		previewBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name: "Preview",
		})
		err = previewBtn.Click()
		require.NoError(t, err)
		time.Sleep(500 * time.Millisecond)

		refreshButton := page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name: "Refresh",
		}).First()
		err = refreshButton.Click()
		require.NoError(t, err)

		killButton := page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name: "Kill",
		})
		err = killButton.WaitFor(playwright.LocatorWaitForOptions{
			State:   playwright.WaitForSelectorStateVisible,
			Timeout: playwright.Float(baseTimeout),
		})
		require.NoError(t, err)

		err = killButton.Click(playwright.LocatorClickOptions{
			NoWaitAfter: playwright.Bool(true),
		})
		require.NoError(t, err)

		noApp := page.GetByText("No Application Running", playwright.PageGetByTextOptions{
			Exact: playwright.Bool(false),
		})
		err = noApp.WaitFor(playwright.LocatorWaitForOptions{
			State:   playwright.WaitForSelectorStateVisible,
			Timeout: playwright.Float(baseTimeout),
		})
		require.NoError(t, err)

		cleanupNeeded = false
	})
}
