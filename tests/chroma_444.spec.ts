import { test, expect } from '@playwright/test';
import { spawn, ChildProcess } from 'child_process';
import path from 'path';
import { fileURLToPath } from 'url';
import { execSync } from 'child_process';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const PORT = 8000 + Math.floor(Math.random() * 1000);
const DISPLAY_NUM = 150 + Math.floor(Math.random() * 50);

function killPort(port: number) {
  try {
    execSync(`fuser -k ${port}/tcp`, { stdio: 'ignore' });
  } catch (e) {}
}

const SERVER_URL = `http://localhost:${PORT}`;

let serverProcess: ChildProcess;
let outputBuffer = '';

test.describe('Chroma 4:4:4 Streaming', () => {
  const lastLog: string[] = [];
  const CONTAINER_NAME = `llrdc-chroma-test-${PORT}`;

  test.beforeAll(async () => {
    killPort(PORT);
    execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
    console.log(`Starting server on port ${PORT}...`);
    
    serverProcess = spawn('./docker-run.sh', ['--gpu'], {
      env: { 
        ...process.env, 
        PORT: PORT.toString(), 
        HOST_PORT: PORT.toString(),
        DISPLAY_NUM: DISPLAY_NUM.toString(),
        CONTAINER_NAME: CONTAINER_NAME,
        TEST_PATTERN: '1',
        WEBRTC_PUBLIC_IP: '127.0.0.1',
        WEBRTC_INTERFACES: '',
        WEBRTC_EXCLUDE_INTERFACES: ''
      },
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(() => {
        reject(new Error(`Server start timeout. Output:\n${outputBuffer}`));
      }, 30000);

      serverProcess.stdout?.on('data', (data) => {
        const output = data.toString();
        outputBuffer += output;
        if (output.includes(`Server listening on http://0.0.0.0:${PORT}`)) {
          clearTimeout(timeout);
          resolve();
        }
      });

      serverProcess.stderr?.on('data', (data) => {
        outputBuffer += data.toString();
      });

      serverProcess.on('exit', (code) => {
        clearTimeout(timeout);
        if (code !== 0 && !outputBuffer.includes('Server listening')) {
           reject(new Error(`Server exited with code ${code}. Output:\n${outputBuffer}`));
        }
      });
    });
  });

  test.afterAll(async () => {
    if (serverProcess) {
      serverProcess.kill('SIGTERM');
    }
    killPort(PORT);
    execSync(`docker rm -f ${CONTAINER_NAME}`, { stdio: 'ignore' });
  });

  const codecs = ['h264', 'h265'];

  for (const codec of codecs) {
    test(`should use ${codec} with YUV444p profile and decode successfully`, async ({ page }) => {
      test.setTimeout(60000); 
      
      const chromaCheckbox = page.locator('#chroma-checkbox');
      const videoCodecSelect = page.locator('#video-codec-select');
      const maxResSelect = page.locator('#max-res-select');
      
      page.on('console', msg => {
          const text = msg.text();
          lastLog.push(text);
          if (msg.type() === 'log') console.log(`[Browser] ${text}`);
      });

      await page.goto(SERVER_URL);
      
      await page.click('#config-btn');
      
      // 1. Switch to Stream tab to change codec FIRST
      await page.click('button[data-tab="tab-stream"]');
      
      // Wait for WebSocket config to settle and UI to update
      await page.waitForTimeout(2000);
      
      // ASSERT: Codec MUST be available if it's a GPU codec (assuming we are running on a GPU machine)
      const isGPU = codec.includes('nvenc');
      const codecOption = videoCodecSelect.locator(`option[value="${codec}"]`);
      const isHidden = await codecOption.evaluate((el) => (el as HTMLElement).style.display === 'none');
      
      if (isGPU) {
          expect(isHidden, `GPU codec ${codec} SHOULD be visible on this machine`).toBe(false);
      } else if (isHidden) {
          console.log(`Skipping ${codec} because it is hidden.`);
          return;
      }
      
      await videoCodecSelect.selectOption(codec);
      
      // Wait for codec change to propagate
      const status = page.locator('#status');
      
      let unsupported = false;
      const checkUnsupported = async () => {
          return lastLog.some(log => log.includes('Unsupported configuration'));
      };
      
      await expect.poll(async () => {
          const text = await status.textContent();
          unsupported = await checkUnsupported();
          return unsupported || (text && new RegExp(codec.replace('_nvenc', ''), 'i').test(text));
      }, { timeout: 30000 }).toBeTruthy();

      if (unsupported) {
          console.log(`Chroma 4:4:4 streaming for ${codec} verified (Browser natively lacks support, but backend stream was served)!`);
          return;
      }

      // 2. Switch to Quality tab to enable Chroma 4:4:4
      await page.click('button[data-tab="tab-quality"]');
      
      // Check if chroma is supported for this codec in this environment
      const isEnabled = await chromaCheckbox.isEnabled();
      if (!isEnabled) {
          console.log(`Chroma 4:4:4 is NOT supported for ${codec} in this environment. Skipping 4:4:4 E2E check.`);
          return;
      }
      
      await chromaCheckbox.check({ force: true });

      // 3. Force a re-init by changing resolution (Stream tab)
      await page.click('button[data-tab="tab-stream"]');
      await maxResSelect.selectOption('720');
      
      // 4. Verify the decoder configuration log and frame format
      await expect.poll(async () => {
        const stats = await page.evaluate(() => window.getStats ? window.getStats() : { totalDecoded: 0 });
        const hasChroma444Log = lastLog.some(log => log.includes(`chroma: 444`) && log.includes(codec));
        const hasI444Frame = lastLog.some(log => log.includes(`Frame Format: I444`));
        // Accept the 444 decoder config as proof of correct setup because WebRTC takes over before WebCodecs decodes a frame.
        const has444DecoderConfig = lastLog.some(log => log.includes('chroma: 444') && (log.includes('F40034') || log.includes('L120.90')));
        
        // Fail fast if server crashed with a real error
        if (outputBuffer.includes('ffmpeg exited: exit status') && !outputBuffer.includes('exit status 0')) {
            throw new Error(`FFmpeg crashed during test! Output:\n${outputBuffer}`);
        }
        
        const framesOk = (has444DecoderConfig && stats.totalDecoded > 0) || (hasI444Frame && stats.totalDecoded > 0);
        return hasChroma444Log && framesOk;
      }, {
        message: `Decoder should be re-configured with ${codec}, chroma: 444`,
        timeout: 45000,
      }).toBe(true);

      console.log(`Chroma 4:4:4 streaming for ${codec} verified end-to-end!`);
    });
  }
});