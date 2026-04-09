import { createRequire } from "node:module";
import { defineConfig } from "vitest/config";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { svelteTesting } from "@testing-library/svelte/vite";

const require = createRequire(import.meta.url);
const testingLibrarySvelteEntry = require.resolve("@testing-library/svelte");

export default defineConfig({
  base: "/",
  plugins: [svelte(), svelteTesting()],
  resolve: {
    alias: [
      {
        find: /^@testing-library\/svelte$/,
        replacement: testingLibrarySvelteEntry,
      },
    ],
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
