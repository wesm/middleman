import { describe, expect, it } from "vitest";

import { labelPickerPopoverStyle } from "./labelPickerPosition.js";

function rect(input: Pick<DOMRect, "right" | "bottom">): DOMRect {
  return {
    x: 0,
    y: 0,
    width: 0,
    height: 0,
    top: input.bottom,
    left: input.right,
    right: input.right,
    bottom: input.bottom,
    toJSON: () => ({}),
  };
}

describe("labelPickerPopoverStyle", () => {
  it("right-aligns to the trigger when there is room", () => {
    expect(labelPickerPopoverStyle(rect({ right: 900, bottom: 100 }), 1200)).toContain("left: 540px");
  });

  it("clamps to the viewport edge near the right side", () => {
    expect(labelPickerPopoverStyle(rect({ right: 1250, bottom: 100 }), 1200)).toContain("left: 828px");
  });

  it("uses the available viewport width when narrow", () => {
    const style = labelPickerPopoverStyle(rect({ right: 260, bottom: 100 }), 300);

    expect(style).toContain("left: 12px");
    expect(style).toContain("width: 276px");
  });
});
