import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { svelteTesting } from "@testing-library/svelte/vite";
import type { UserConfig } from "vite";
import type { InlineConfig } from "vitest/node";
import { healthcheckPlugin } from "./src/lib/dev/healthcheckPlugin";

const require = createRequire(import.meta.url);
const testingLibrarySvelteEntry = require.resolve("@testing-library/svelte");

const apiUrl = process.env.MIDDLEMAN_API_URL ?? "http://127.0.0.1:8090";
const uiPkg = fileURLToPath(new URL("../packages/ui", import.meta.url));

const config = {
  base: "/",
  plugins: [healthcheckPlugin(), svelte(), svelteTesting()],
  resolve: {
    alias: [
      {
        find: /^@testing-library\/svelte$/,
        replacement: testingLibrarySvelteEntry,
      },
    ],
  },
  optimizeDeps: {
    exclude: ["@middleman/ui"],
  },
  server: {
    fs: { allow: [".", uiPkg] },
    watch: { ignored: [`!${uiPkg}/**`, "!**/node_modules/@middleman/ui/**"] },
    proxy: {
      "/api": {
        target: apiUrl,
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: [
      "src/**/*.{test,spec}.?(c|m)[jt]s?(x)",
      "../packages/ui/src/**/*.{test,spec}.?(c|m)[jt]s?(x)",
    ],
    exclude: ["tests/e2e/**", "tests/e2e-full/**", "node_modules/**"],
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    chunkSizeWarningLimit: 1000,
  },
} satisfies UserConfig & { test: InlineConfig };

export default config;
