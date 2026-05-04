<script lang="ts">
  import type { DiffFile, FilePreview } from "../../api/types.js";
  import { getStores } from "../../context.js";
  import { renderMarkdown } from "../../utils/markdown.js";

  interface Props {
    file: DiffFile;
    owner: string;
    name: string;
    number: number;
    active: boolean;
  }

  const { file, owner, name, number, active }: Props = $props();
  const { diff: diffStore } = getStores();

  interface MarkdownDiffBlock {
    type: "context" | "add" | "delete";
    html: string;
  }

  let loading = $state(false);
  let error = $state<string | null>(null);
  let preview = $state<FilePreview | null>(null);
  let requestVersion = 0;

  const isMarkdownFile = $derived(isMarkdownPath(file.path));
  const markdownBlocks = $derived.by(() => buildMarkdownDiff(file));
  const text = $derived(preview ? decodeText(preview.content) : "");
  const dataURL = $derived(preview ? `data:${preview.media_type};base64,${preview.content}` : "");
  const kind = $derived(previewKind(file.path, preview?.media_type ?? ""));
  const displayText = $derived(formatText(file.path, text));

  $effect(() => {
    if (!active || isMarkdownFile) return;
    const version = ++requestVersion;
    loading = true;
    error = null;
    preview = null;
    void diffStore.loadFilePreview(owner, name, number, file.path)
      .then((result) => {
        if (version !== requestVersion) return;
        preview = result;
      })
      .catch((err: unknown) => {
        if (version !== requestVersion) return;
        error = err instanceof Error ? err.message : String(err);
      })
      .finally(() => {
        if (version === requestVersion) loading = false;
      });
  });

  function decodeText(content: string): string {
    const binary = atob(content);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
      bytes[i] = binary.charCodeAt(i);
    }
    return new TextDecoder("utf-8", { fatal: false }).decode(bytes);
  }

  function isMarkdownPath(path: string): boolean {
    return [".md", ".markdown", ".mdown", ".mkd"].includes(extension(path));
  }

  function extension(path: string): string {
    const idx = path.lastIndexOf(".");
    return idx >= 0 ? path.slice(idx).toLowerCase() : "";
  }

  function previewKind(
    path: string,
    mediaType: string,
  ): "markdown" | "image" | "pdf" | "text" | "unsupported" {
    const ext = extension(path);
    if (mediaType.startsWith("image/")) return "image";
    if (mediaType === "application/pdf") return "pdf";
    if (
      mediaType.includes("markdown") ||
      [".md", ".markdown", ".mdown", ".mkd"].includes(ext)
    ) return "markdown";
    if (
      mediaType.startsWith("text/") ||
      mediaType.includes("json") ||
      mediaType.includes("yaml") ||
      mediaType.includes("toml") ||
      [".css", ".csv", ".html", ".js", ".jsx", ".ts", ".tsx", ".xml"].includes(ext)
    ) return "text";
    return "unsupported";
  }

  function formatText(path: string, value: string): string {
    if (extension(path) !== ".json") return value;
    try {
      return `${JSON.stringify(JSON.parse(value), null, 2)}\n`;
    } catch {
      return value;
    }
  }

  function buildMarkdownDiff(source: DiffFile): MarkdownDiffBlock[] {
    const blocks: MarkdownDiffBlock[] = [];
    let currentType: MarkdownDiffBlock["type"] | null = null;
    let currentLines: string[] = [];

    function flush(): void {
      if (!currentType || currentLines.length === 0) return;
      const markdown = `${currentLines.join("\n")}\n`;
      blocks.push({
        type: currentType,
        html: renderMarkdown(markdown, { owner, name }),
      });
      currentType = null;
      currentLines = [];
    }

    for (const hunk of source.hunks) {
      for (const line of hunk.lines) {
        if (line.type !== currentType) {
          flush();
          currentType = line.type;
        }
        currentLines.push(line.content);
      }
    }
    flush();
    return blocks;
  }
</script>

