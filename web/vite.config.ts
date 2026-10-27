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
  test: { environment: "jsdom", include: ["tests/**/*.test.ts"] },
});
