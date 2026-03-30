import { Marked } from "marked";
import DOMPurify from "dompurify";

const marked = new Marked({
  breaks: true,
  gfm: true,
});

const cache = new Map<string, string>();

export function renderMarkdown(raw: string): string {
  if (!raw) return "";
  const cached = cache.get(raw);
  if (cached !== undefined) return cached;
  const html = DOMPurify.sanitize(marked.parse(raw) as string, {
    ADD_ATTR: ["target"],
  });
  if (cache.size > 500) cache.clear();
  cache.set(raw, html);
  return html;
}
