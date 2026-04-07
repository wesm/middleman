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
</script>

<div
  class="diff-line"
  class:diff-line--add={type === "add"}
  class:diff-line--del={type === "delete"}
>
  <span
    class="gutter gutter-old"
    class:gutter--add={type === "add"}
    class:gutter--del={type === "delete"}
  >{oldNum ?? ""}</span>
  <span
    class="gutter gutter-new"
    class:gutter--add={type === "add"}
    class:gutter--del={type === "delete"}
  >{newNum ?? ""}</span>
  <span
    class="marker"
    class:marker--add={type === "add"}
    class:marker--del={type === "delete"}
  >{marker}</span>
  <pre class="code">{#each tokens as span}<span style:--dc={span.darkColor} style:--lc={span.lightColor}>{span.content}</span>{/each}{#if noNewline}<span class="no-newline"> (no newline at end of file)</span>{/if}</pre>
</div>

<style>
  .diff-line {
    display: flex;
    align-items: stretch;
    line-height: 20px;
    font-size: 12px;
    background: var(--diff-bg);
  }

  .diff-line--add {
    background: var(--diff-add-bg);
  }

  .diff-line--del {
    background: var(--diff-del-bg);
  }

  .gutter {
    width: 50px;
    flex-shrink: 0;
    text-align: right;
    padding: 0 8px 0 0;
    font-family: var(--font-mono);
    font-size: 11px;
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
    font-size: 12px;
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
    flex: 1;
    min-width: 0;
    margin: 0;
    padding: 0 8px 0 4px;
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 20px;
    color: var(--diff-text);
    white-space: pre;
    overflow-x: visible;
    background: transparent;
    border: none;
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
    font-size: 11px;
  }
</style>
