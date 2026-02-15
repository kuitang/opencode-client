package e2e

import (
	"testing"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

func dropdownSSEBeforeEach(t *testing.T, page playwright.Page) {
	t.Helper()
	_, err := page.Goto(baseURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	})
	require.NoError(t, err)

	codeBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{
		Name: "Code",
	})
	err = codeBtn.Click()
	require.NoError(t, err)

	_, err = page.WaitForSelector("#file-selector")
	require.NoError(t, err)
}

func TestDropdownSSE(t *testing.T) {
	t.Run("focus is preserved across synthetic SSE refreshes", func(t *testing.T) {
		page := newPage(t)
		dropdownSSEBeforeEach(t, page)

		result, err := page.Evaluate(`() => {
			const select = document.getElementById('file-selector');
			if (!select) return { ok: false, reason: 'missing dropdown' };
			select.focus();
			const initialFocused = document.activeElement === select;
			document.body.dispatchEvent(new CustomEvent('htmx:sseMessage'));
			return {
				ok: initialFocused && document.activeElement === select,
				initialFocused: initialFocused,
				afterFocused: document.activeElement === select,
			};
		}`)
		require.NoError(t, err)

		m, ok := result.(map[string]interface{})
		require.True(t, ok)
		require.True(t, m["ok"].(bool), "focus should be preserved across SSE refreshes")
	})

	t.Run("selection persists when options change", func(t *testing.T) {
		page := newPage(t)
		dropdownSSEBeforeEach(t, page)

		result, err := page.Evaluate(`() => {
			const select = document.getElementById('file-selector');
			if (!select) return { ok: false, reason: 'missing dropdown' };

			const optionA = document.createElement('option');
			optionA.value = 'playwright-a.txt';
			optionA.textContent = 'playwright-a.txt';
			select.appendChild(optionA);
			select.value = optionA.value;
			select.focus();

			const optionB = document.createElement('option');
			optionB.value = 'playwright-b.txt';
			optionB.textContent = 'playwright-b.txt';
			const previousValue = select.value;
			select.appendChild(optionB);
			document.body.dispatchEvent(new CustomEvent('htmx:sseMessage'));

			return {
				ok: document.activeElement === select && select.value === previousValue,
				selection: select.value,
			};
		}`)
		require.NoError(t, err)

		m, ok := result.(map[string]interface{})
		require.True(t, ok)
		require.True(t, m["ok"].(bool), "selection should persist when options change")
		require.Equal(t, "playwright-a.txt", m["selection"])
	})
}
