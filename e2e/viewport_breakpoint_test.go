package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

// viewportOverflow checks whether any direct children of the flex container
// overflow the viewport width at the current viewport size.
func viewportOverflow(t *testing.T, page playwright.Page) (overflowing bool, details string) {
	t.Helper()
	result, err := page.Evaluate(`() => {
		const vw = window.innerWidth;
		const container = document.querySelector('.flex.h-full');
		if (!container) return { overflowing: false, details: 'no flex container found' };

		const children = container.children;
		let totalChildWidth = 0;
		const rects = [];
		for (let i = 0; i < children.length; i++) {
			const rect = children[i].getBoundingClientRect();
			totalChildWidth += rect.width;
			rects.push({
				tag: children[i].tagName,
				id: children[i].id,
				left: Math.round(rect.left),
				right: Math.round(rect.right),
				width: Math.round(rect.width),
			});
		}

		// Check if any child extends beyond viewport
		let overflowing = false;
		for (let i = 0; i < children.length; i++) {
			const rect = children[i].getBoundingClientRect();
			if (rect.right > vw + 1 || rect.left < -1) {
				overflowing = true;
			}
		}

		return {
			overflowing: overflowing,
			viewportWidth: vw,
			totalChildWidth: Math.round(totalChildWidth),
			children: rects,
		};
	}`)
	require.NoError(t, err)

	m := result.(map[string]interface{})
	of := m["overflowing"].(bool)
	detail := fmt.Sprintf("vw=%v totalChildWidth=%v children=%v",
		m["viewportWidth"], m["totalChildWidth"], m["children"])
	return of, detail
}

func TestViewportBreakpointTransition(t *testing.T) {
	t.Run("no overflow when sweeping through lg breakpoint", func(t *testing.T) {
		page := newPage(t)
		_, err := page.Goto(baseURL, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		})
		require.NoError(t, err)
		_, err = page.WaitForSelector("#chat-container")
		require.NoError(t, err)

		// Sweep from desktop (1200) down through breakpoint (1024) to mobile (800)
		// in 20px steps, checking for overflow at each width.
		for width := 1200; width >= 800; width -= 20 {
			err := page.SetViewportSize(width, 720)
			require.NoError(t, err)
			time.Sleep(50 * time.Millisecond)

			overflowing, details := viewportOverflow(t, page)
			require.False(t, overflowing,
				"overflow at viewport width %d: %s", width, details)
		}

		// Sweep back up to confirm no overflow on the return trip
		for width := 800; width <= 1200; width += 20 {
			err := page.SetViewportSize(width, 720)
			require.NoError(t, err)
			time.Sleep(50 * time.Millisecond)

			overflowing, details := viewportOverflow(t, page)
			require.False(t, overflowing,
				"overflow at viewport width %d (sweeping up): %s", width, details)
		}
	})

	t.Run("layout snaps correctly at breakpoint boundaries", func(t *testing.T) {
		page := newPage(t)
		_, err := page.Goto(baseURL, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		})
		require.NoError(t, err)
		_, err = page.WaitForSelector("#chat-container")
		require.NoError(t, err)

		// Desktop: chat should be a sidebar (less than full width)
		setViewportAndDispatch(t, page, 1280, 720)
		desktopChatWidth := getChatWidth(t, page)
		desktopMainWidth := getMainWidth(t, page)
		require.Less(t, desktopChatWidth, 1280.0,
			"chat should be a sidebar on desktop, not full width")
		require.Greater(t, desktopMainWidth, 0.0,
			"main content should be visible on desktop")

		// Mobile: both should fit within viewport
		setViewportAndDispatch(t, page, 375, 812)
		mobileChatWidth := getChatWidth(t, page)
		require.LessOrEqual(t, mobileChatWidth, 375.0,
			"chat should not exceed viewport width on mobile")

		overflowing, details := viewportOverflow(t, page)
		require.False(t, overflowing,
			"no overflow on mobile: %s", details)
	})
}

func getChatWidth(t *testing.T, page playwright.Page) float64 {
	t.Helper()
	result, err := page.Evaluate(`() => {
		const chat = document.getElementById('chat-container');
		if (!chat) return 0;
		return chat.getBoundingClientRect().width;
	}`)
	require.NoError(t, err)
	return toFloat64(result)
}

func getMainWidth(t *testing.T, page playwright.Page) float64 {
	t.Helper()
	result, err := page.Evaluate(`() => {
		const main = document.querySelector('main');
		if (!main) return 0;
		return main.getBoundingClientRect().width;
	}`)
	require.NoError(t, err)
	return toFloat64(result)
}
