import { defineConfig } from "vitest/config";
import vue from "@vitejs/plugin-vue";
export default defineConfig({
  plugins: [vue()],
  server: {
    proxy: {
      "/static/img": {
        target: process.env.GOEPAY_API_URL || "http://127.0.0.1:8080",
        changeOrigin: false,
      },
      "/console": {
        target: process.env.GOEPAY_API_URL || "http://127.0.0.1:8080",
        changeOrigin: false,
      },
    },
  },
  build: {
    rollupOptions: {
      output: {
        // The framework changes less often than console code; keep it in its
        // own long-cached chunk next to the on-demand page chunks.
        manualChunks: { vendor: ["vue", "vue-router", "qrcode"] },
      },
    },
  },
  test: { environment: "jsdom", include: ["tests/**/*.test.ts"] },
});
