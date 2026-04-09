import { defineConfig } from "vitest/config";
import { fileURLToPath, URL } from "node:url";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { svelteTesting } from "@testing-library/svelte/vite";

export default defineConfig({
  base: "/",
  plugins: [svelte(), svelteTesting()],
  resolve: {
    alias: {
      "@testing-library/svelte": fileURLToPath(
        new URL("./node_modules/@testing-library/svelte/src/index.js", import.meta.url),
      ),
    },
  },
  test: {
    environment: "jsdom",
    include: [
      "src/**/*.{test,spec}.?(c|m)[jt]s?(x)",
      "../packages/ui/src/**/*.{test,spec}.?(c|m)[jt]s?(x)",
    ],
    exclude: ["tests/e2e/**", "tests/e2e-full/**", "node_modules/**"],
  },
  server: {
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8090",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    chunkSizeWarningLimit: 1000,
  },
});
