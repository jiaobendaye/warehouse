import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { resolve } from 'path';

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:17880',
      '/mcp': 'http://127.0.0.1:17880',
      '/healthz': 'http://127.0.0.1:17880',
    },
  },
  build: {
    outDir: resolve(__dirname, 'dist'),
  },
});