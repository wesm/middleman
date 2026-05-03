import { Marked } from "marked";
import type { TokenizerAndRendererExtension } from "marked";
import DOMPurify from "dompurify";

interface RepoContext {
  owner: string;
  name: string;
  platformHost?: string | undefined;
}

const itemRefTokenAttr = "data-middleman-item-ref-token";
let activeItemRefToken = "";

function trustedItemRefToken(): string {
  const bytes = new Uint8Array(16);
  globalThis.crypto.getRandomValues(bytes);
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
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
      const tokenAttr = activeItemRefToken
        ? ` ${itemRefTokenAttr}="${activeItemRefToken}"`
        : "";
      return `<a class="item-ref" href="${href}"${tokenAttr} data-owner="${t.owner}" data-name="${t.name}" data-number="${t.number}"${platformHostAttr}>${t.text}</a>`;
    },
  };
}

const htmlCache = new Map<string, string>();
const markedCache = new Map<string, Marked>();

function trustGeneratedItemRefs(html: string, token: string): string {
  if (typeof document === "undefined") {
    return html;
  }

  const template = document.createElement("template");
  template.innerHTML = html;
  for (const element of template.content.querySelectorAll(`[${itemRefTokenAttr}]`)) {
    if (element.getAttribute(itemRefTokenAttr) === token) {
      element.setAttribute("data-middleman-item-ref", "true");
    }
    element.removeAttribute(itemRefTokenAttr);
  }
  return template.innerHTML;
}

function getMarked(repo?: RepoContext): Marked {
  const key = repo ? `${repo.platformHost ?? ""}:${repo.owner}/${repo.name}` : "";
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
  const key = repo
    ? `${repo.platformHost ?? ""}:${repo.owner}/${repo.name}\0${raw}`
    : raw;
  const cached = htmlCache.get(key);
  if (cached !== undefined) return cached;

  const token = trustedItemRefToken();
  activeItemRefToken = token;
  let parsed: string;
  try {
    parsed = getMarked(repo).parse(raw) as string;
  } finally {
    activeItemRefToken = "";
  }

  const sanitized = DOMPurify.sanitize(
    parsed,
    {
      ADD_ATTR: [
        "target",
        "data-owner",
        "data-name",
        "data-number",
        "data-platform-host",
        itemRefTokenAttr,
      ],
      FORBID_ATTR: ["data-middleman-item-ref"],
    },
  );
  const html = trustGeneratedItemRefs(sanitized, token);
  if (htmlCache.size > 500) htmlCache.clear();
  htmlCache.set(key, html);
  return html;
}
