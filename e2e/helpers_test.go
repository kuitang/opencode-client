package e2e

import (
	"testing"

	"github.com/playwright-community/playwright-go"
)

// injectSessionHelper adds an init script that patches window.fetch to track
// active sessions, mirroring the JS helpers/session-helper.js behaviour.
func injectSessionHelper(t *testing.T, page playwright.Page) {
	t.Helper()
	err := page.AddInitScript(playwright.Script{
		Content: playwright.String(`(function sessionHelper() {
  const originalFetch = window.fetch;
  window.fetch = function patchedFetch(...args) {
    return originalFetch.apply(this, args).then((response) => {
      const url = args[0];
      if ((url === '/send' || url === '/') && response && response.ok) {
        try { window.localStorage.setItem('hasActiveSession', 'true'); }
        catch (error) { console.warn('[Session Helper] Failed to persist session marker', error); }
      }
      return response;
    });
  };
  if (window.localStorage.getItem('hasActiveSession') === 'true') {
    document.cookie = 'session=playwright_restored; path=/';
  }
})();`),
	})
	if err != nil {
		t.Fatalf("injectSessionHelper: %v", err)
	}
}

// focusTerminal locates the terminal iframe and focuses the terminal element
// inside it. It returns the FrameLocator for further interaction.
func focusTerminal(t *testing.T, page playwright.Page) playwright.FrameLocator {
	t.Helper()
	frame := page.FrameLocator("#terminal-iframe")

	body := frame.Locator("body")
	err := body.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateAttached,
		Timeout: playwright.Float(15000),
	})
	if err != nil {
		t.Fatalf("focusTerminal: waiting for body: %v", err)
	}

	xtermScreen := frame.Locator("x-screen")
	count, err := xtermScreen.Count()
	if err != nil {
		t.Fatalf("focusTerminal: counting x-screen: %v", err)
	}
	if count > 0 {
		err = xtermScreen.First().WaitFor(playwright.LocatorWaitForOptions{
			State:   playwright.WaitForSelectorStateVisible,
			Timeout: playwright.Float(15000),
		})
		if err != nil {
			t.Fatalf("focusTerminal: waiting for x-screen visible: %v", err)
		}
		if err := xtermScreen.First().Click(); err != nil {
			t.Fatalf("focusTerminal: clicking x-screen: %v", err)
		}
		return frame
	}

	terminalInput := frame.GetByRole("textbox", playwright.FrameLocatorGetByRoleOptions{
		Name: "terminal input",
	})
	err = terminalInput.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(15000),
	})
	if err != nil {
		t.Fatalf("focusTerminal: waiting for terminal input: %v", err)
	}
	if err := terminalInput.Click(); err != nil {
		t.Fatalf("focusTerminal: clicking terminal input: %v", err)
	}
	return frame
}

// runCommandInTerminal focuses the terminal and types a command followed by Enter.
func runCommandInTerminal(t *testing.T, page playwright.Page, command string) {
	t.Helper()
	focusTerminal(t, page)
	if err := page.Keyboard().Type(command); err != nil {
		t.Fatalf("runCommandInTerminal: typing command: %v", err)
	}
	if err := page.Keyboard().Press("Enter"); err != nil {
		t.Fatalf("runCommandInTerminal: pressing Enter: %v", err)
	}
}
