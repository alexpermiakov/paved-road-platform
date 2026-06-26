import { defineConfig } from '@playwright/test';

// Base URL of the live deployment under test — the ephemeral dev cluster's ALB.
// Required: there is no sensible default for a smoke test (refuse to "pass"
// silently against nothing).
const baseURL = process.env.SMOKE_BASE_URL;
if (!baseURL) {
  throw new Error('SMOKE_BASE_URL is required (e.g. http://<alb-hostname>)');
}

export default defineConfig({
  // One spec per service under services/; shared helpers live in lib/ and are
  // not collected as tests. Adding a service = drop in services/<name>.spec.ts.
  testDir: './services',
  // A freshly provisioned cluster may still be syncing when smoke starts, and
  // traffic crosses a real ALB — keep timeouts generous and retry transient hops.
  timeout: 60_000,
  expect: { timeout: 15_000 },
  retries: 2,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL,
    extraHTTPHeaders: { Accept: 'application/json' },
    ignoreHTTPSErrors: true,
  },
});
