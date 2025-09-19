const { defineConfig, devices } = require('@playwright/test');

module.exports = defineConfig({
  testDir: 'test/ui',
  timeout: 30000,
  expect: {
    timeout: 5000,
  },
  reporter: [['list']],
  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:8080',
    headless: !!process.env.CI,
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  workers: parseInt(process.env.PLAYWRIGHT_WORKERS || '1', 10),
});
