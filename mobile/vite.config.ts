import legacy from '@vitejs/plugin-legacy';
import vue from '@vitejs/plugin-vue';
import path from 'path';
import { defineConfig } from 'vitest/config';

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [vue(), legacy()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  optimizeDeps: {
    // Imported dynamically, so Vite would otherwise re-optimize (and reload the
    // page) the first time they are needed.
    include: ['@capacitor/app', '@capacitor/barcode-scanner', '@capacitor/device', '@capacitor/preferences'],
  },
  test: {
    globals: true,
    environment: 'jsdom',
    // Playwright e2e specs live in tests/e2e.
    include: ['tests/unit/**/*.spec.{js,ts}'],
  },
});
