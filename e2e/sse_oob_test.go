package e2e

import (
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

// parseCount extracts the first integer from a string, returning 0 if none found.
func parseCount(text string) int {
	re := regexp.MustCompile(`\d+`)
	match := re.FindString(text)
	if match == "" {
		return 0
	}
	n, _ := strconv.Atoi(match)
	return n
}

func createSandboxFiles(container string) error {
	cmd := exec.Command("docker", "exec", container, "sh", "-c",
		"cd /app && echo 'test content' > sse_test1.txt && echo 'def foo(): pass' > sse_test2.py")
	return cmd.Run()
}

func cleanupSandboxFiles(container string) error {
	cmd := exec.Command("docker", "exec", container, "sh", "-c",
		"cd /app && rm -f sse_test1.txt sse_test2.py")
	return cmd.Run()
}

func TestSSEOOB(t *testing.T) {
	container := os.Getenv("PLAYWRIGHT_SANDBOX_CONTAINER")
	if container == "" {
		t.Skip("Set PLAYWRIGHT_SANDBOX_CONTAINER to run SSE OOB tests")
	}

	t.Run("code tab stats update when sandbox files change", func(t *testing.T) {
		page := newPage(t)

		_, err := page.Goto(baseURL, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		})
		require.NoError(t, err)

		codeBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name: "Code",
		})
		err = codeBtn.Click()
		require.NoError(t, err)

		_, err = page.WaitForSelector("#file-count-container")
		require.NoError(t, err)

		// Capture initial state.
		initialFilesText, err := page.Locator("#file-count-container p:first-child").TextContent()
		require.NoError(t, err)
		initialLinesText, err := page.Locator("#line-count-container p:first-child").TextContent()
		require.NoError(t, err)
		initialOptions, err := page.Locator("#file-selector option").Count()
		require.NoError(t, err)

		// Create sandbox files.
		err = createSandboxFiles(container)
		require.NoError(t, err)
		defer func() {
			_ = cleanupSandboxFiles(container)
		}()

		// Trigger a message to cause refresh.
		err = page.Locator("#message-input").Fill("List the files in the current directory")
		require.NoError(t, err)
		err = page.Locator(`button[type="submit"]`).Click()
		require.NoError(t, err)

		// Wait for the file count to change.
		_, err = page.WaitForFunction(
			`(initial) => {
				const current = document.querySelector('#file-count-container p:first-child');
				return current && current.textContent !== initial;
			}`,
			initialFilesText,
			playwright.PageWaitForFunctionOptions{
				Timeout: playwright.Float(20000),
			},
		)
		require.NoError(t, err)

		// Check updated values.
		newFilesText, err := page.Locator("#file-count-container p:first-child").TextContent()
		require.NoError(t, err)
		newLinesText, err := page.Locator("#line-count-container p:first-child").TextContent()
		require.NoError(t, err)
		newOptions, err := page.Locator("#file-selector option").Count()
		require.NoError(t, err)

		require.Greater(t, parseCount(newFilesText), parseCount(initialFilesText),
			"file count should increase")
		require.GreaterOrEqual(t, parseCount(newLinesText), parseCount(initialLinesText),
			"line count should not decrease")
		require.Greater(t, newOptions, initialOptions,
			"dropdown options should increase")

		// Verify the new files appear in the dropdown options.
		optionTexts, err := page.Locator("#file-selector option").AllTextContents()
		require.NoError(t, err)

		foundTest1 := false
		foundTest2 := false
		for _, text := range optionTexts {
			if strings.Contains(text, "sse_test1.txt") {
				foundTest1 = true
			}
			if strings.Contains(text, "sse_test2.py") {
				foundTest2 = true
			}
		}
		require.True(t, foundTest1, "dropdown should contain sse_test1.txt")
		require.True(t, foundTest2, "dropdown should contain sse_test2.py")
	})
}
