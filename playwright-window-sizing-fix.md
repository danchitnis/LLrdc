# Playwright Browser Window Sizing Bug

## Summary

The remote desktop was not actually being cropped by the app layout in normal browser use.
The bug was specific to headed Playwright Chromium on this machine.

The visible browser window opened too small, so the bottom of the remote desktop was hidden unless the window was manually stretched.

## Symptoms

- Normal browser usage looked correct.
- The issue was most obvious in the viewport-scaling test.
- The page content itself could report the expected viewport size, but the actual headed browser window still did not visibly show the full desktop.
- An early attempted fix removed some layout drift, but still left the real OS window too small.

## Root Cause

There were two separate Playwright/browser issues interacting:

1. Native Wayland headed Chromium produced fractional layout overshoot.

   - On this setup, a requested `1280x800` viewport could lay out as roughly `1280.447 x 800.447`.
   - That made element bounds slightly larger than `window.innerWidth/innerHeight`.
   - This was a Playwright headed Chromium + Wayland environment issue, not a viewer sizing bug.

2. The Playwright project config was still overriding the intended real-window settings.

   - `projects.chromium.use = { ...devices['Desktop Chrome'] }` reintroduced Playwright's fixed device viewport.
   - That cancelled the attempt to use a larger real browser window.
   - As a result, the headed browser still needed manual stretching even after page geometry looked better.

## What Did Not Fix It

- Rewriting only the viewport assertions.
- Changing viewer CSS/layout alone.
- Forcing CSS `zoom`, transforms, or JS `window.resizeTo(...)`.
- Setting a larger `--window-size` while still using the fixed Desktop Chrome device profile.

## Final Fix

The final stable fix was in [`playwright.config.ts`](/home/danial/code/LLrdc/playwright.config.ts):

1. Stop using the `Desktop Chrome` device override for the Chromium project.

   - That override was restoring a fixed Playwright device viewport and defeating the real-window fix.

2. Run headed Chromium through X11/XWayland instead of native Wayland.

   - Use `--ozone-platform=x11`.
   - This removed the fractional viewport/layout overshoot.

3. Use a real browser window instead of a globally fixed Playwright viewport.

   - Set `use.viewport = null`.
   - Keep per-test `page.setViewportSize(...)` calls for deterministic page content size.

4. Set the outer window size to match this environment's X11 chrome delta.

   - `screen: { width: 1324, height: 931 }`
   - `--window-size=1324,931`
   - On this machine, that yields an inner page viewport of `1280x800` without extra bottom cropping and without unnecessary right-side slack.

## Why `1324x931`

This was measured empirically on this machine.

- Outer window: `1324x931`
- Inner viewport after `page.setViewportSize({ width: 1280, height: 800 })`: `1280x800`

This means the effective browser chrome overhead here is:

- Width delta: `44px`
- Height delta: `131px`

Those values are environment-specific and should not be assumed portable across all desktops/window managers.

## Important Test Conditions

For the WebRTC tests involved in this issue:

- Run with a single Playwright worker.
- Use `--host-net` for the llrdc container, otherwise WebRTC may fail for unrelated reasons.

## How To Debug This Again

If this regresses in the future, check these in order:

1. Verify the Chromium project is not reapplying a device preset like `Desktop Chrome`.
2. Log both page and outer-window metrics:

   - `window.innerWidth`
   - `window.innerHeight`
   - `window.outerWidth`
   - `window.outerHeight`
   - `screen.availWidth`
   - `screen.availHeight`

3. Check whether headed Chromium is running on native Wayland instead of X11/XWayland.
4. Confirm the test is using a real window (`viewport: null`) and not only an emulated viewport.
5. Re-measure the correct `--window-size` for the current desktop environment if window-manager chrome changed.

## Key Lesson

This looked like a remote-desktop/video sizing bug, but the real problem was Playwright headed browser window geometry.

Future fixes should debug:

- page viewport size,
- actual outer browser window size,
- compositor/backend choice (Wayland vs X11),
- and config overrides from Playwright device presets

before changing viewer layout code.
