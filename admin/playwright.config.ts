import { existsSync } from 'node:fs';

import { defineConfig } from '@playwright/test';

function localChromiumExecutable() {
  const configured = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH;
  if (configured && existsSync(configured)) return configured;

  const candidates =
    process.platform === 'darwin'
      ? ['/Applications/Google Chrome.app/Contents/MacOS/Google Chrome']
      : process.platform === 'win32'
        ? [
            `${process.env.PROGRAMFILES ?? ''}\\Google\\Chrome\\Application\\chrome.exe`,
            `${process.env['PROGRAMFILES(X86)'] ?? ''}\\Google\\Chrome\\Application\\chrome.exe`,
          ]
        : [
            '/usr/bin/google-chrome',
            '/usr/bin/chromium',
            '/usr/bin/chromium-browser',
          ];
  return candidates.find((candidate) => candidate && existsSync(candidate));
}

const executablePath = process.env.CI ? undefined : localChromiumExecutable();

export default defineConfig({
  testDir: './tests/e2e',
  outputDir: './test-results/e2e',
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: [['line']],
  use: {
    headless: true,
    locale: 'zh-CN',
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    launchOptions: executablePath ? { executablePath } : {},
  },
  webServer: [
    {
      command:
        'node ./tests/e2e/static-server.mjs --root ./apps/web-antd/dist --port 4173',
      url: 'http://127.0.0.1:4173/auth/login',
      reuseExistingServer: !process.env.CI,
    },
    {
      command:
        'node ./tests/e2e/static-server.mjs --root ./apps/web-ele/dist --port 4174',
      url: 'http://127.0.0.1:4174/auth/login',
      reuseExistingServer: !process.env.CI,
    },
    {
      command:
        'node ./tests/e2e/static-server.mjs --root ./apps/web-naive/dist --port 4175',
      url: 'http://127.0.0.1:4175/auth/login',
      reuseExistingServer: !process.env.CI,
    },
    {
      command:
        'node ./tests/e2e/static-server.mjs --root ./apps/install/dist --mount /install --port 4176',
      url: 'http://127.0.0.1:4176/install',
      reuseExistingServer: !process.env.CI,
    },
  ],
  projects: [
    { name: 'web-antd', use: { baseURL: 'http://127.0.0.1:4173' } },
    { name: 'web-ele', use: { baseURL: 'http://127.0.0.1:4174' } },
    { name: 'web-naive', use: { baseURL: 'http://127.0.0.1:4175' } },
    { name: 'installer', use: { baseURL: 'http://127.0.0.1:4176' } },
  ],
});
