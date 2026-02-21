import { defineConfig } from 'vite';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

export default defineConfig({
    build: {
        outDir: 'public',
        emptyOutDir: true,
        rollupOptions: {
            input: {
                main: resolve(__dirname, 'viewer.html'),
            },
        },
    },
    server: {
        port: 5173,
        proxy: {
            // Proxy WebSocket to Go Backend during Dev
            '/ws': {
                target: 'ws://localhost:8080',
                ws: true,
            },
        }
    }
});
