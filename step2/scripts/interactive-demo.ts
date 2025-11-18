#!/usr/bin/env node
/**
 * Step 2 demo: launch a headless sway session, start gedit, inject keystrokes
 * with wtype, and capture a screenshot for verification.
 */
import { spawn, spawnSync, ChildProcess } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const PROJECT_ROOT = path.resolve(__dirname, '..');
const LOCAL_TMP_DIR =
  process.env.LOCAL_TMP_DIR || path.join(PROJECT_ROOT, '.temp');
fs.mkdirSync(LOCAL_TMP_DIR, { recursive: true });

const WAYLAND_SOCKET = process.env.WAYLAND_SOCKET || 'remote-desktop-2';
const DEMO_TEXT =
  process.env.DEMO_TEXT || 'Hello from remote desktop step 2!';
const DEMO_APP = process.env.DEMO_APP || 'gedit';

const SCREENSHOT_PATH =
  process.env.OUTPUT_PATH ||
  path.join(LOCAL_TMP_DIR, 'step2-demo.png');
const XDG_RUNTIME_DIR =
  process.env.XDG_RUNTIME_DIR ||
  fs.mkdtempSync(path.join(os.tmpdir(), 'remote-desktop-step2-'));
fs.chmodSync(XDG_RUNTIME_DIR, 0o700);
const XDG_INFO_FILE = path.join(LOCAL_TMP_DIR, 'xdg-runtime-dir.txt');
fs.writeFileSync(XDG_INFO_FILE, XDG_RUNTIME_DIR, 'utf8');

const REQUIRED_BINARIES = ['sway', 'grim', 'wtype', DEMO_APP];
const cleanupTasks = [
  () => {
    try {
      fs.rmSync(XDG_RUNTIME_DIR, { recursive: true, force: true });
    } catch (_) {
      // ignore
    }
  },
];
let compositorProcess: ChildProcess | undefined;
let appProcess: ChildProcess | undefined;

function ensureBinaries() {
  for (const binary of REQUIRED_BINARIES) {
    const result = spawnSync('which', [binary], { stdio: 'ignore' });
    if (result.status !== 0) {
      throw new Error(
        `Missing dependency "${binary}". Install it before running this script.`,
      );
    }
  }
}

function wait(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function startSway() {
  const env = {
    ...process.env,
    WAYLAND_DISPLAY: '',
    XDG_RUNTIME_DIR,
    WLR_BACKENDS: 'headless',
    WLR_LIBINPUT_NO_DEVICES: '1',
    WLR_RENDERER: 'pixman',
  };
  compositorProcess = spawn('sway', ['--debug'], { env, stdio: 'inherit' });
  cleanupTasks.push(() => {
    if (compositorProcess && !compositorProcess.killed) {
      compositorProcess.kill('SIGTERM');
    }
  });
  process.on('SIGINT', shutdown);
  process.on('SIGTERM', shutdown);
  await waitForSocketOrExit();
  console.log(`Headless sway ready (XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR}).`);
}

function waitForWaylandSocket(timeoutMs = 5000): Promise<string> {
  const start = Date.now();
  return new Promise((resolve, reject) => {
    (function poll() {
      try {
        const entries = fs.readdirSync(XDG_RUNTIME_DIR);
        const socket = entries.find((entry) => entry.startsWith('wayland-'));
        if (socket) {
          const target = path.join(XDG_RUNTIME_DIR, WAYLAND_SOCKET);
          const source = path.join(XDG_RUNTIME_DIR, socket);
          if (target !== source) {
            try {
              fs.rmSync(target);
            } catch (_) {
              // ignore
            }
            fs.symlinkSync(source, target);
          }
          return resolve(socket);
        }
      } catch (_) {
        // directory may not be ready yet
      }
      if (Date.now() - start > timeoutMs) {
        return reject(
          new Error(
            `Timed out waiting for Wayland socket after ${timeoutMs}ms`,
          ),
        );
      }
      setTimeout(poll, 100);
    })();
  });
}

function waitForSocketOrExit(): Promise<string> {
  return new Promise((resolve, reject) => {
    const onExit = (code: number | null) => {
      reject(new Error(`sway exited before exposing a socket (code ${code})`));
    };
    if (compositorProcess) compositorProcess.once('exit', onExit);
    waitForWaylandSocket()
      .then((socket) => {
        if (compositorProcess) compositorProcess.off('exit', onExit);
        resolve(socket);
      })
      .catch((err) => {
        if (compositorProcess) compositorProcess.off('exit', onExit);
        reject(err);
      });
  });
}

function launchDemoApp() {
  const env = {
    ...process.env,
    WAYLAND_DISPLAY: WAYLAND_SOCKET,
    XDG_RUNTIME_DIR,
  };
  console.log(`Launching ${DEMO_APP} inside nested Wayland session...`);
  appProcess = spawn(DEMO_APP, [], { env, stdio: 'ignore' });
  cleanupTasks.push(() => {
    if (appProcess && !appProcess.killed) {
      appProcess.kill('SIGTERM');
    }
  });
}

function typeTextWithWtype(text: string) {
  return new Promise<void>((resolve, reject) => {
    const env = {
      ...process.env,
      WAYLAND_DISPLAY: WAYLAND_SOCKET,
      XDG_RUNTIME_DIR,
    };
    const typer = spawn('wtype', ['-d', '80', '--', text], {
      env,
      stdio: 'inherit',
    });
    typer.on('exit', (code) => {
      if (code === 0) {
        resolve();
      } else {
        reject(new Error(`wtype exited with code ${code}`));
      }
    });
    typer.on('error', reject);
  });
}

function captureScreenshot() {
  return new Promise<void>((resolve, reject) => {
    const env = {
      ...process.env,
      WAYLAND_DISPLAY: WAYLAND_SOCKET,
      XDG_RUNTIME_DIR,
    };
    const args = [SCREENSHOT_PATH];
    const grim = spawn('grim', args, { env, stdio: 'inherit' });
    grim.on('exit', (code) => {
      if (code === 0) {
        resolve();
      } else {
        reject(new Error(`grim exited with code ${code}`));
      }
    });
    grim.on('error', reject);
  });
}

async function main() {
  ensureBinaries();
  await startSway();
  await wait(1000);
  launchDemoApp();
  console.log('Waiting for the app to initialize...');
  await wait(3000);
  console.log(`Typing demo text via wtype: "${DEMO_TEXT}"`);
  await typeTextWithWtype(DEMO_TEXT);
  await wait(1000);
  console.log('Capturing screenshot...');
  await captureScreenshot();
  console.log(`Screenshot saved to ${SCREENSHOT_PATH}`);
  console.log(`XDG runtime dir recorded at ${XDG_INFO_FILE}`);
  shutdown();
}

function shutdown() {
  while (cleanupTasks.length) {
    const fn = cleanupTasks.pop();
    try {
      if (fn) fn();
    } catch (err) {
      console.warn('Cleanup step failed:', err);
    }
  }
  process.exit(0);
}

main().catch((err) => {
  console.error(err);
  shutdown();
});
