import { Marked } from "marked";
import type { TokenizerAndRendererExtension } from "marked";
import DOMPurify from "dompurify";

interface RepoContext {
  owner: string;
  name: string;
}

function itemRefExtension(repo?: RepoContext): TokenizerAndRendererExtension {
  return {
    name: "itemRef",
    level: "inline",
    start(src: string): number | undefined {
      // Cross-repo: look for word chars before #
      const crossIdx = src.search(/[\w.-]+\/[\w.-]+#\d/);
      // Bare: look for # preceded by start or non-word
      const bareIdx = src.search(/(^|[^\w])#\d/);
      const adjusted = bareIdx >= 0 && src[bareIdx] !== "#"
        ? bareIdx + 1
        : bareIdx;
      if (crossIdx >= 0 && (adjusted < 0 || crossIdx <= adjusted)) {
        return crossIdx;
      }
      return adjusted >= 0 ? adjusted : undefined;
    },
    tokenizer(src: string): { type: string; raw: string; owner: string; name: string; number: number; text: string } | undefined {
      // Cross-repo: owner/name#123
      const crossMatch = src.match(
        /^([\w.-]+)\/([\w.-]+)#(\d+)/,
      );
      if (crossMatch) {
        return {
          type: "itemRef",
          raw: crossMatch[0],
          owner: crossMatch[1]!,
          name: crossMatch[2]!,
          number: parseInt(crossMatch[3]!, 10),
          text: crossMatch[0],
        };
      }
      // Bare ref: #123
      const bareMatch = src.match(/^#(\d+)/);
      if (bareMatch && repo) {
        return {
          type: "itemRef",
          raw: bareMatch[0],
          owner: repo.owner,
          name: repo.name,
          number: parseInt(bareMatch[1]!, 10),
          text: bareMatch[0],
        };
      }
      return undefined;
    },
    renderer(token): string {
      const t = token as unknown as { owner: string; name: string; number: number; text: string };
      const href = `https://github.com/${t.owner}/${t.name}/issues/${t.number}`;
      return `<a class="item-ref" href="${href}" data-owner="${t.owner}" data-name="${t.name}" data-number="${t.number}">${t.text}</a>`;
    },
  };
}

const cache = new Map<string, string>();

export function renderMarkdown(
  raw: string,
  repo?: RepoContext,
): string {
  if (!raw) return "";
  const key = repo ? `${repo.owner}/${repo.name}\0${raw}` : raw;
  const cached = cache.get(key);
  if (cached !== undefined) return cached;

  const marked = new Marked({
    breaks: true,
    gfm: true,
  });
  marked.use({ extensions: [itemRefExtension(repo)] });

  const html = DOMPurify.sanitize(marked.parse(raw) as string, {
    ADD_ATTR: ["target", "data-owner", "data-name", "data-number"],
  });
  if (cache.size > 500) cache.clear();
  cache.set(key, html);
  return html;
}
