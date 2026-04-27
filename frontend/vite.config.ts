import { createRequire } from "node:module";
import path from "node:path";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { svelteTesting } from "@testing-library/svelte/vite";
import { searchForWorkspaceRoot, type Plugin, type UserConfig } from "vite";
import type { InlineConfig } from "vitest/node";
import { resolveDevApiUrl } from "./src/lib/dev/apiProxyTarget";
import { healthcheckPlugin } from "./src/lib/dev/healthcheckPlugin";

const require = createRequire(import.meta.url);
const testingLibrarySvelteEntry = require.resolve("@testing-library/svelte");

const apiUrl = resolveDevApiUrl();
const devServerPort = resolveViteServerPort();
const workspaceRoot = searchForWorkspaceRoot(process.cwd());
const uiPkg = path.resolve(process.cwd(), "../packages/ui");
const uiIndex = path.resolve(process.cwd(), "../packages/ui/src/index.ts");
const uiGeneratedClient = path.resolve(process.cwd(), "../packages/ui/src/api/generated/client.ts");
const uiGeneratedSchema = path.resolve(process.cwd(), "../packages/ui/src/api/generated/schema.ts");
const uiApiTypes = path.resolve(process.cwd(), "../packages/ui/src/api/types.ts");
const uiApiCsrf = path.resolve(process.cwd(), "../packages/ui/src/api/csrf.ts");
const uiStoreDetail = path.resolve(process.cwd(), "../packages/ui/src/stores/detail.svelte.ts");
const uiStoreEvents = path.resolve(process.cwd(), "../packages/ui/src/stores/events.svelte.ts");
const uiStorePulls = path.resolve(process.cwd(), "../packages/ui/src/stores/pulls.svelte.ts");
const uiStoreIssues = path.resolve(process.cwd(), "../packages/ui/src/stores/issues.svelte.ts");
const uiStoreActivity = path.resolve(process.cwd(), "../packages/ui/src/stores/activity.svelte.ts");
const uiStoreSync = path.resolve(process.cwd(), "../packages/ui/src/stores/sync.svelte.ts");
const uiStoreDiff = path.resolve(process.cwd(), "../packages/ui/src/stores/diff.svelte.ts");
const uiStoreGrouping = path.resolve(process.cwd(), "../packages/ui/src/stores/grouping.svelte.ts");
const uiStoreSettings = path.resolve(process.cwd(), "../packages/ui/src/stores/settings.svelte.ts");

function devApiUrlPlugin(url: string): Plugin {
  return {
    name: "middleman-dev-api-url",
    apply: "serve",
    transformIndexHtml() {
      return [
        {
          tag: "script",
          children:
            `window.__MIDDLEMAN_DEV_API_URL__ = ${JSON.stringify(url)};`,
          injectTo: "head-prepend",
        },
      ];
    },
  };
}

export function resolveViteServerPort(
  argv: readonly string[] = process.argv,
): number {
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (!arg) continue;
    if (arg === "--port" && i+1 < argv.length) {
      const next = argv[i+1];
      const parsed = parsePort(next);
      if (parsed !== null) return parsed;
    }
    if (arg.startsWith("--port=")) {
      const parsed = parsePort(arg.slice("--port=".length));
      if (parsed !== null) return parsed;
    }
  }
  return 5174;
}

function parsePort(value: string | undefined): number | null {
  if (!value) return null;
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < 1 || parsed > 65535) {
    return null;
  }
  return parsed;
}

const config = {
  base: "/",
  plugins: [
    healthcheckPlugin(),
    devApiUrlPlugin(apiUrl),
    svelte(),
    svelteTesting(),
  ],
  resolve: {
    alias: [
      {
        find: /^@testing-library\/svelte$/,
        replacement: testingLibrarySvelteEntry,
      },
      {
        find: /^@middleman\/ui$/,
        replacement: uiIndex,
      },
      {
        find: /^@middleman\/ui\/api\/client$/,
        replacement: uiGeneratedClient,
      },
      {
        find: /^@middleman\/ui\/api\/schema$/,
        replacement: uiGeneratedSchema,
      },
      {
        find: /^@middleman\/ui\/api\/types$/,
        replacement: uiApiTypes,
      },
      {
        find: /^@middleman\/ui\/api\/csrf$/,
        replacement: uiApiCsrf,
      },
      {
        find: /^@middleman\/ui\/stores\/detail$/,
        replacement: uiStoreDetail,
      },
      {
        find: /^@middleman\/ui\/stores\/events$/,
        replacement: uiStoreEvents,
      },
      {
        find: /^@middleman\/ui\/stores\/pulls$/,
        replacement: uiStorePulls,
      },
      {
        find: /^@middleman\/ui\/stores\/issues$/,
        replacement: uiStoreIssues,
      },
      {
        find: /^@middleman\/ui\/stores\/activity$/,
        replacement: uiStoreActivity,
      },
      {
        find: /^@middleman\/ui\/stores\/sync$/,
        replacement: uiStoreSync,
      },
      {
        find: /^@middleman\/ui\/stores\/diff$/,
        replacement: uiStoreDiff,
      },
      {
        find: /^@middleman\/ui\/stores\/grouping$/,
        replacement: uiStoreGrouping,
      },
      {
        find: /^@middleman\/ui\/stores\/settings$/,
        replacement: uiStoreSettings,
      },
    ],
  },
  optimizeDeps: {
    exclude: ["@middleman/ui"],
  },
  server: {
    host: "127.0.0.1",
    port: devServerPort,
    strictPort: true,
    hmr: {
      protocol: "ws",
      host: "127.0.0.1",
      clientPort: devServerPort,
      path: "/__vite_hmr",
    },
    fs: { allow: [workspaceRoot, uiPkg] },
    proxy: {
      "/api": {
        target: apiUrl,
        changeOrigin: true,
        timeout: 0,
        proxyTimeout: 0,
      },
      "/ws": {
        target: apiUrl,
        changeOrigin: true,
        ws: true,
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
    chunkSizeWarningLimit: 1500,
  },
} satisfies UserConfig & { test: InlineConfig };

export default config;
