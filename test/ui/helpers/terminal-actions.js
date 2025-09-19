const { expect } = require('@playwright/test');

async function focusTerminal(page) {
  const frame = page.frameLocator('#terminal-iframe');
  await expect(frame.locator('body')).toBeAttached({ timeout: 15000 });

  const xtermScreen = frame.locator('x-screen');
  if (await xtermScreen.count()) {
    await expect(xtermScreen.first()).toBeVisible({ timeout: 15000 });
    await xtermScreen.first().click();
    return frame;
  }

  const terminalInput = frame.getByRole('textbox', { name: /terminal input/i });
  await expect(terminalInput).toBeVisible({ timeout: 15000 });
  await terminalInput.click();
  return frame;
}

async function runCommandInTerminal(page, command) {
  await focusTerminal(page);
  await page.keyboard.type(command);
  await page.keyboard.press('Enter');
}

module.exports = {
  focusTerminal,
  runCommandInTerminal,
};
