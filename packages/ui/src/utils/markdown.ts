import { Marked } from "marked";
import type { TokenizerAndRendererExtension } from "marked";
import DOMPurify from "dompurify";

interface RepoContext {
  provider: string;
  platformHost?: string | undefined;
  owner: string;
  name: string;
  repoPath: string;
}

type ItemRefToken = {
  type: string;
  raw: string;
  provider: string;
  platformHost?: string | undefined;
  owner: string;
  name: string;
  repoPath: string;
  number: number;
  text: string;
};

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
    tokenizer(this: { lexer: { state: { inLink: boolean } } }, src: string): ItemRefToken | undefined {
      if (this.lexer.state.inLink || !repo) return undefined;

      const crossMatch = src.match(
        /^([\w.-]+)\/([\w.-]+)#(\d+)(?!\w)/,
      );
      if (crossMatch) {
        const owner = crossMatch[1]!;
        const name = crossMatch[2]!;
        return {
          type: "itemRef",
          raw: crossMatch[0],
          provider: repo.provider,
          platformHost: repo.platformHost,
          owner,
          name,
          repoPath: `${owner}/${name}`,
          number: parseInt(crossMatch[3]!, 10),
          text: crossMatch[0],
        };
      }

      const bareMatch = src.match(/^#(\d+)(?!\w)/);
      if (bareMatch) {
        return {
          type: "itemRef",
          raw: bareMatch[0],
          provider: repo.provider,
          platformHost: repo.platformHost,
          owner: repo.owner,
          name: repo.name,
          repoPath: repo.repoPath,
          number: parseInt(bareMatch[1]!, 10),
          text: bareMatch[0],
        };
      }
      return undefined;
    },
    renderer(token): string {
      const t = token as unknown as ItemRefToken;
      const hostAttr = t.platformHost ? ` data-platform-host="${t.platformHost}"` : "";
      const href = t.platformHost
        ? `https://${t.platformHost}/${t.repoPath}/issues/${t.number}`
        : `/${t.provider}/${t.repoPath}/issues/${t.number}`;
      return `<a class="item-ref" href="${href}" data-provider="${t.provider}"${hostAttr} data-owner="${t.owner}" data-name="${t.name}" data-repo-path="${t.repoPath}" data-number="${t.number}">${t.text}</a>`;
    },
  };
}

const htmlCache = new Map<string, string>();
const markedCache = new Map<string, Marked>();

function getMarked(repo?: RepoContext): Marked {
  const key = repo ? `${repo.provider}/${repo.platformHost ?? ""}/${repo.repoPath}` : "";
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
  const key = repo ? `${repo.provider}/${repo.platformHost ?? ""}/${repo.repoPath}\0${raw}` : raw;
  const cached = htmlCache.get(key);
  if (cached !== undefined) return cached;

  const html = DOMPurify.sanitize(
    getMarked(repo).parse(raw) as string,
    {
      ADD_ATTR: [
        "target",
        "data-provider",
        "data-platform-host",
        "data-owner",
        "data-name",
        "data-repo-path",
        "data-number",
      ],
    },
  );
  if (htmlCache.size > 500) htmlCache.clear();
  htmlCache.set(key, html);
  return html;
}
