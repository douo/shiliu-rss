import { defineConfig, devices } from '@playwright/test';
import { readFileSync } from 'node:fs';
const env = Object.fromEntries(
  readFileSync(new URL('../config/dev.env', import.meta.url), 'utf8')
    .split('\n')
    .filter((x) => x && !x.startsWith('#'))
    .map((x) => x.split('='))
);
const baseURL = process.env.SHILIU_E2E_URL || `http://127.0.0.1:${env.SHILIU_WEB_PORT}`;
export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  timeout: 45_000,
  expect: { timeout: 10_000 },
  reporter: 'list',
  use: { baseURL, trace: 'retain-on-failure' },
  projects: [
    {
      name: 'desktop',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 900 } }
    },
    { name: 'mobile', use: { ...devices['iPhone 13'], defaultBrowserType: 'chromium' } }
  ],
  webServer: process.env.SHILIU_E2E_URL
    ? undefined
    : { command: 'pnpm dev', url: baseURL, reuseExistingServer: !process.env.CI, timeout: 30_000 }
});
