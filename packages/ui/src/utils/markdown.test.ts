import { describe, expect, it } from "vitest";
import { renderMarkdown } from "./markdown.js";

describe("renderMarkdown task lists", () => {
  it("renders disabled checkboxes by default", () => {
    const html = renderMarkdown("- [ ] one\n- [x] two");
    expect(html).toContain('disabled=""');
    expect(html).not.toContain("data-task-index");
  });

  it("renders enabled checkboxes with sequential indices when interactiveTasks is set", () => {
    const html = renderMarkdown(
      "- [ ] alpha\n- [x] beta\n- [ ] gamma",
      undefined,
      { interactiveTasks: true },
    );
    expect(html).not.toContain('disabled=""');
    expect(html).toContain('data-task-index="0"');
    expect(html).toContain('data-task-index="1"');
    expect(html).toContain('data-task-index="2"');
  });

  it("starts the task index at zero for every render", () => {
    const opts = { interactiveTasks: true } as const;
    const first = renderMarkdown("- [ ] a", undefined, opts);
    const second = renderMarkdown("- [ ] b", undefined, opts);
    expect(first).toContain('data-task-index="0"');
    expect(second).toContain('data-task-index="0"');
  });

  it("preserves checked state when interactiveTasks is set", () => {
    const html = renderMarkdown("- [x] done", undefined, {
      interactiveTasks: true,
    });
    expect(html).toContain('checked=""');
  });

  it("caches interactive and non-interactive renders separately", () => {
    const src = "- [ ] task";
    const plain = renderMarkdown(src);
    const interactive = renderMarkdown(src, undefined, {
      interactiveTasks: true,
    });
    expect(plain).toContain('disabled=""');
    expect(interactive).toContain('data-task-index="0"');
  });

  it("emits a drag handle and item-level data-task-index for interactive tasks", () => {
    const html = renderMarkdown("- [ ] a\n- [ ] b", undefined, {
      interactiveTasks: true,
    });
    expect(html).toContain(
      '<li class="task-list-item task-list-item--interactive" data-task-index="0">',
    );
    expect(html).toContain(
      '<span class="task-drag-handle" data-task-index="0"',
    );
    expect(html).toContain(
      '<span class="task-drag-handle" data-task-index="1"',
    );
    expect(html).toContain('draggable="true"');
  });

  it("does not emit drag handles in non-interactive mode", () => {
    const html = renderMarkdown("- [ ] a");
    expect(html).not.toContain("task-drag-handle");
    expect(html).not.toContain("draggable");
  });

  it("emits only one input per task item in interactive mode", () => {
    const html = renderMarkdown("- [ ] a", undefined, {
      interactiveTasks: true,
    });
    const matches = html.match(/<input/g) ?? [];
    expect(matches.length).toBe(1);
  });

  it("preserves per-listitem indices when task items are nested", () => {
    // Each <li> and its drag handle MUST carry the same index as the
    // checkbox that lives directly inside that <li>. A nested child
    // must not leak its index back up to its parent's wrapper.
    const html = renderMarkdown(
      "- [ ] outer\n  - [ ] inner\n- [x] sibling",
      undefined,
      { interactiveTasks: true },
    );
    // The outer <li> wraps both the outer checkbox AND the nested
    // list — its data-task-index must match its OWN checkbox (0),
    // not the nested child's (1).
    expect(html).toContain(
      '<li class="task-list-item task-list-item--interactive" data-task-index="0">',
    );
    expect(html).toContain(
      '<li class="task-list-item task-list-item--interactive" data-task-index="1">',
    );
    expect(html).toContain(
      '<li class="task-list-item task-list-item--interactive" data-task-index="2">',
    );
    expect(html).toContain(
      '<span class="task-drag-handle" data-task-index="0"',
    );
    expect(html).toContain(
      '<span class="task-drag-handle" data-task-index="1"',
    );
    expect(html).toContain(
      '<span class="task-drag-handle" data-task-index="2"',
    );
    // Sanity-check pairing: the outer <li> contains the nested <li>
    // in its inner content, and the outer's drag handle precedes
    // the outer's checkbox.
    const outerOpen = html.indexOf(
      'data-task-index="0"><span class="task-drag-handle" data-task-index="0"',
    );
    expect(outerOpen).toBeGreaterThanOrEqual(0);
  });
});
