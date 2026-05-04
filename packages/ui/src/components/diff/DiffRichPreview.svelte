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

  let loading = $state(false);
  let error = $state<string | null>(null);
  let preview = $state<FilePreview | null>(null);
  let requestVersion = 0;

  const text = $derived(preview ? decodeText(preview.content) : "");
  const dataURL = $derived(preview ? `data:${preview.media_type};base64,${preview.content}` : "");
  const kind = $derived(previewKind(file.path, preview?.media_type ?? ""));
  const displayText = $derived(formatText(file.path, text));

  $effect(() => {
    if (!active) return;
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
</script>

<div class="preview-shell">
  {#if loading}
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
