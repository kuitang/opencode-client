const { test, expect } = require('@playwright/test');
const { resolveBaseURL } = require('./helpers/navigation');

async function checkAlignment(page) {
  return page.evaluate(() => {
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
      difference,
      headerHeight: headerRect.height,
      navHeight: navRect.height,
      headerPresent: true,
      navPresent: true,
    };
  });
}

test.describe('Layout alignment', () => {
  test.beforeEach(async ({ page }, testInfo) => {
    const baseURL = resolveBaseURL(testInfo);
    await page.goto(baseURL, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('header');
    await page.waitForSelector('[data-testid="tab-navigation"]');
  });

  test('chat header and tab nav are flush on desktop', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });
    const result = await checkAlignment(page);
    expect(result.ok).toBeTruthy();
  });

  test('alignment holds across common breakpoints', async ({ page }) => {
    const viewports = [
      { width: 375, height: 667 },
      { width: 768, height: 1024 },
      { width: 1024, height: 768 },
    ];

    for (const viewport of viewports) {
      await page.setViewportSize(viewport);
      const result = await checkAlignment(page);
      expect(result.ok).toBeTruthy();
    }
  });
});
