import { spawn } from 'child_process';
import { EventEmitter } from 'events';
import path from 'node:path'; // Using TS ES module syntax for import if module:commonjs is not set, but tsx handles it. 
// Actually, let's use standard requires if we are unsure, but user environment seems to support ESM or tsx.
// The previous error was "require is not defined", so we stick to imports or create a simpler script.
// Let's use ESM imports as requested.
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';

// --- Improved NAL Splitter (Copy from server.ts) ---
class NALSplitter {
    private buffer: Buffer;

    constructor(private onNAL: (nal: Buffer) => void) {
        this.buffer = Buffer.alloc(0);
    }

    feed(data: Buffer) {
        this.buffer = Buffer.concat([this.buffer, data]);

        while (true) {
            let startCodeLen = 0;
            if (this.buffer.length >= 4 && this.buffer[0] === 0 && this.buffer[1] === 0 && this.buffer[2] === 0 && this.buffer[3] === 1) {
                startCodeLen = 4;
            } else if (this.buffer.length >= 3 && this.buffer[0] === 0 && this.buffer[1] === 0 && this.buffer[2] === 1) {
                startCodeLen = 3;
            } else {
                const idx = this.buffer.indexOf(Buffer.from([0, 0, 1]));
                if (idx === -1) {
                    if (this.buffer.length > 3) {
                        this.buffer = this.buffer.subarray(this.buffer.length - 3);
                    }
                    break;
                }

                let realStart = idx;
                if (idx > 0 && this.buffer[idx - 1] === 0) {
                    realStart = idx - 1;
                }
                this.buffer = this.buffer.subarray(realStart);
                continue;
            }

            const nextStart = this.buffer.indexOf(Buffer.from([0, 0, 1]), startCodeLen);

            if (nextStart === -1) {
                break;
            }

            let splitPoint = nextStart;
            if (nextStart > 0 && this.buffer[nextStart - 1] === 0) {
                splitPoint = nextStart - 1;
            }

            const nalUnit = this.buffer.subarray(0, splitPoint);
            this.onNAL(nalUnit);

            this.buffer = this.buffer.subarray(splitPoint);
        }
    }
}

// --- Diagnostic Config ---
const DISPLAY = process.env.DISPLAY || ':99';
const FPS = 30;

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const PROJECT_ROOT = path.resolve(__dirname, '..');
const FFMPEG_PATH = path.join(PROJECT_ROOT, 'bin/ffmpeg');

if (!fs.existsSync(FFMPEG_PATH)) {
    console.error(`FFMPEG not found at ${FFMPEG_PATH}`);
    process.exit(1);
}

console.log(`Diagnosing stream from ${DISPLAY} at ${FPS} FPS...`);

// Same args as server.ts
const inputArgs = [
    '-f', 'x11grab',
    '-draw_mouse', '1',
    '-framerate', `${FPS}`,
    '-s', '1280x720',
    '-i', `${DISPLAY}`,
];

const outputArgs = [
    '-vf', `scale=1280:720,fps=${FPS}`,
    '-r', `${FPS}`,
    '-fps_mode', 'cfr',
    '-c:v', 'libx264',
    '-pix_fmt', 'yuv420p',
    '-profile:v', 'baseline',
    '-level', '3.1',
    '-bf', '0',
    '-preset', 'veryfast',
    '-tune', 'zerolatency',
    '-b:v', '10000k',
    '-maxrate', '10000k',
    '-bufsize', '20000k',
    '-g', '1',
    '-keyint_min', '1',
    '-x264-params', 'rc-lookahead=0:sync-lookahead=0:scenecut=0:slices=1',
    '-f', 'h264',
    '-'
];

const ffmpegArgs = [
    '-probesize', '32',
    '-analyzeduration', '0',
    '-fflags', 'nobuffer',
    ...inputArgs,
    ...outputArgs
];

// --- Xvfb Setup ---
console.log('Starting Xvfb...');
const xvfb = spawn('Xvfb', [DISPLAY, '-screen', '0', '1280x720x24']);
await new Promise(r => setTimeout(r, 1000)); // Wait for Xvfb

const ffmpegProcess = spawn(FFMPEG_PATH, ffmpegArgs, { env: process.env });

let nalCount = 0;
let spsCount = 0;
let ppsCount = 0;
let idrCount = 0;
let sliceCount = 0;
let totalBytes = 0;
let startTime = Date.now();

const splitter = new NALSplitter((nal) => {
    nalCount++;
    totalBytes += nal.length;

    // Check NAL Header
    // Skip start code if present in `nal` (NALSplitter logic says extracted NAL includes start code)
    // 00 00 01 or 00 00 00 01
    let ptr = 0;
    while (ptr < nal.length - 1 && nal[ptr] === 0) ptr++;
    if (ptr < nal.length && nal[ptr] === 1) ptr++;

    if (ptr < nal.length) {
        const forbidden_zero_bit = (nal[ptr] & 0x80) >> 7;
        const nal_ref_idc = (nal[ptr] & 0x60) >> 5;
        const nal_unit_type = nal[ptr] & 0x1F;

        if (forbidden_zero_bit !== 0) {
            console.error(`[ERROR] Forbidden bit set! NAL #${nalCount}`);
        }

        let typeStr = 'UNKNOWN';
        if (nal_unit_type === 7) { typeStr = 'SPS'; spsCount++; }
        else if (nal_unit_type === 8) { typeStr = 'PPS'; ppsCount++; }
        else if (nal_unit_type === 5) { typeStr = 'IDR_SLICE'; idrCount++; }
        else if (nal_unit_type === 1) { typeStr = 'SLICE'; sliceCount++; }
        else if (nal_unit_type === 6) { typeStr = 'SEI'; }

        console.log(`NAL #${nalCount}: Type=${nal_unit_type} (${typeStr}), Size=${nal.length}, RefId=${nal_ref_idc}`);
    }

    if (nalCount >= 100) {
        ffmpegProcess.kill();
    }
});

ffmpegProcess.stdout.on('data', (data) => {
    splitter.feed(data);
});

ffmpegProcess.stderr.on('data', (data) => {
    console.log(`ffmpeg stderr: ${data}`);
});

ffmpegProcess.on('exit', (code) => {
    xvfb.kill();
    const duration = (Date.now() - startTime) / 1000;
    console.log(`\n--- Diagnosis Report ---`);
    console.log(`Duration: ${duration.toFixed(2)}s`);
    console.log(`Total NALs: ${nalCount}`);
    console.log(`SPS: ${spsCount}, PPS: ${ppsCount}`);
    console.log(`IDR: ${idrCount}, Slice: ${sliceCount}`);

    if (nalCount > 0) {
        console.log(`Avg NAL Size: ${(totalBytes / nalCount).toFixed(0)} bytes`);
    }

    if (spsCount === 0) console.error(`FAIL: No SPS detected.`);
    if (sliceCount > 0) console.error(`FAIL: P-Slices detected with -g 1`);
});
