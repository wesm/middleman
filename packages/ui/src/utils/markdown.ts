import { Marked } from "marked";
import type { TokenizerAndRendererExtension } from "marked";
import DOMPurify from "dompurify";

interface RepoContext {
  owner: string;
  name: string;
  platformHost?: string | undefined;
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
    tokenizer(this: { lexer: { state: { inLink: boolean } } }, src: string): { type: string; raw: string; owner: string; name: string; number: number; text: string; platformHost?: string | undefined } | undefined {
      // Don't tokenize inside markdown link/image labels
      // to avoid producing invalid nested <a> elements.
      if (this.lexer.state.inLink) return undefined;

      // Cross-repo: owner/name#123 (with trailing word boundary)
      const crossMatch = src.match(
        /^([\w.-]+)\/([\w.-]+)#(\d+)(?!\w)/,
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
      // Bare ref: #123 (with trailing word boundary)
      const bareMatch = src.match(/^#(\d+)(?!\w)/);
      if (bareMatch && repo) {
        return {
          type: "itemRef",
          raw: bareMatch[0],
          owner: repo.owner,
          name: repo.name,
          number: parseInt(bareMatch[1]!, 10),
          ...(repo.platformHost ? { platformHost: repo.platformHost } : {}),
          text: bareMatch[0],
        };
      }
      return undefined;
    },
    renderer(token): string {
      const t = token as unknown as { owner: string; name: string; number: number; text: string; platformHost?: string | undefined };
      const href = `https://github.com/${t.owner}/${t.name}/issues/${t.number}`;
      const platformHostAttr = t.platformHost
        ? ` data-platform-host="${t.platformHost}"`
        : "";
      return `<a class="item-ref" href="${href}" data-owner="${t.owner}" data-name="${t.name}" data-number="${t.number}"${platformHostAttr}>${t.text}</a>`;
    },
  };
}

const htmlCache = new Map<string, string>();
const markedCache = new Map<string, Marked>();

function getMarked(repo?: RepoContext): Marked {
  const key = repo ? `${repo.owner}/${repo.name}` : "";
  let instance = markedCache.get(key);
  if (!instance) {
    instance = new Marked({ breaks: true, gfm: true });
    instance.use({ extensions: [itemRefExtension(repo)] });
    markedCache.set(key, instance);
  }
  return instance;
}

export function renderMarkdown(
  raw: string,
  repo?: RepoContext,
): string {
  if (!raw) return "";
  const key = repo ? `${repo.owner}/${repo.name}\0${raw}` : raw;
  const cached = htmlCache.get(key);
  if (cached !== undefined) return cached;

  const html = DOMPurify.sanitize(
    getMarked(repo).parse(raw) as string,
    {
      ADD_ATTR: [
        "target",
        "data-owner",
        "data-name",
        "data-number",
        "data-platform-host",
      ],
    },
  );
  if (htmlCache.size > 500) htmlCache.clear();
  htmlCache.set(key, html);
  return html;
}
