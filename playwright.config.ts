import { defineConfig } from '@playwright/test';

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
    headless: false,
    // Use a real headed browser window so Chromium's OS window can be larger
    // than the emulated page viewport. Tests that need a deterministic viewport
    // still call page.setViewportSize(), but the visible browser window stays large
    // enough to show the entire desktop without manual stretching.
    viewport: null,
    screen: { width: 1324, height: 931 },
    launchOptions: {
      args: [
        '--autoplay-policy=no-user-gesture-required',
        // Headed Chromium on native Wayland produces fractional viewport overshoot here.
        // Force X11/XWayland so CSS pixels match Playwright's requested viewport exactly.
        '--ozone-platform=x11',
        '--window-size=1324,931'
      ]
    },
    trace: 'on-first-retry',
    video: 'on',
  },
  projects: [
    {
      name: 'chromium',
    },
    // {
    //   name: 'webkit',
    //   use: { ...devices['Desktop Safari'] },
    // },
  ],
});
