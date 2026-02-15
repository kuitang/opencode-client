package e2e

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

func terminalBeforeEach(t *testing.T, page playwright.Page, appURL string) {
	t.Helper()
	target := appURL
	if target == "" {
		target = baseURL
	}
	_, err := page.Goto(target, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	})
	require.NoError(t, err)

	terminalBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{
		Name: "Terminal",
	})
	err = terminalBtn.Click()
	require.NoError(t, err)
	time.Sleep(1000 * time.Millisecond)
}

func TestTerminal(t *testing.T) {
	appURL := os.Getenv("TERMINAL_APP_URL")
	gottyURL := os.Getenv("TERMINAL_GOTTY_URL")

	t.Run("terminal iframe is rendered and interactive", func(t *testing.T) {
		page := newPage(t)
		terminalBeforeEach(t, page, appURL)

		iframe := page.Locator("#terminal-iframe")
		visible, err := iframe.IsVisible()
		require.NoError(t, err)
		require.True(t, visible)

		runCommandInTerminal(t, page, `echo "hello from playwright"`)
		time.Sleep(500 * time.Millisecond)
	})

	t.Run("direct gotty access is available", func(t *testing.T) {
		if gottyURL == "" {
			t.Skip("Set TERMINAL_GOTTY_URL to run direct gotty checks")
		}
		page := newPage(t)
		_, err := page.Goto(gottyURL, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		})
		require.NoError(t, err)

		title, err := page.Title()
		require.NoError(t, err)
		require.Regexp(t, `(?i)gotty`, title)
	})

	t.Run("files created through terminal appear in Code tab", func(t *testing.T) {
		if gottyURL == "" {
			t.Skip("Set TERMINAL_GOTTY_URL to verify file sync through terminal")
		}
		page := newPage(t)

		filename := fmt.Sprintf("playwright-terminal-%d.txt", time.Now().UnixMilli())

		_, err := page.Goto(gottyURL, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		})
		require.NoError(t, err)

		// Focus the gotty terminal (x-screen element).
		terminal := page.Locator("x-screen").First()
		err = terminal.WaitFor(playwright.LocatorWaitForOptions{
			State:   playwright.WaitForSelectorStateVisible,
			Timeout: playwright.Float(15000),
		})
		require.NoError(t, err)
		err = terminal.Click()
		require.NoError(t, err)

		err = page.Keyboard().Type(fmt.Sprintf(`echo "terminal content" > %s`, filename))
		require.NoError(t, err)
		err = page.Keyboard().Press("Enter")
		require.NoError(t, err)

		err = page.Keyboard().Type(fmt.Sprintf("cat %s", filename))
		require.NoError(t, err)
		err = page.Keyboard().Press("Enter")
		require.NoError(t, err)
		time.Sleep(1000 * time.Millisecond)

		target := appURL
		if target == "" {
			target = baseURL
		}
		_, err = page.Goto(target, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		})
		require.NoError(t, err)

		codeBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name: "Code",
		})
		err = codeBtn.Click()
		require.NoError(t, err)
		time.Sleep(1000 * time.Millisecond)

		text, err := page.Locator("#main-content").TextContent()
		require.NoError(t, err)
		require.Contains(t, text, filename)
	})
}
