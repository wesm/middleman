import { createHighlighter, type Highlighter, type BundledLanguage } from "shiki";

let highlighterPromise: Promise<Highlighter> | null = null;

const LANGS = [
  "go", "typescript", "javascript", "tsx", "jsx", "python", "rust", "json",
  "yaml", "markdown", "sql", "shellscript", "css", "html", "toml",
  "dockerfile", "makefile", "svelte",
];

function getHighlighter(): Promise<Highlighter> {
  if (!highlighterPromise) {
    highlighterPromise = createHighlighter({
      themes: ["github-dark", "github-light"],
      langs: LANGS,
    });
  }
  return highlighterPromise;
}

const EXT_TO_LANG: Record<string, string> = {
  go: "go", ts: "typescript", tsx: "tsx", js: "javascript",
  jsx: "jsx", py: "python", rs: "rust", json: "json",
  yaml: "yaml", yml: "yaml", md: "markdown", sql: "sql",
  sh: "shellscript", bash: "shellscript", css: "css", html: "html",
  toml: "toml", mk: "makefile", svelte: "svelte",
};

const BASENAME_TO_LANG: Record<string, string> = {
  Dockerfile: "dockerfile",
  Makefile: "makefile",
};

export function langFromPath(path: string): string | undefined {
  const base = path.split("/").pop() ?? "";
  if (BASENAME_TO_LANG[base]) return BASENAME_TO_LANG[base];
  const ext = base.split(".").pop() ?? "";
  return EXT_TO_LANG[ext];
}

export interface DualToken {
  content: string;
  darkColor?: string;
  lightColor?: string;
}

// Tokenize a single line for both light and dark themes in one grammar pass.
// Uses Shiki's native dual-theme support so token boundaries are guaranteed
// aligned across themes — zipping by index would be unsafe otherwise.
export async function tokenizeLineDual(
  code: string,
  lang: string | undefined,
): Promise<DualToken[]> {
  if (!lang) {
    return [{ content: code }];
  }
  try {
    const hl = await getHighlighter();
    const lines = hl.codeToTokensWithThemes(code, {
      lang: lang as BundledLanguage,
      themes: { dark: "github-dark", light: "github-light" },
    });
    if (lines.length === 0) return [{ content: code }];
    const line = lines[0];
    if (!line) return [{ content: code }];
    return line.map((t) => {
      const darkColor = t.variants.dark?.color;
      const lightColor = t.variants.light?.color;
      return {
        content: t.content,
        ...(darkColor != null ? { darkColor } : {}),
        ...(lightColor != null ? { lightColor } : {}),
      };
    });
  } catch {
    return [{ content: code }];
  }
}

