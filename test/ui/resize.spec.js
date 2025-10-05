const { test, expect } = require('@playwright/test');
const { resolveBaseURL } = require('./helpers/navigation');

async function setViewportAndDispatch(page, size) {
  await page.setViewportSize(size);
  await page.waitForTimeout(100);
  await page.evaluate(() => {
    window.dispatchEvent(new Event('resize'));
  });
  await page.waitForTimeout(200);
}

async function ensureMessages(page) {
  await page.evaluate(() => {
    const messagesDiv = document.getElementById('messages');
    if (!messagesDiv) {
      throw new Error('messages container missing');
    }
    if (messagesDiv.children.length >= 30) {
      return;
    }
    for (let i = messagesDiv.children.length; i < 30; i++) {
      messagesDiv.insertAdjacentHTML(
        'beforeend',
        `<div class="flex mb-4"><div class="bg-blue-100 text-gray-900 rounded-lg p-3 max-w-[80%]">Test message ${i}</div></div>`
      );
    }
  });
}

async function getChatState(page) {
  return page.evaluate(() => {
    const chat = document.getElementById('chat-container');
    if (!chat) {
      return { missing: true };
    }
    const classes = Array.from(chat.classList);
    return {
      missing: false,
      classes,
      hasExpanded: chat.classList.contains('chat-expanded'),
      hasMinimized: chat.classList.contains('chat-minimized'),
    };
  });
}

test.describe('Responsive resizing behaviour', () => {
  test.beforeEach(async ({ page }, testInfo) => {
    const baseURL = resolveBaseURL(testInfo);
    await page.goto(baseURL, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('#chat-container');
    await ensureMessages(page);
  });

  test('chat container toggles layout classes across breakpoints', async ({ page }) => {
    await setViewportAndDispatch(page, { width: 1280, height: 720 });
    const desktopState = await getChatState(page);
    expect(desktopState.missing).toBeFalsy();

    await setViewportAndDispatch(page, { width: 375, height: 812 });
    const mobileState = await getChatState(page);
    expect(mobileState.missing).toBeFalsy();

    expect(mobileState.classes.join(' ')).not.toEqual(desktopState.classes.join(' '));
  });

  test('scroll position is preserved when returning to previous viewport', async ({ page }) => {
    await setViewportAndDispatch(page, { width: 375, height: 812 });

    const targetScroll = await page.evaluate(() => {
      const messagesDiv = document.getElementById('messages');
      messagesDiv.scrollTop = messagesDiv.scrollHeight / 2;
      return messagesDiv.scrollTop;
    });

    await setViewportAndDispatch(page, { width: 1280, height: 720 });
    await setViewportAndDispatch(page, { width: 375, height: 812 });

    const restored = await page.evaluate(() => {
      const messagesDiv = document.getElementById('messages');
      return messagesDiv.scrollTop;
    });

    expect(Math.abs(restored - targetScroll)).toBeLessThan(60);
  });

  test('smooth scrolling continues through resize events', async ({ page }) => {
    await setViewportAndDispatch(page, { width: 1280, height: 720 });

    const before = await page.evaluate(async () => {
      const messagesDiv = document.getElementById('messages');
      messagesDiv.scrollTop = messagesDiv.scrollHeight;
      messagesDiv.scrollTo({ top: 0, behavior: 'smooth' });
      return { scrollHeight: messagesDiv.scrollHeight };
    });

    await page.waitForTimeout(100);
    await setViewportAndDispatch(page, { width: 375, height: 812 });
    await page.waitForTimeout(400);
    await setViewportAndDispatch(page, { width: 1280, height: 720 });
    await page.waitForTimeout(300);

    const after = await page.evaluate(() => {
      const messagesDiv = document.getElementById('messages');
      return {
        finalScroll: messagesDiv.scrollTop,
        scrollHeight: messagesDiv.scrollHeight,
      };
    });

    expect(after.scrollHeight).toBe(before.scrollHeight);
    expect(after.finalScroll).toBeGreaterThan(-1);
  });
});
