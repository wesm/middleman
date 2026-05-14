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

export interface RenderMarkdownOpts {
  // When true, GFM task-list checkboxes render as enabled <input> elements
  // tagged with data-task-index="N" (zero-based, in document order). The
  // caller is responsible for intercepting clicks and persisting state —
  // unhandled clicks toggle visually but do not save.
  interactiveTasks?: boolean;
}

// Per-render state for the custom checkbox renderer. Marked is single-
// threaded synchronous, so a module-level variable is safe.
let renderState = {
  taskIndex: 0,
  interactiveTasks: false,
  // Set by the checkbox renderer to the index it just emitted, then
  // read by the listitem renderer (which fires AFTER its inner tokens)
  // so a single task carries the same index on both elements.
  lastCheckboxIndex: -1,
};

const htmlCache = new Map<string, string>();
const markedCache = new Map<string, Marked>();

// Six-dot drag handle SVG used to grab a task-list item. Inlined so
// the rendered markdown is self-contained and no extra fetch is needed.
const DRAG_HANDLE_SVG =
  `<svg viewBox="0 0 12 16" width="12" height="16" aria-hidden="true">`
  + `<circle cx="3" cy="3" r="1.2"/>`
  + `<circle cx="9" cy="3" r="1.2"/>`
  + `<circle cx="3" cy="8" r="1.2"/>`
  + `<circle cx="9" cy="8" r="1.2"/>`
  + `<circle cx="3" cy="13" r="1.2"/>`
  + `<circle cx="9" cy="13" r="1.2"/>`
  + `</svg>`;

function getMarked(repo?: RepoContext): Marked {
  const key = repo ? `${repo.provider}/${repo.platformHost ?? ""}/${repo.repoPath}` : "";
  let instance = markedCache.get(key);
  if (!instance) {
    instance = new Marked({ breaks: true, gfm: true });
    instance.use({ extensions: [itemRefExtension(repo)] });
    instance.use({
      renderer: {
        // The checkbox renderer runs first (as the parser walks the
        // inner tokens of a task list item) and owns the index; the
        // listitem renderer reads that index back to attach a matching
        // drag handle and wrapper data attribute.
        checkbox({ checked }: { checked: boolean }): string {
          const index = renderState.taskIndex++;
          renderState.lastCheckboxIndex = index;
          const checkedAttr = checked ? ' checked=""' : "";
          if (renderState.interactiveTasks) {
            return `<input${checkedAttr} type="checkbox" data-task-index="${index}">`;
          }
          return `<input${checkedAttr} disabled="" type="checkbox">`;
        },
        listitem(token: {
          task?: boolean;
          loose?: boolean;
          tokens?: unknown[];
        }): string {
          const self = this as unknown as {
            parser: { parse(toks: unknown[], loose: boolean): string };
          };
          // Reset the checkbox index sentinel so the listitem renderer
          // can detect whether the inner parse actually emitted one.
          renderState.lastCheckboxIndex = -1;
          const inner = self.parser.parse(
            (token.tokens ?? []) as unknown[],
            !!token.loose,
          );
          if (!token.task) return `<li>${inner}</li>\n`;
          if (!renderState.interactiveTasks) {
            return `<li class="task-list-item">${inner}</li>\n`;
          }
          const index = renderState.lastCheckboxIndex;
          const handle =
            `<span class="task-drag-handle" `
            + `data-task-index="${index}" `
            + `draggable="true" `
            + `role="button" `
            + `tabindex="-1" `
            + `aria-label="Drag to reorder">`
            + DRAG_HANDLE_SVG
            + `</span>`;
          return (
            `<li class="task-list-item task-list-item--interactive" `
            + `data-task-index="${index}">`
            + `${handle}${inner}</li>\n`
          );
        },
      },
    });
    markedCache.set(key, instance);
  }
  return instance;
}

export function renderMarkdown(
  raw: string,
  repo?: RepoContext,
  opts: RenderMarkdownOpts = {},
): string {
  if (!raw) return "";
  const interactiveTasks = !!opts.interactiveTasks;
  const repoKey = repo
    ? `${repo.provider}/${repo.platformHost ?? ""}/${repo.repoPath}`
    : "";
  const key = `${repoKey}\0${interactiveTasks ? 1 : 0}\0${raw}`;
  const cached = htmlCache.get(key);
  if (cached !== undefined) return cached;

  renderState = { taskIndex: 0, interactiveTasks, lastCheckboxIndex: -1 };
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
        "data-task-index",
        "draggable",
      ],
    },
  );
  if (htmlCache.size > 500) htmlCache.clear();
  htmlCache.set(key, html);
  return html;
}
