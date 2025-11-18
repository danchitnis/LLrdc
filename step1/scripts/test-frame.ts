#!/usr/bin/env node
/**
 * Bootstraps a headless Wayland session (sway by default), captures a frame
 * with grim, and writes it through a FIFO so we can verify the pipeline.
 */
import { spawn, spawnSync, execSync, ChildProcess } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const PROJECT_ROOT = path.resolve(__dirname, '..');

const WAYLAND_SOCKET = process.env.WAYLAND_SOCKET || 'remote-desktop-1';
const WAYLAND_COMPOSITOR =
  process.env.WAYLAND_COMPOSITOR || 'sway';
const PIPE_PATH =
  process.env.PIPE_PATH || path.join(os.tmpdir(), 'remote-desktop-frame.pipe');
const LOCAL_TMP_DIR =
  process.env.LOCAL_TMP_DIR || path.join(PROJECT_ROOT, '.temp');
fs.mkdirSync(LOCAL_TMP_DIR, { recursive: true });
const OUTPUT_PATH =
  process.env.OUTPUT_PATH ||
  path.join(LOCAL_TMP_DIR, 'remote-desktop-test.png');
const XDG_RUNTIME_DIR =
  process.env.XDG_RUNTIME_DIR ||
  fs.mkdtempSync(path.join(os.tmpdir(), 'remote-desktop-xdg-'));
fs.chmodSync(XDG_RUNTIME_DIR, 0o700);
const CAPTURE_TIMEOUT_MS = Number(
  process.env.CAPTURE_TIMEOUT_MS || 10_000,
);
const WAYLAND_READY_TIMEOUT_MS = Number(
  process.env.WAYLAND_READY_TIMEOUT_MS || 5_000,
);

const REQUIRED_BINARIES = ['grim', 'mkfifo', WAYLAND_COMPOSITOR];
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
let waylandSocketSymlink: string | undefined;

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

function ensureFifo(pipePath: string) {
  try {
    const stats = fs.statSync(pipePath);
    if (!stats.isFIFO()) {
      fs.rmSync(pipePath);
      throw new Error('Recreating non-FIFO path at ' + pipePath);
    }
    fs.rmSync(pipePath);
  } catch {
    // File did not exist, continue.
  }
  execSync(`mkfifo ${pipePath}`);
  cleanupTasks.push(() => {
    try {
      fs.rmSync(pipePath);
    } catch (_) {
      // ignore
    }
  });
}

function startWeston() {
  const env = {
    ...process.env,
    XDG_RUNTIME_DIR,
    WAYLAND_DISPLAY: '',
  };
  compositorProcess = spawn(
    'weston',
    [
      '--backend=headless',
      `--socket=${WAYLAND_SOCKET}`,
      '--idle-time=0',
      '--log=/tmp/weston-remote-desktop.log',
    ],
    { env, stdio: 'inherit' },
  );
  cleanupTasks.push(() => {
    if (compositorProcess && !compositorProcess.killed) {
      compositorProcess.kill('SIGTERM');
    }
  });
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

  const actualSocket = await waitForSocketOrExit();
  if (actualSocket !== WAYLAND_SOCKET) {
    const target = path.join(XDG_RUNTIME_DIR, WAYLAND_SOCKET);
    const source = path.join(XDG_RUNTIME_DIR, actualSocket);
    try {
      fs.rmSync(target);
    } catch (_) {
      // ignore if it doesn't exist
    }
    fs.symlinkSync(source, target);
    waylandSocketSymlink = target;
    const symlinkPath = target;
    cleanupTasks.push(() => {
      try {
        fs.rmSync(symlinkPath);
      } catch (_) {
        // ignore
      }
    });
  }
}

function wait(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitForWaylandSocket() {
  const start = Date.now();
  while (Date.now() - start < WAYLAND_READY_TIMEOUT_MS) {
    try {
      const entries = fs.readdirSync(XDG_RUNTIME_DIR);
      const socketName = entries.find((entry) =>
        entry.startsWith('wayland-'),
      );
      if (socketName) {
        return socketName;
      }
    } catch (_) {
      // ignore until directory becomes available
    }
    await wait(100);
  }
  throw new Error(
    `Timed out waiting for Wayland compositor socket after ${WAYLAND_READY_TIMEOUT_MS}ms`,
  );
}

function waitForSocketOrExit(): Promise<string> {
  return new Promise((resolve, reject) => {
    const onExit = (code: number | null) => {
      reject(new Error(`sway exited before providing a socket (code ${code})`));
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

function captureFrame() {
  return new Promise<void>((resolve, reject) => {
    const reader = fs.createReadStream(PIPE_PATH);
    const writer = fs.createWriteStream(OUTPUT_PATH);
    reader.pipe(writer);

    const grimEnv = {
      ...process.env,
      XDG_RUNTIME_DIR,
      WAYLAND_DISPLAY: WAYLAND_SOCKET,
    };
    const grim = spawn('grim', [PIPE_PATH], {
      env: grimEnv,
      stdio: 'inherit',
    });

    const timeout = setTimeout(() => {
      grim.kill('SIGTERM');
      reader.destroy();
      writer.end();
      reject(
        new Error(
          `Timed out waiting for grim after ${CAPTURE_TIMEOUT_MS}ms`,
        ),
      );
    }, CAPTURE_TIMEOUT_MS);

    grim.on('exit', (code) => {
      clearTimeout(timeout);
      if (code === 0) {
        writer.once('close', () => resolve());
        reader.destroy();
        writer.end();
      } else {
        reject(new Error(`grim exited with code ${code}`));
      }
    });

    grim.on('error', (err) => {
      clearTimeout(timeout);
      reader.destroy();
      writer.end();
      reject(err);
    });
  });
}

async function main() {
  ensureBinaries();
  ensureFifo(PIPE_PATH);
  if (WAYLAND_COMPOSITOR === 'weston') {
    startWeston();
  } else if (WAYLAND_COMPOSITOR === 'sway') {
    await startSway();
  } else {
    throw new Error(
      `Unsupported compositor "${WAYLAND_COMPOSITOR}". Set WAYLAND_COMPOSITOR to either "sway" or "weston".`,
    );
  }

  process.on('SIGINT', shutdown);
  process.on('SIGTERM', shutdown);

  // Give the compositor a moment to be ready before grabbing a frame.
  await wait(500);

  console.log('Capturing frame from Wayland session...');
  await captureFrame();
  console.log(`Frame saved at ${OUTPUT_PATH}`);
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
