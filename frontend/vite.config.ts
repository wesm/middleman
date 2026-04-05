import { defineConfig } from "vitest/config";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { svelteTesting } from "@testing-library/svelte/vite";

export default defineConfig({
  base: "/",
  plugins: [svelte(), svelteTesting()],
  test: {
    environment: "jsdom",
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
  },
});
