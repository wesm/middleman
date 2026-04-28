import type { DiffFile } from "../../api/types.js";

export type DiffSummaryCategory = "plansDocs" | "code" | "tests" | "other";

export interface DiffLineTotals {
  additions: number;
  deletions: number;
}

export type DiffLineSummary = Record<DiffSummaryCategory | "total", DiffLineTotals>;

const ZERO_TOTALS: DiffLineTotals = { additions: 0, deletions: 0 };

const codeExtensions = new Set([
  ".bash",
  ".c",
  ".cc",
  ".cpp",
  ".cs",
  ".css",
  ".go",
  ".gql",
  ".graphql",
  ".h",
  ".hpp",
  ".html",
  ".java",
  ".js",
  ".jsx",
  ".kt",
  ".kts",
  ".less",
  ".php",
  ".py",
  ".rb",
  ".rs",
  ".sass",
  ".scss",
  ".sh",
  ".sql",
  ".svelte",
  ".swift",
  ".ts",
  ".tsx",
  ".vue",
  ".zsh",
]);

const docsExtensions = new Set([
  ".adoc",
  ".md",
  ".mdx",
  ".rst",
  ".txt",
]);

function pathParts(path: string): string[] {
  return path.toLowerCase().split(/[\\/]+/).filter(Boolean);
}

function basename(path: string): string {
  const parts = pathParts(path);
  return parts[parts.length - 1] ?? "";
}

function extension(path: string): string {
  const base = basename(path);
  const dot = base.lastIndexOf(".");
  return dot >= 0 ? base.slice(dot) : "";
}

function hasTestSignal(parts: string[], base: string): boolean {
  return (
    parts.some((part) =>
      part === "test" ||
      part === "tests" ||
      part === "__tests__" ||
      part === "e2e" ||
      part === "spec"
    ) ||
    base.includes(".test.") ||
    base.includes(".spec.") ||
    base.endsWith("_test.go") ||
    base.endsWith("_test.py") ||
    base.startsWith("test_") ||
    base.endsWith(".snap")
  );
}

function hasDocsSignal(parts: string[], base: string, ext: string): boolean {
  return (
    parts.some((part) =>
      part === "doc" ||
      part === "docs" ||
      part === "documentation" ||
      part === "plan" ||
      part === "plans" ||
      part === "context"
    ) ||
    docsExtensions.has(ext) ||
    [
      "changelog",
      "code_of_conduct",
      "contributing",
      "license",
      "notice",
      "readme",
      "security",
    ].some((name) => base === name || base.startsWith(`${name}.`))
  );
}

export function categorizeDiffFile(path: string): DiffSummaryCategory {
  const parts = pathParts(path);
  const base = basename(path);
  const ext = extension(path);

  if (hasTestSignal(parts, base)) return "tests";
  if (hasDocsSignal(parts, base, ext)) return "plansDocs";
  if (codeExtensions.has(ext)) return "code";
  return "other";
}

function emptySummary(): DiffLineSummary {
  return {
    plansDocs: { ...ZERO_TOTALS },
    code: { ...ZERO_TOTALS },
    tests: { ...ZERO_TOTALS },
    other: { ...ZERO_TOTALS },
    total: { ...ZERO_TOTALS },
  };
}

export function summarizeDiffFiles(files: DiffFile[]): DiffLineSummary {
  const summary = emptySummary();
  for (const file of files) {
    const category = categorizeDiffFile(file.path);
    summary[category].additions += file.additions;
    summary[category].deletions += file.deletions;
    summary.total.additions += file.additions;
    summary.total.deletions += file.deletions;
  }
  return summary;
}