<div class="preview-shell">
  {#if isMarkdownFile}
    <div class="diff-rich-preview markdown-body markdown-rich-diff">
      {#each markdownBlocks as block, index (`${index}:${block.type}`)}
        <div
          class={[
            "markdown-rich-diff__block",
            `markdown-rich-diff__block--${block.type}`,
          ]}
        >
          {@html block.html}
        </div>
      {/each}
    </div>
  {:else if loading}
    <div class="preview-state">Loading preview</div>
  {:else if error}
    <div class="preview-state preview-state--error">{error}</div>
  {:else if preview}
    {#if kind === "markdown"}
      <div class="diff-rich-preview markdown-body">
        {@html renderMarkdown(text, { owner, name })}
      </div>
    {:else if kind === "image"}
      <div class="diff-image-preview">
        <img src={dataURL} alt={file.path} />
      </div>
    {:else if kind === "pdf"}
      <object
        class="diff-object-preview"
        data={dataURL}
        type={preview.media_type}
        aria-label={`${file.path} preview`}
      >
        <a href={dataURL}>Open PDF preview</a>
      </object>
    {:else if kind === "text"}
      <pre class="diff-text-preview">{displayText}</pre>
    {:else}
      <div class="preview-state">No rich preview for {preview.media_type}</div>
    {/if}
  {/if}
</div>

<style>
  .preview-shell {
    min-height: 140px;
    background: var(--bg-surface);
  }

  .preview-state {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 140px;
    padding: 20px;
    color: var(--text-muted);
    font-size: 13px;
  }

  .preview-state--error {
    color: var(--accent-red);
  }

  .diff-rich-preview {
    max-width: 920px;
    padding: 24px 32px 36px;
    color: var(--text-primary);
  }

  .markdown-rich-diff {
    max-width: 980px;
  }

  .markdown-rich-diff__block {
    position: relative;
    padding: 1px 12px;
    border-left: 3px solid transparent;
  }

  .markdown-rich-diff__block--add {
    background: color-mix(in srgb, var(--diff-add-bg) 76%, transparent);
    border-left-color: var(--diff-add-text);
  }

  .markdown-rich-diff__block--delete {
    background: color-mix(in srgb, var(--diff-del-bg) 78%, transparent);
    border-left-color: var(--diff-del-text);
  }

  .markdown-rich-diff__block--add :global(*) {
    color: var(--text-primary);
  }

  .markdown-rich-diff__block--delete :global(*) {
    color: var(--text-primary);
  }

  .markdown-rich-diff__block--delete :global(p),
  .markdown-rich-diff__block--delete :global(li),
  .markdown-rich-diff__block--delete :global(h1),
  .markdown-rich-diff__block--delete :global(h2),
  .markdown-rich-diff__block--delete :global(h3),
  .markdown-rich-diff__block--delete :global(h4),
  .markdown-rich-diff__block--delete :global(h5),
  .markdown-rich-diff__block--delete :global(h6) {
    text-decoration: line-through;
    text-decoration-color: color-mix(in srgb, var(--diff-del-text) 70%, transparent);
  }

  .diff-image-preview {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 240px;
    padding: 24px;
    background:
      linear-gradient(45deg, var(--bg-inset) 25%, transparent 25%),
      linear-gradient(-45deg, var(--bg-inset) 25%, transparent 25%),
      linear-gradient(45deg, transparent 75%, var(--bg-inset) 75%),
      linear-gradient(-45deg, transparent 75%, var(--bg-inset) 75%);
    background-color: var(--bg-surface);
    background-position: 0 0, 0 10px, 10px -10px, -10px 0;
    background-size: 20px 20px;
  }

  .diff-image-preview img {
    max-width: min(100%, 960px);
    max-height: 70vh;
    object-fit: contain;
    border: 1px solid var(--border-muted);
    background: var(--bg-surface);
  }

  .diff-object-preview {
    width: 100%;
    height: min(72vh, 900px);
    border: 0;
    background: var(--bg-surface);
  }

  .diff-text-preview {
    margin: 0;
    padding: 18px 22px 28px;
    color: var(--diff-text);
    background: var(--diff-bg);
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.55;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
