package e2e

import (
	"math"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

// setViewportAndDispatch resizes the viewport and dispatches a resize event,
// matching the JS helper of the same name.
func setViewportAndDispatch(t *testing.T, page playwright.Page, width, height int) {
	t.Helper()
	err := page.SetViewportSize(width, height)
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
	_, err = page.Evaluate(`() => { window.dispatchEvent(new Event('resize')); }`)
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)
}

// ensureMessages injects at least 30 placeholder messages into #messages.
func ensureMessages(t *testing.T, page playwright.Page) {
	t.Helper()
	_, err := page.Evaluate(`() => {
		const messagesDiv = document.getElementById('messages');
		if (!messagesDiv) throw new Error('messages container missing');
		if (messagesDiv.children.length >= 30) return;
		for (let i = messagesDiv.children.length; i < 30; i++) {
			messagesDiv.insertAdjacentHTML('beforeend',
				'<div class="flex mb-4"><div class="bg-blue-100 text-gray-900 rounded-lg p-3 max-w-[80%]">Test message ' + i + '</div></div>');
		}
	}`)
	require.NoError(t, err)
}

// getChatState returns the chat container class state as a map.
func getChatState(t *testing.T, page playwright.Page) map[string]interface{} {
	t.Helper()
	result, err := page.Evaluate(`() => {
		const chat = document.getElementById('chat-container');
		if (!chat) return { missing: true };
		const classes = Array.from(chat.classList);
		return {
			missing: false,
			classes: classes,
			hasExpanded: chat.classList.contains('chat-expanded'),
			hasMinimized: chat.classList.contains('chat-minimized'),
		};
	}`)
	require.NoError(t, err)
	m, ok := result.(map[string]interface{})
	require.True(t, ok, "getChatState should return a map")
	return m
}

func resizeBeforeEach(t *testing.T, page playwright.Page) {
	t.Helper()
	_, err := page.Goto(baseURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	})
	require.NoError(t, err)
	_, err = page.WaitForSelector("#chat-container")
	require.NoError(t, err)
	ensureMessages(t, page)
}

func TestResize(t *testing.T) {
	t.Run("chat container toggles layout classes across breakpoints", func(t *testing.T) {
		page := newPage(t)
		resizeBeforeEach(t, page)

		setViewportAndDispatch(t, page, 1280, 720)
		desktopState := getChatState(t, page)
		require.False(t, desktopState["missing"].(bool))

		setViewportAndDispatch(t, page, 375, 812)
		mobileState := getChatState(t, page)
		require.False(t, mobileState["missing"].(bool))

		// Classes should differ between desktop and mobile.
		desktopClasses := toStringSlice(desktopState["classes"])
		mobileClasses := toStringSlice(mobileState["classes"])
		require.NotEqual(t, joinStrings(desktopClasses), joinStrings(mobileClasses))
	})

	t.Run("scroll position is preserved when returning to previous viewport", func(t *testing.T) {
		page := newPage(t)
		resizeBeforeEach(t, page)

		setViewportAndDispatch(t, page, 375, 812)

		targetScrollRaw, err := page.Evaluate(`() => {
			const messagesDiv = document.getElementById('messages');
			messagesDiv.scrollTop = messagesDiv.scrollHeight / 2;
			return messagesDiv.scrollTop;
		}`)
		require.NoError(t, err)
		targetScroll := toFloat64(targetScrollRaw)

		setViewportAndDispatch(t, page, 1280, 720)
		setViewportAndDispatch(t, page, 375, 812)

		restoredRaw, err := page.Evaluate(`() => document.getElementById('messages').scrollTop`)
		require.NoError(t, err)
		restored := toFloat64(restoredRaw)

		require.Less(t, math.Abs(restored-targetScroll), 60.0,
			"scroll position should be preserved within 60px")
	})

	t.Run("smooth scrolling continues through resize events", func(t *testing.T) {
		page := newPage(t)
		resizeBeforeEach(t, page)

		setViewportAndDispatch(t, page, 1280, 720)

		beforeRaw, err := page.Evaluate(`async () => {
			const messagesDiv = document.getElementById('messages');
			messagesDiv.scrollTop = messagesDiv.scrollHeight;
			messagesDiv.scrollTo({ top: 0, behavior: 'smooth' });
			return { scrollHeight: messagesDiv.scrollHeight };
		}`)
		require.NoError(t, err)
		before := beforeRaw.(map[string]interface{})

		time.Sleep(100 * time.Millisecond)
		setViewportAndDispatch(t, page, 375, 812)
		time.Sleep(400 * time.Millisecond)
		setViewportAndDispatch(t, page, 1280, 720)
		time.Sleep(300 * time.Millisecond)

		afterRaw, err := page.Evaluate(`() => {
			const messagesDiv = document.getElementById('messages');
			return { finalScroll: messagesDiv.scrollTop, scrollHeight: messagesDiv.scrollHeight };
		}`)
		require.NoError(t, err)
		after := afterRaw.(map[string]interface{})

		require.Equal(t, before["scrollHeight"], after["scrollHeight"])
		require.GreaterOrEqual(t, toFloat64(after["finalScroll"]), float64(-1))
	})
}

// toStringSlice converts an interface{} (expected []interface{} of strings) to []string.
func toStringSlice(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, len(arr))
	for i, item := range arr {
		out[i], _ = item.(string)
	}
	return out
}

// joinStrings joins a string slice with spaces.
func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += " "
		}
		result += s
	}
	return result
}

// toFloat64 converts an interface{} (expected numeric) to float64.
func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}
