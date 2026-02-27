import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  // Run tests serially to avoid multiple concurrent Docker containers.
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  // Always run with a single worker to ensure one test file at a time.
  workers: 1,
  // Use a CLI reporter so the test run exits cleanly (no HTML report server).
  reporter: 'line',
  use: {
    trace: 'on-first-retry',
    video: 'on',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
    // {
    //   name: 'webkit',
    //   use: { ...devices['Desktop Safari'] },
    // },
  ],
});
