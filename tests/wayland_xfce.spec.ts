import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';

const CONTAINER_NAME = 'llrdc-wayland-xfce-test';
const PORT = '8083';

test.describe('Wayland XFCE Verification', () => {
  test.beforeAll(async () => {
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}

    console.log('Starting container for XFCE verification...');
    execSync(`docker run -d --name ${CONTAINER_NAME} -p ${PORT}:8080 -e PORT=8080 danchitnis/llrdc:wayland-latest`);
    
    // Give it plenty of time to boot XFCE
    await new Promise(r => setTimeout(r, 15000));
  });

  test.afterAll(async () => {
    console.log('Cleaning up container...');
    try {
      execSync(`docker rm -f ${CONTAINER_NAME} 2>/dev/null || true`);
    } catch (e) {}
  });

  test('should verify XFCE processes are running', async () => {
    const processes = execSync(`docker exec ${CONTAINER_NAME} ps aux`).toString();
    console.log('--- PROCESS LIST ---');
    console.log(processes);
    console.log('--- END LIST ---');

    // Debug logs from startup
    try {
      const xfceLog = execSync(`docker exec ${CONTAINER_NAME} cat /tmp/xfce.log`).toString();
      console.log('--- XFCE LOG ---');
      console.log(xfceLog);
      console.log('--- END XFCE LOG ---');
    } catch (e) {
      console.log('XFCE log not found yet');
    }

    // Core XFCE components
    expect(processes).toContain('xfsettingsd');
    expect(processes).toContain('xfce4-panel');
    expect(processes).toContain('xfdesktop');
    
    // Compositor and Helper
    expect(processes).toContain('labwc');
    expect(processes).toContain('wayland_mouse_client');
  });

  test('should verify WebRTC connection is stable', async ({ page }) => {
    await page.goto(`http://localhost:${PORT}`);
    const statusEl = page.locator('#status');
    await expect(statusEl).toHaveText(/WebRTC/i, { timeout: 30000 });
  });
});
