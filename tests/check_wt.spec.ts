import { test, expect } from '@playwright/test';
test('check webtransport', async ({ page }) => {
  const result = await page.evaluate(() => {
    return {
      hasWebTransport: 'WebTransport' in window,
      isSecureContext: window.isSecureContext
    };
  });
  console.log('Browser Info:', JSON.stringify(result));
});
