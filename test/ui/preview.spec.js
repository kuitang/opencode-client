const { test, expect } = require('@playwright/test');
const { spawn } = require('child_process');

const BASE_TIMEOUT = parseInt(process.env.PREVIEW_TEST_TIMEOUT || '60000', 10);

function startPythonServer(port) {
  const server = spawn('python3', ['-m', 'http.server', String(port)], {
    stdio: 'ignore',
    detached: false,
  });
  return server;
}

test.describe.configure({ mode: 'serial' });

test.describe('Preview tab experience', () => {
  test.beforeEach(async ({ page, baseURL }) => {
    await page.goto(baseURL, { waitUntil: 'networkidle' });
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
    const testPort = parseInt(process.env.PREVIEW_TEST_SERVER_PORT || '5555', 10);
    const server = startPythonServer(testPort);

    try {
      // Give the server time to boot.
      await page.waitForTimeout(2000);

      await page.getByRole('button', { name: 'Preview' }).click();
      await page.waitForTimeout(500);
      const refreshButton = page.getByRole('button', { name: 'Refresh' }).first();
      await refreshButton.click();

      const killButton = page.getByRole('button', { name: 'Kill' });
      await expect(killButton).toBeVisible({ timeout: BASE_TIMEOUT });

      await killButton.click();

      await expect(page.getByText('No Application Running', { exact: false })).toBeVisible({ timeout: BASE_TIMEOUT });
    } finally {
      if (server && !server.killed) {
        server.kill('SIGTERM');
      }
    }
  });
});
