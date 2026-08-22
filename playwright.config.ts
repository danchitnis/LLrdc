import { defineConfig } from '@playwright/test';

const chromeExecutable = process.env.PLAYWRIGHT_CHROME_EXECUTABLE;
const launchArgs = [
  '--autoplay-policy=no-user-gesture-required',
  '--window-size=1324,931',
];

if (process.platform === 'linux') {
  launchArgs.splice(1, 0, '--ozone-platform=wayland');
}

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
    // Use a real headed installed Chrome window so the OS window can be larger
    // than the emulated page viewport. Tests that need a deterministic viewport
    // still call page.setViewportSize(), but the visible browser window stays large
    // enough to show the entire desktop without manual stretching.
    viewport: null,
    screen: { width: 1324, height: 931 },
    // The CPU browser runners set an explicit system Chrome executable. Keep
    // the Chrome channel fallback for direct Playwright use on other hosts.
    channel: chromeExecutable ? undefined : 'chrome',
    launchOptions: {
      ...(chromeExecutable ? { executablePath: chromeExecutable } : {}),
      args: launchArgs,
    },
    trace: 'on-first-retry',
    video: 'on',
  },
  projects: [
    {
      name: 'chrome',
    },
  ],
});
