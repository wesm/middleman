import { describe, expect, it } from "vitest";
import {
  listTaskItems,
  moveTaskListItem,
  toggleTaskListItem,
} from "./task-list.js";

describe("listTaskItems", () => {
  it("returns an empty list for empty input", () => {
    expect(listTaskItems("")).toEqual([]);
  });

  it("returns an empty list when there are no tasks", () => {
    expect(listTaskItems("plain prose\nno tasks here")).toEqual([]);
  });

  it("captures index, checked state, and line number in document order", () => {
    const src = [
      "intro line",
      "- [ ] first",
      "  text",
      "- [x] second",
      "* [X] third (uppercase)",
      "1. [ ] fourth (ordered)",
    ].join("\n");
    expect(listTaskItems(src)).toEqual([
      { index: 0, checked: false, line: 1 },
      { index: 1, checked: true, line: 3 },
      { index: 2, checked: true, line: 4 },
      { index: 3, checked: false, line: 5 },
    ]);
  });

  it("ignores task-shaped lines inside fenced code blocks", () => {
    const src = [
      "- [ ] real one",
      "```",
      "- [ ] not a task",
      "- [x] also fenced",
      "```",
      "- [x] real two",
    ].join("\n");
    expect(listTaskItems(src)).toEqual([
      { index: 0, checked: false, line: 0 },
      { index: 1, checked: true, line: 5 },
    ]);
  });

  it("handles indented (nested) task list items", () => {
    const src = "- [ ] outer\n  - [x] inner";
    expect(listTaskItems(src)).toEqual([
      { index: 0, checked: false, line: 0 },
      { index: 1, checked: true, line: 1 },
    ]);
  });
});

describe("toggleTaskListItem", () => {
  it("flips an unchecked box to checked", () => {
    expect(toggleTaskListItem("- [ ] a", 0)).toBe("- [x] a");
  });

  it("flips a checked box to unchecked", () => {
    expect(toggleTaskListItem("- [x] a", 0)).toBe("- [ ] a");
  });

  it("normalizes uppercase X to space when unchecking", () => {
    expect(toggleTaskListItem("- [X] a", 0)).toBe("- [ ] a");
  });

  it("returns source unchanged when the index is out of range", () => {
    expect(toggleTaskListItem("- [ ] a", 5)).toBe("- [ ] a");
  });

  it("returns source unchanged when the index is negative", () => {
    expect(toggleTaskListItem("- [ ] a", -1)).toBe("- [ ] a");
  });

  it("toggles only the targeted item and leaves others intact", () => {
    const src = "- [ ] first\n- [ ] second\n- [x] third";
    expect(toggleTaskListItem(src, 1)).toBe(
      "- [ ] first\n- [x] second\n- [x] third",
    );
  });

  it("preserves line content after the checkbox", () => {
    const src = "- [ ] item with **bold** and `code`";
    expect(toggleTaskListItem(src, 0)).toBe(
      "- [x] item with **bold** and `code`",
    );
  });

  it("ignores task-shaped lines inside fenced code blocks when counting", () => {
    const src = [
      "- [ ] outer one",
      "```",
      "- [ ] fenced",
      "```",
      "- [ ] outer two",
    ].join("\n");
    const out = toggleTaskListItem(src, 1);
    // index 1 is "outer two", not the fenced line
    expect(out).toBe(
      [
        "- [ ] outer one",
        "```",
        "- [ ] fenced",
        "```",
        "- [x] outer two",
      ].join("\n"),
    );
  });

  it("supports ordered-list task markers", () => {
    expect(toggleTaskListItem("1. [ ] step one", 0)).toBe(
      "1. [x] step one",
    );
  });

  it("supports nested task items by document order", () => {
    const src = "- [ ] outer\n  - [ ] inner";
    expect(toggleTaskListItem(src, 1)).toBe(
      "- [ ] outer\n  - [x] inner",
    );
  });
});

