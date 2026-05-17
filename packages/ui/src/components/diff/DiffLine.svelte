<script lang="ts">
  import type { DualToken } from "../../utils/highlight.js";

  interface Props {
    type: "context" | "add" | "delete";
    content: string;
    oldNum?: number;
    newNum?: number;
    noNewline?: boolean;
    tokens: DualToken[];
  }

  const { type, oldNum, newNum, noNewline, tokens }: Props = $props();

  const marker = $derived(type === "add" ? "+" : type === "delete" ? "-" : " ");
  const lineNum = $derived(newNum ?? oldNum ?? "");
</script>

<div
  class="diff-line"
  class:diff-line--add={type === "add"}
  class:diff-line--del={type === "delete"}
>
  <span
    class="gutter"
    class:gutter--add={type === "add"}
    class:gutter--del={type === "delete"}
  >{lineNum}</span>
  <span
    class="marker"
    class:marker--add={type === "add"}
    class:marker--del={type === "delete"}
  >{marker}</span>
  <pre class="code">{#each tokens as span, index (index)}<span style:--dc={span.darkColor} style:--lc={span.lightColor}>{span.content}</span>{/each}{#if noNewline}<span class="no-newline"> (no newline at end of file)</span>{/if}</pre>
</div>

<style>
  .diff-line {
    display: flex;
    align-items: stretch;
    line-height: 20px;
    font-size: var(--font-size-sm);
    background: var(--diff-bg);
  }

  .diff-line--add {
    background: var(--diff-add-bg);
  }

  .diff-line--del {
    background: var(--diff-del-bg);
  }

  .gutter {
    width: var(--diff-line-number-gutter-width, 50px);
    flex-shrink: 0;
    text-align: right;
    padding: 0 8px 0 1ch;
    font-family: var(--font-mono);
    font-size: var(--font-size-xs);
    color: var(--diff-line-num);
    user-select: none;
    line-height: 20px;
    background: var(--diff-bg);
  }

  .gutter--add {
    background: var(--diff-add-gutter);
  }

  .gutter--del {
    background: var(--diff-del-gutter);
  }

  .marker {
    width: 16px;
    flex-shrink: 0;
    text-align: center;
    font-family: var(--font-mono);
    font-size: var(--font-size-sm);
    color: var(--diff-text);
    user-select: none;
    line-height: 20px;
  }

  .marker--add {
    color: var(--diff-add-text);
  }

  .marker--del {
    color: var(--diff-del-text);
  }

  .code {
    flex: 1 0 auto;
    margin: 0;
    padding: 0 8px 0 4px;
    font-family: var(--font-mono);
    font-size: var(--font-size-sm);
    line-height: 20px;
    color: var(--diff-text);
    white-space: pre;
    background: transparent;
    border: none;
  }

  :global(.diff-area--word-wrap) .code {
    flex-basis: 0;
    min-width: 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  /* Token colors via CSS custom properties — theme switch is pure CSS,
     no JS re-renders needed. Each span carries --dc (dark) and --lc (light). */
  .code span:not(.no-newline) {
    color: var(--lc, inherit);
  }

  :global(html.dark) .code span:not(.no-newline) {
    color: var(--dc, inherit);
  }

  .no-newline {
    color: var(--diff-line-num);
    font-style: italic;
    font-size: var(--font-size-xs);
  }
</style>
