package e2e

import (
	"testing"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

func TestSSEScroll(t *testing.T) {
	t.Run("auto-scroll logic respects user position", func(t *testing.T) {
		page := newPage(t)

		_, err := page.Goto(baseURL, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		})
		require.NoError(t, err)

		_, err = page.WaitForSelector("#messages")
		require.NoError(t, err)

		result, err := page.Evaluate(`async () => {
			const results = [];
			const messagesDiv = document.getElementById('messages');
			if (!messagesDiv) return { success: false, error: 'Messages container not found' };

			const isAtBottom = () =>
				Math.abs(messagesDiv.scrollHeight - messagesDiv.scrollTop - messagesDiv.clientHeight) < 10;

			// Test 1: Initial load scrolls to bottom
			results.push({ test: 'Initial load scrolls to bottom', passed: isAtBottom() });

			// Test 2: Auto-scrolls when at bottom
			messagesDiv.scrollTop = messagesDiv.scrollHeight;
			messagesDiv.insertAdjacentHTML('beforeend',
				'<div id="assistant-test1" class="my-2">Test message 1</div>');
			document.body.dispatchEvent(new CustomEvent('htmx:sseMessage'));
			await new Promise(r => setTimeout(r, 100));
			results.push({ test: 'Auto-scrolls when at bottom', passed: isAtBottom() });

			// Test 3: Preserves position when scrolled up
			messagesDiv.scrollTop = messagesDiv.scrollHeight / 2;
			const midPosition = messagesDiv.scrollTop;
			messagesDiv.insertAdjacentHTML('beforeend',
				'<div id="assistant-test2" class="my-2">Test message 2</div>');
			document.body.dispatchEvent(new CustomEvent('htmx:sseMessage'));
			await new Promise(r => setTimeout(r, 100));
			results.push({
				test: 'Preserves position when scrolled up',
				passed: Math.abs(messagesDiv.scrollTop - midPosition) < 50,
			});

			// Test 4: Auto-scrolls within threshold
			messagesDiv.scrollTop = messagesDiv.scrollHeight - messagesDiv.clientHeight - 50;
			messagesDiv.insertAdjacentHTML('beforeend',
				'<div id="assistant-test3" class="my-2">Test message 3</div>');
			document.body.dispatchEvent(new CustomEvent('htmx:sseMessage'));
			await new Promise(r => setTimeout(r, 100));
			results.push({ test: 'Auto-scrolls within threshold', passed: isAtBottom() });

			// Test 5: Handles rapid message updates
			messagesDiv.scrollTop = messagesDiv.scrollHeight;
			for (let i = 0; i < 5; i++) {
				messagesDiv.insertAdjacentHTML('beforeend',
					'<div id="assistant-rapid' + i + '" class="my-2">Rapid ' + i + '</div>');
				document.body.dispatchEvent(new CustomEvent('htmx:sseMessage'));
				await new Promise(r => setTimeout(r, 20));
			}
			results.push({ test: 'Handles rapid message updates', passed: isAtBottom() });

			return { success: results.every(e => e.passed !== false), results: results };
		}`)
		require.NoError(t, err)

		m, ok := result.(map[string]interface{})
		require.True(t, ok, "expected result to be a map")
		require.True(t, m["success"].(bool), "all scroll checks should pass: %v", m["results"])
	})
}
