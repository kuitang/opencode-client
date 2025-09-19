const { test, expect } = require('@playwright/test');
const { resolveBaseURL } = require('./helpers/navigation');
const { runCommandInTerminal } = require('./helpers/terminal-actions');

const APP_URL = process.env.TERMINAL_APP_URL;
const GOTTY_URL = process.env.TERMINAL_GOTTY_URL;

test.describe('Terminal tab', () => {
  test.beforeEach(async ({ page }, testInfo) => {
    const base = resolveBaseURL(testInfo, APP_URL);
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.getByRole('button', { name: 'Terminal' }).click();
    await page.waitForTimeout(1000);
  });

  const focusGottyTerminal = async (page) => {
    const terminal = page.locator('x-screen').first();
    await expect(terminal).toBeVisible({ timeout: 15000 });
    await terminal.click();
  };

  test('terminal iframe is rendered and interactive', async ({ page }) => {
    const iframe = page.locator('#terminal-iframe');
    await expect(iframe).toBeVisible();

    await runCommandInTerminal(page, 'echo "hello from playwright"');

    await page.waitForTimeout(500);
  });

  test('direct gotty access is available', async ({ page }) => {
    test.skip(!GOTTY_URL, 'Set TERMINAL_GOTTY_URL to run direct gotty checks');

    await page.goto(GOTTY_URL, { waitUntil: 'domcontentloaded' });
    await expect(page).toHaveTitle(/GoTTY|gotty/i);
  });

  test('files created through terminal appear in Code tab', async ({ page }, testInfo) => {
    test.skip(!GOTTY_URL, 'Set TERMINAL_GOTTY_URL to verify file sync through terminal');

    const filename = `playwright-terminal-${Date.now()}.txt`;

    await page.goto(GOTTY_URL, { waitUntil: 'domcontentloaded' });
    await focusGottyTerminal(page);

    await page.keyboard.type(`echo "terminal content" > ${filename}`);
    await page.keyboard.press('Enter');
    await page.keyboard.type(`cat ${filename}`);
    await page.keyboard.press('Enter');
    await page.waitForTimeout(1000);

    const base = resolveBaseURL(testInfo, APP_URL);
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.getByRole('button', { name: 'Code' }).click();
    await page.waitForTimeout(1000);

    const codeContent = await page.locator('#main-content').textContent();
    expect(codeContent).toContain(filename);
  });
});
