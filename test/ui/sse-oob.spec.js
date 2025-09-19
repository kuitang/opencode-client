const { test, expect } = require('@playwright/test');
const { promisify } = require('util');
const { exec } = require('child_process');

const execAsync = promisify(exec);
const CONTAINER = process.env.PLAYWRIGHT_SANDBOX_CONTAINER;

function parseCount(text) {
  const match = text && text.match(/\d+/);
  return match ? Number(match[0]) : 0;
}

async function createSandboxFiles(container) {
  await execAsync(`docker exec ${container} sh -c "cd /app && echo 'test content' > sse_test1.txt && echo 'def foo(): pass' > sse_test2.py"`);
}

async function cleanupSandboxFiles(container) {
  await execAsync(`docker exec ${container} sh -c "cd /app && rm -f sse_test1.txt sse_test2.py"`);
}

test.describe('SSE out-of-band updates', () => {
  test.skip(!CONTAINER, 'Set PLAYWRIGHT_SANDBOX_CONTAINER to run SSE OOB tests');

  test.beforeEach(async ({ page }, testInfo) => {
    await page.goto(testInfo.config.use.baseURL, { waitUntil: 'domcontentloaded' });
    await page.getByRole('button', { name: 'Code' }).click();
    await page.waitForSelector('#file-count-container');
  });

  test('code tab stats update when sandbox files change', async ({ page }) => {
    const initialFilesText = await page.locator('#file-count-container p:first-child').textContent();
    const initialLinesText = await page.locator('#line-count-container p:first-child').textContent();
    const initialOptions = await page.locator('#file-selector option').count();

    await createSandboxFiles(CONTAINER);

    try {
      await page.fill('#message-input', 'List the files in the current directory');
      await page.click('button[type="submit"]');

      await page.waitForFunction(
        (initial) => {
          const current = document.querySelector('#file-count-container p:first-child');
          return current && current.textContent !== initial;
        },
        initialFilesText,
        { timeout: 20000 },
      );

      const newFilesText = await page.locator('#file-count-container p:first-child').textContent();
      const newLinesText = await page.locator('#line-count-container p:first-child').textContent();
      const newOptions = await page.locator('#file-selector option').count();

      expect(parseCount(newFilesText)).toBeGreaterThan(parseCount(initialFilesText));
      expect(parseCount(newLinesText)).toBeGreaterThanOrEqual(parseCount(initialLinesText));
      expect(newOptions).toBeGreaterThan(initialOptions);

      const optionTexts = await page.locator('#file-selector option').allTextContents();
      expect(optionTexts.some((text) => text.includes('sse_test1.txt'))).toBeTruthy();
      expect(optionTexts.some((text) => text.includes('sse_test2.py'))).toBeTruthy();
    } finally {
      await cleanupSandboxFiles(CONTAINER);
    }
  });
});
