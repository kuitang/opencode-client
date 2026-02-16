package e2e

import (
	"fmt"
	"testing"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

// checkAlignment evaluates whether the chat header and tab navigation are
// aligned (bottom edges within 1px of each other).
func checkAlignment(t *testing.T, page playwright.Page) map[string]interface{} {
	t.Helper()
	result, err := page.Evaluate(`() => {
		const chatHeader = document.querySelector('header');
		const tabNav = document.querySelector('[data-testid="tab-navigation"]');
		if (!chatHeader || !tabNav) {
			return {
				ok: false,
				reason: 'Missing header or nav',
				headerPresent: !!chatHeader,
				navPresent: !!tabNav,
			};
		}
		const headerRect = chatHeader.getBoundingClientRect();
		const navRect = tabNav.getBoundingClientRect();
		const difference = Math.abs(headerRect.bottom - navRect.bottom);
		return {
			ok: difference <= 1,
			difference: difference,
			headerHeight: headerRect.height,
			navHeight: navRect.height,
			headerPresent: true,
			navPresent: true,
		};
	}`)
	require.NoError(t, err)

	m, ok := result.(map[string]interface{})
	require.True(t, ok, "checkAlignment should return a map")
	return m
}

func layoutFlushBeforeEach(t *testing.T, page playwright.Page) {
	t.Helper()
	_, err := page.Goto(baseURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	})
	require.NoError(t, err)

	_, err = page.WaitForSelector("header")
	require.NoError(t, err)
	_, err = page.WaitForSelector("[data-testid=\"tab-navigation\"]")
	require.NoError(t, err)
}

func TestLayoutFlush(t *testing.T) {
	t.Run("chat header and tab nav are flush on desktop", func(t *testing.T) {
		page := newPage(t)
		layoutFlushBeforeEach(t, page)

		err := page.SetViewportSize(1280, 720)
		require.NoError(t, err)

		result := checkAlignment(t, page)
		require.True(t, result["ok"].(bool),
			"header and nav should be flush; difference=%v", result["difference"])
	})

	t.Run("alignment holds across common breakpoints", func(t *testing.T) {
		page := newPage(t)
		layoutFlushBeforeEach(t, page)

		// Desktop viewports: chat header and tab nav should be flush (side by side)
		desktopViewports := []struct{ width, height int }{
			{1024, 768},
			{1280, 720},
		}
		for _, vp := range desktopViewports {
			t.Run(fmt.Sprintf("%dx%d_flush", vp.width, vp.height), func(t *testing.T) {
				err := page.SetViewportSize(vp.width, vp.height)
				require.NoError(t, err)

				result := checkAlignment(t, page)
				require.True(t, result["ok"].(bool),
					"alignment should hold at %dx%d; difference=%v",
					vp.width, vp.height, result["difference"])
			})
		}

		// Mobile viewports: elements stacked vertically, both should be visible
		mobileViewports := []struct{ width, height int }{
			{375, 667},
			{768, 1024},
		}
		for _, vp := range mobileViewports {
			t.Run(fmt.Sprintf("%dx%d_stacked", vp.width, vp.height), func(t *testing.T) {
				err := page.SetViewportSize(vp.width, vp.height)
				require.NoError(t, err)

				headerVisible, err := page.Locator("header").IsVisible()
				require.NoError(t, err)
				require.True(t, headerVisible, "chat header should be visible at %dx%d", vp.width, vp.height)

				navVisible, err := page.Locator("[data-testid=\"tab-navigation\"]").IsVisible()
				require.NoError(t, err)
				require.True(t, navVisible, "tab nav should be visible at %dx%d", vp.width, vp.height)
			})
		}
	})
}
