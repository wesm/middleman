import { beforeEach, describe, expect, it, vi } from "vitest";

import { dispatchKeydown } from "./dispatch.svelte.js";
import {
  registerScopedActions,
  resetRegistry,
} from "./registry.svelte.js";
import { resetModalStack } from "@middleman/ui/stores/keyboard/modal-stack";
import type { Action, Context } from "./types.js";

const ctx: Context = {
  page: "pulls",
  route: { page: "pulls" } as never,
  selectedPR: null,
  selectedIssue: null,
  isDiffView: false,
  detailTab: "conversation",
};

const event = (init: Partial<KeyboardEvent>) =>
  Object.assign(new KeyboardEvent("keydown", init), {
    preventDefault: vi.fn(),
  });

describe("dispatchKeydown — global registry", () => {
  beforeEach(() => {
    resetRegistry();
    resetModalStack();
  });

  it("runs the matching action's handler and preventDefaults", () => {
    const handler = vi.fn();
    const a: Action = {
      id: "go.next",
      label: "Next",
      scope: "view-pulls",
      binding: { key: "j" },
      priority: 0,
      when: () => true,
      handler,
    };
    registerScopedActions("test", [a]);
    const e = event({ key: "j" });
    dispatchKeydown(e, () => ctx);
    expect(handler).toHaveBeenCalled();
    expect(e.preventDefault).toHaveBeenCalled();
  });

  it("does not run actions whose when returns false", () => {
    const handler = vi.fn();
    const a: Action = {
      id: "go.next",
      label: "Next",
      scope: "view-pulls",
      binding: { key: "j" },
      priority: 0,
      when: () => false,
      handler,
    };
    registerScopedActions("test", [a]);
    dispatchKeydown(event({ key: "j" }), () => ctx);
    expect(handler).not.toHaveBeenCalled();
  });
});
