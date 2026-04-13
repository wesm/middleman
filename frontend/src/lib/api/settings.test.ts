import { beforeEach, describe, expect, it, vi } from "vitest";
import { removeRepo } from "./settings.js";

describe("settings api", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({
      ok: true,
      text: vi.fn(),
    }));
  });

  it("encodes repo names for delete requests", async () => {
    await removeRepo("acme", "widgets-?");

    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/repos/acme/widgets-%3F",
      { method: "DELETE", headers: { "Content-Type": "application/json" } },
    );
  });
});
