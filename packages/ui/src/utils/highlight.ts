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

export interface TokenSpan {
  content: string;
  color?: string;
}

export async function tokenizeLine(
  code: string,
  lang: string | undefined,
  theme: "github-dark" | "github-light",
): Promise<TokenSpan[]> {
  if (!lang) {
    return [{ content: code }];
  }
  try {
    const hl = await getHighlighter();
    const tokens = hl.codeToTokensBase(code, { lang: lang as BundledLanguage, theme });
    if (tokens.length === 0) return [{ content: code }];
    const line = tokens[0];
    if (!line) return [{ content: code }];
    return line.map((t) => ({ content: t.content, ...(t.color != null ? { color: t.color } : {}) }));
  } catch {
    return [{ content: code }];
  }
}

// Shared reactive theme state. Initialized from the DOM and kept in sync
// via a single MutationObserver (avoids one observer per DiffFile).
let themeObserver: MutationObserver | null = null;
const themeListeners: Set<(dark: boolean) => void> = new Set();

function ensureThemeObserver(): void {
  if (themeObserver) return;
  themeObserver = new MutationObserver(() => {
    const dark = document.documentElement.classList.contains("dark");
    for (const fn of themeListeners) fn(dark);
  });
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["class"],
  });
}

export function subscribeTheme(callback: (dark: boolean) => void): () => void {
  ensureThemeObserver();
  themeListeners.add(callback);
  return () => {
    themeListeners.delete(callback);
    if (themeListeners.size === 0 && themeObserver) {
      themeObserver.disconnect();
      themeObserver = null;
    }
  };
}

export function isDarkTheme(): boolean {
  return document.documentElement.classList.contains("dark");
}
