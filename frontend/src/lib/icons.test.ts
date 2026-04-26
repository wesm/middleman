import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import * as icons from "./icons.ts";

const approvedIconNames = [
  "AlertIcon",
  "ChevronDownIcon",
  "MergeConflictIcon",
  "MoonIcon",
  "RefreshIcon",
  "SearchIcon",
  "SettingsIcon",
  "SidebarToggleIcon",
  "SpinnerIcon",
  "SunIcon",
  "SyncIcon",
] as const;

describe("icons barrel", () => {
  afterEach(() => {
    cleanup();
  });

  it("exports the approved app icon set", () => {
    expect(Object.keys(icons).sort()).toEqual([...approvedIconNames].sort());
  });

  it("renders each approved icon as an svg", () => {
    for (const name of approvedIconNames) {
      const IconComponent = icons[name];
      const { container, unmount } = render(IconComponent, {
        props: {
          size: "16",
          "aria-hidden": "true",
        },
      });

      expect(container.querySelector("svg"), `${name} should render an svg`).toBeTruthy();
      unmount();
    }
  });
});