describe("moveTaskListItem", () => {
  it("moves an item downward to a later position", () => {
    const src = "- [ ] A\n- [ ] B\n- [ ] C";
    expect(moveTaskListItem(src, 0, 2)).toBe(
      "- [ ] B\n- [ ] C\n- [ ] A",
    );
  });

  it("moves an item upward to an earlier position", () => {
    const src = "- [ ] A\n- [ ] B\n- [ ] C";
    expect(moveTaskListItem(src, 2, 0)).toBe(
      "- [ ] C\n- [ ] A\n- [ ] B",
    );
  });

  it("swaps two adjacent items", () => {
    const src = "- [ ] A\n- [ ] B\n- [ ] C";
    expect(moveTaskListItem(src, 0, 1)).toBe(
      "- [ ] B\n- [ ] A\n- [ ] C",
    );
    expect(moveTaskListItem(src, 1, 0)).toBe(
      "- [ ] B\n- [ ] A\n- [ ] C",
    );
  });

  it("returns source unchanged when from === to", () => {
    const src = "- [ ] A\n- [ ] B";
    expect(moveTaskListItem(src, 0, 0)).toBe(src);
  });

  it("returns source unchanged when an index is out of range", () => {
    const src = "- [ ] A\n- [ ] B";
    expect(moveTaskListItem(src, 0, 5)).toBe(src);
    expect(moveTaskListItem(src, 5, 0)).toBe(src);
    expect(moveTaskListItem(src, -1, 0)).toBe(src);
  });

  it("preserves checked state of the moved item", () => {
    const src = "- [ ] A\n- [x] B\n- [ ] C";
    expect(moveTaskListItem(src, 1, 0)).toBe(
      "- [x] B\n- [ ] A\n- [ ] C",
    );
  });

  it("preserves non-task content between task items", () => {
    const src = "- [ ] A\nsome prose\n- [ ] B\n- [ ] C";
    expect(moveTaskListItem(src, 0, 2)).toBe(
      "some prose\n- [ ] B\n- [ ] C\n- [ ] A",
    );
  });

  it("skips fenced task-shaped lines when counting", () => {
    const src = [
      "- [ ] real one",
      "```",
      "- [ ] fenced",
      "```",
      "- [ ] real two",
      "- [ ] real three",
    ].join("\n");
    expect(moveTaskListItem(src, 0, 2)).toBe(
      [
        "```",
        "- [ ] fenced",
        "```",
        "- [ ] real two",
        "- [ ] real three",
        "- [ ] real one",
      ].join("\n"),
    );
  });

  it("carries continuation lines along with the moved task", () => {
    const src = [
      "- [ ] first",
      "- [ ] second",
      "  continued text",
      "  more continuation",
      "- [ ] third",
    ].join("\n");
    expect(moveTaskListItem(src, 1, 2)).toBe(
      [
        "- [ ] first",
        "- [ ] third",
        "- [ ] second",
        "  continued text",
        "  more continuation",
      ].join("\n"),
    );
  });

  it("carries nested sub-task children along with the moved task", () => {
    const src = [
      "- [ ] outer first",
      "  - [ ] inner a",
      "  - [ ] inner b",
      "- [ ] outer second",
    ].join("\n");
    // Move outer-first (index 0) to outer-second's slot (index 3).
    expect(moveTaskListItem(src, 0, 3)).toBe(
      [
        "- [ ] outer second",
        "- [ ] outer first",
        "  - [ ] inner a",
        "  - [ ] inner b",
      ].join("\n"),
    );
  });

  it("returns source unchanged when dragging a task onto its own nested child", () => {
    const src = "- [ ] outer\n  - [ ] inner\n- [ ] sibling";
    // index 0 is outer, index 1 is inner. inner sits inside outer's
    // block, so moving outer to land on inner is a no-op.
    expect(moveTaskListItem(src, 0, 1)).toBe(src);
  });

  it("preserves blank lines and prose outside the moved block", () => {
    const src = [
      "Intro line",
      "",
      "- [ ] A",
      "- [ ] B",
      "  with continuation",
      "",
      "Trailing prose",
    ].join("\n");
    // Move A (index 0) to B's slot (index 1). B carries its
    // continuation; A becomes a single-line block.
    expect(moveTaskListItem(src, 0, 1)).toBe(
      [
        "Intro line",
        "",
        "- [ ] B",
        "  with continuation",
        "- [ ] A",
        "",
        "Trailing prose",
      ].join("\n"),
    );
  });
});
