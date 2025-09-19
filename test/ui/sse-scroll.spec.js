const { test, expect } = require('@playwright/test');

async function runScrollChecks(page) {
  return page.evaluate(async () => {
    const results = [];
    const messagesDiv = document.getElementById('messages');
    if (!messagesDiv) {
      return { success: false, error: 'Messages container not found' };
    }

    const isAtBottom = () => Math.abs(messagesDiv.scrollHeight - messagesDiv.scrollTop - messagesDiv.clientHeight) < 10;

    // Initial load should be at bottom
    results.push({
      test: 'Initial load scrolls to bottom',
      passed: isAtBottom(),
    });

    // Auto-scroll when at bottom
    messagesDiv.scrollTop = messagesDiv.scrollHeight;
    messagesDiv.insertAdjacentHTML('beforeend', '<div id="assistant-test1" class="my-2">Test message 1</div>');
    document.body.dispatchEvent(new CustomEvent('htmx:sseMessage'));
    await new Promise((resolve) => setTimeout(resolve, 100));
    results.push({
      test: 'Auto-scrolls when at bottom',
      passed: isAtBottom(),
    });

    // Preserve position when user scrolls up
    messagesDiv.scrollTop = messagesDiv.scrollHeight / 2;
    const midPosition = messagesDiv.scrollTop;
    messagesDiv.insertAdjacentHTML('beforeend', '<div id="assistant-test2" class="my-2">Test message 2</div>');
    document.body.dispatchEvent(new CustomEvent('htmx:sseMessage'));
    await new Promise((resolve) => setTimeout(resolve, 100));
    results.push({
      test: 'Preserves position when scrolled up',
      passed: Math.abs(messagesDiv.scrollTop - midPosition) < 50,
    });

    // Threshold behaviour near bottom
    messagesDiv.scrollTop = messagesDiv.scrollHeight - messagesDiv.clientHeight - 50;
    messagesDiv.insertAdjacentHTML('beforeend', '<div id="assistant-test3" class="my-2">Test message 3</div>');
    document.body.dispatchEvent(new CustomEvent('htmx:sseMessage'));
    await new Promise((resolve) => setTimeout(resolve, 100));
    results.push({
      test: 'Auto-scrolls within threshold',
      passed: isAtBottom(),
    });

    // Rapid updates stay at bottom
    messagesDiv.scrollTop = messagesDiv.scrollHeight;
    for (let i = 0; i < 5; i++) {
      messagesDiv.insertAdjacentHTML('beforeend', `<div id="assistant-rapid${i}" class="my-2">Rapid ${i}</div>`);
      document.body.dispatchEvent(new CustomEvent('htmx:sseMessage'));
      await new Promise((resolve) => setTimeout(resolve, 20));
    }
    results.push({
      test: 'Handles rapid message updates',
      passed: isAtBottom(),
    });

    return {
      success: results.every((entry) => entry.passed !== false),
      results,
    };
  });
}

test.describe('SSE scroll behaviour', () => {
  test.beforeEach(async ({ page }, testInfo) => {
    await page.goto(testInfo.config.use.baseURL, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('#messages');
  });

  test('auto-scroll logic respects user position', async ({ page }) => {
    const outcome = await runScrollChecks(page);
    expect(outcome.success).toBeTruthy();
  });
});
