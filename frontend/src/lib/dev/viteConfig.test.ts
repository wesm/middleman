import path from "node:path";
import { describe, expect, it } from "vitest";
import config, {
  resolveViteServerPort,
} from "../../../vite.config";

describe("vite config", () => {
  it("aliases @middleman/ui to the workspace source tree", () => {
    const aliases = Array.isArray(config.resolve?.alias) ? config.resolve.alias : [];

    const uiRootAlias = aliases.find(
      (alias) => alias.find instanceof RegExp && alias.find.source === "^@middleman\\/ui$",
    );
    const uiSubpathAlias = aliases.find(
      (alias) => alias.find instanceof RegExp && alias.find.source === "^@middleman\\/ui\\/api\\/client$",
    );

    expect(uiRootAlias?.replacement).toBe(
      path.resolve(process.cwd(), "../packages/ui/src/index.ts"),
    );
    expect(uiSubpathAlias?.replacement).toBe(
      path.resolve(process.cwd(), "../packages/ui/src/api/generated/client.ts"),
    );
  });

  it("pins the dev server host to IPv4 loopback", () => {
    expect(config.server?.host).toBe("127.0.0.1");
    expect(config.server?.port).toBe(5174);
    expect(config.server?.strictPort).toBe(true);
    expect(config.server?.hmr).toEqual({
      protocol: "ws",
      host: "127.0.0.1",
      clientPort: 5174,
      path: "/__vite_hmr",
    });
  });

  it("keeps API proxy connections open for SSE streams", () => {
    const proxy = config.server?.proxy;
    expect(proxy).toBeDefined();
    expect(typeof proxy).toBe("object");
    if (!proxy || typeof proxy !== "object" || Array.isArray(proxy)) {
      throw new Error("expected object proxy config");
    }

    const apiProxy = proxy["/api"];
    expect(apiProxy).toMatchObject({
      changeOrigin: true,
      timeout: 0,
      proxyTimeout: 0,
    });
    expect(apiProxy).not.toMatchObject({ ws: true });
  });

  it("proxies terminal websocket upgrades under /ws only", () => {
    const proxy = config.server?.proxy;
    expect(proxy).toBeDefined();
    expect(typeof proxy).toBe("object");
    if (!proxy || typeof proxy !== "object" || Array.isArray(proxy)) {
      throw new Error("expected object proxy config");
    }

    expect(proxy["/ws"]).toMatchObject({
      changeOrigin: true,
      ws: true,
    });
  });

  it("uses the Vite CLI port for dev server HMR settings", () => {
    expect(resolveViteServerPort(["vite", "--port", "4173"])).toBe(4173);
    expect(resolveViteServerPort(["vite", "--port=4180"])).toBe(4180);
    expect(resolveViteServerPort(["vite"])).toBe(5174);
  });
});
