const { test, expect } = require('@playwright/test');

test.describe('File dropdown during SSE updates', () => {
  test.beforeEach(async ({ page }, testInfo) => {
    await page.goto(testInfo.config.use.baseURL, { waitUntil: 'domcontentloaded' });
    await page.getByRole('button', { name: 'Code' }).click();
    await page.waitForSelector('#file-selector');
  });

  test('focus is preserved across synthetic SSE refreshes', async ({ page }) => {
    const result = await page.evaluate(() => {
      const select = document.getElementById('file-selector');
      if (!select) {
        return { ok: false, reason: 'missing dropdown' };
      }

      select.focus();
      const initialFocused = document.activeElement === select;

      document.body.dispatchEvent(new CustomEvent('htmx:sseMessage'));

      return {
        ok: initialFocused && document.activeElement === select,
        initialFocused,
        afterFocused: document.activeElement === select,
      };
    });

    expect(result.ok).toBeTruthy();
  });

  test('selection persists when options change', async ({ page }) => {
    const outcome = await page.evaluate(() => {
      const select = document.getElementById('file-selector');
      if (!select) {
        return { ok: false, reason: 'missing dropdown' };
      }

      const optionA = document.createElement('option');
      optionA.value = 'playwright-a.txt';
      optionA.textContent = 'playwright-a.txt';
      select.appendChild(optionA);

      select.value = optionA.value;
      select.focus();

      const optionB = document.createElement('option');
      optionB.value = 'playwright-b.txt';
      optionB.textContent = 'playwright-b.txt';

      // Simulate DOM churn from SSE update
      const previousValue = select.value;
      select.appendChild(optionB);
      document.body.dispatchEvent(new CustomEvent('htmx:sseMessage'));

      return {
        ok: document.activeElement === select && select.value === previousValue,
        selection: select.value,
      };
    });

    expect(outcome.ok).toBeTruthy();
    expect(outcome.selection).toBe('playwright-a.txt');
  });
});
