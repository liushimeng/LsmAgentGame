import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';

// Vite config. The dev server proxies /api → HTTPS backend on 39001 and
// /ws → WSS on 39002. `secure: false` accepts the self-signed dev cert.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 39003,
    https: false, // the proxy upgrades to https on the backend
    proxy: {
      '/api': {
        target: 'https://127.0.0.1:39001',
        changeOrigin: true,
        secure: false,
      },
      '/ws': {
        target: 'wss://127.0.0.1:39002',
        ws: true,
        changeOrigin: true,
        secure: false,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
  },
});
