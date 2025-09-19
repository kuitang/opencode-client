const { test, expect } = require('@playwright/test');
const { resolveBaseURL } = require('./helpers/navigation');
const { runCommandInTerminal } = require('./helpers/terminal-actions');

const BASE_TIMEOUT = parseInt(process.env.PREVIEW_TEST_TIMEOUT || '60000', 10);

test.describe.configure({ mode: 'serial' });

test.describe('Preview tab experience', () => {
  test.beforeEach(async ({ page }, testInfo) => {
    const baseURL = resolveBaseURL(testInfo);
    await page.goto(baseURL, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('#message-input', { timeout: BASE_TIMEOUT });
  });

  test('renders preview content on first load', async ({ page }) => {
    const mainContent = page.locator('#main-content');
    await expect(mainContent).toBeVisible();

    // Ensure the old placeholder is gone
    await expect(mainContent).not.toContainText('Welcome to VibeCoding');

    const iframeCount = await page.locator('#preview-iframe').count();
    if (iframeCount === 0) {
      await expect(page.getByText('No Application Running', { exact: false })).toBeVisible();
    }
  });

  test('switching between Code and Preview preserves preview state', async ({ page }) => {
    await page.getByRole('button', { name: 'Code' }).click();
    await page.waitForTimeout(500);

    await page.getByRole('button', { name: 'Preview' }).click();
    await page.waitForTimeout(500);

    const mainContent = page.locator('#main-content');
    const hasIframe = await page.locator('#preview-iframe').count();
    const hasFallback = await mainContent.locator('text=No Application Running').count();

    expect(hasIframe > 0 || hasFallback > 0).toBeTruthy();
  });

  test('shows and uses Kill button when an external server is detected', async ({ page }) => {
    test.setTimeout(BASE_TIMEOUT + 15000);
    const testPort = parseInt(process.env.PREVIEW_TEST_SERVER_PORT || '5555', 10);
    const startCommand = `python3 -m http.server ${testPort} --bind 0.0.0.0 >/tmp/playwright-preview-${testPort}.log 2>&1 &`;
    const killCommand = `kill $(lsof -t -i:${testPort}) 2>/dev/null || true`;

    let cleanupNeeded = true;

    try {
      await page.getByRole('button', { name: 'Terminal' }).click();
      await runCommandInTerminal(page, startCommand);
      await page.waitForTimeout(2000);

      await page.getByRole('button', { name: 'Preview' }).click();
      await page.waitForTimeout(500);

      const refreshButton = page.getByRole('button', { name: 'Refresh' }).first();
      await refreshButton.click();

      const killButton = page.getByRole('button', { name: 'Kill' });
      await expect(killButton).toBeVisible({ timeout: BASE_TIMEOUT });

      await killButton.click({ noWaitAfter: true });

      await expect(page.getByText('No Application Running', { exact: false })).toBeVisible({ timeout: BASE_TIMEOUT });
      cleanupNeeded = false;
    } finally {
      if (!page.isClosed()) {
        await page.getByRole('button', { name: 'Terminal' }).click();
        await runCommandInTerminal(page, killCommand);
        await page.waitForTimeout(cleanupNeeded ? 1000 : 500);
      }
    }
  });
});
