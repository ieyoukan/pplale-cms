import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The Go server owns the session cookie, so the dev server proxies every API
// and auth route to it rather than running a second origin.
const backend = process.env.CMS_BACKEND ?? 'http://localhost:8080';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: backend, changeOrigin: false },
      '/auth': { target: backend, changeOrigin: false },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test-setup.ts'],
  },
});
