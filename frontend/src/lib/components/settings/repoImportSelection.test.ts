import { describe, expect, it } from "vitest";
import {
  applyRangeSelection,
  filterRows,
  parseImportPattern,
  rowKey,
  selectedRowsForSubmit,
  setAllVisible,
  sortRows,
  type RepoImportRow,
} from "./repoImportSelection.js";

const rows: RepoImportRow[] = [
  { owner: "acme", name: "worker", description: "Background jobs", private: false, fork: false, pushed_at: "2026-04-20T00:00:00Z", already_configured: false },
  { owner: "acme", name: "api", description: "HTTP API", private: true, fork: false, pushed_at: "2026-04-22T00:00:00Z", already_configured: false },
  { owner: "acme", name: "empty", description: null, private: false, fork: false, pushed_at: null, already_configured: false },
  { owner: "acme", name: "widget", description: "Configured", private: false, fork: true, pushed_at: "2026-04-21T00:00:00Z", already_configured: true },
];

describe("repo import selection helpers", () => {
  it("parses owner/pattern and trims whitespace", () => {
    expect(parseImportPattern(" acme / widget-* ")).toEqual({ owner: "acme", pattern: "widget-*" });
  });

  it("rejects malformed patterns before the API call", () => {
    expect(() => parseImportPattern("acme/widgets/extra")).toThrow("Format: owner/pattern");
    expect(() => parseImportPattern("acme*/widgets")).toThrow("glob syntax in owner is not supported");
    expect(() => parseImportPattern("acme/")).toThrow("pattern is required");
  });

  it("filters by owner/name, name, description, and status", () => {
    expect(filterRows(rows, "HTTP", "all").map((row) => row.name)).toEqual(["api"]);
    expect(filterRows(rows, "acme/worker", "all").map((row) => row.name)).toEqual(["worker"]);
    expect(filterRows(rows, "", "already-added").map((row) => row.name)).toEqual(["widget"]);
    const selected = new Set([rowKey(rows[0]!)]);
    expect(filterRows(rows, "", "selected", selected).map((row) => row.name)).toEqual(["worker"]);
    expect(filterRows(rows, "", "unselected", selected).map((row) => row.name)).toEqual(["api", "empty"]);
  });

  it("filters private repositories and forks independently", () => {
    expect(filterRows(rows, "", "all", new Set(), { hidePrivate: true }).map((row) => row.name)).toEqual(["worker", "empty", "widget"]);
    expect(filterRows(rows, "", "all", new Set(), { hideForks: true }).map((row) => row.name)).toEqual(["worker", "api", "empty"]);
    expect(filterRows(rows, "", "all", new Set(), { hidePrivate: true, hideForks: true }).map((row) => row.name)).toEqual(["worker", "empty"]);
  });

  it("sorts deterministically with null pushed_at last", () => {
    expect(sortRows(rows, { field: "pushed_at", direction: "desc" }).map((row) => row.name)).toEqual(["api", "widget", "worker", "empty"]);
    expect(sortRows(rows, { field: "pushed_at", direction: "asc" }).map((row) => row.name)).toEqual(["worker", "widget", "api", "empty"]);
    expect(sortRows(rows, { field: "name", direction: "asc" }).map((row) => row.name)).toEqual(["api", "empty", "widget", "worker"]);
  });

  it("selects and deselects all visible selectable rows", () => {
    const selected = setAllVisible(new Set<string>(), rows, true);
    expect([...selected].sort()).toEqual(["acme/api", "acme/empty", "acme/worker"]);
    expect([...setAllVisible(selected, [rows[0]!, rows[3]!], false)].sort()).toEqual(["acme/api", "acme/empty"]);
  });

  it("applies shift-click ranges with visible anchors", () => {
    const visible = sortRows(rows, { field: "name", direction: "asc" });
    const result = applyRangeSelection({
      selected: new Set<string>(),
      visibleRows: visible,
      anchorKey: "acme/api",
      clickedKey: "acme/worker",
      checked: true,
    });
    expect([...result.selected].sort()).toEqual(["acme/api", "acme/empty", "acme/worker"]);
    expect(result.anchorKey).toBe("acme/worker");
  });

  it("treats hidden anchors as normal clicks", () => {
    const result = applyRangeSelection({
      selected: new Set<string>(),
      visibleRows: [rows[1]!],
      anchorKey: "acme/worker",
      clickedKey: "acme/api",
      checked: true,
    });
    expect([...result.selected]).toEqual(["acme/api"]);
    expect(result.anchorKey).toBe("acme/api");
  });

  it("returns selected rows for submit in full sorted order", () => {
    const sorted = sortRows(rows, { field: "name", direction: "asc" });
    const selected = new Set(["acme/worker", "acme/api"]);
    expect(selectedRowsForSubmit(sorted, selected).map((row) => row.name)).toEqual(["api", "worker"]);
    expect(selectedRowsForSubmit(sorted, selected, { hidePrivate: true }).map((row) => row.name)).toEqual(["worker"]);
  });
});
