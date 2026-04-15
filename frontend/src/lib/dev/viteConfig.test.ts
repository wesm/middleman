import path from "node:path";
import { describe, expect, it } from "vitest";
import config from "../../../vite.config";

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
  });
});
