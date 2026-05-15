<script lang="ts">
  import type { IssueLabel } from "../../api/types.js";

  const DARK_TEXT_COLOR = "#1f2328";
  const WHITE_TEXT_COLOR = "#ffffff";

  interface Props {
    labels?: IssueLabel[] | null;
    mode?: "compact" | "full";
  }

  const { labels = [], mode = "full" }: Props = $props();

  function normalizeColor(color: string): string {
    const hex = color.trim().replace(/^#/, "");

    if (/^[0-9a-fA-F]{3}$/.test(hex)) {
      return `#${hex
        .split("")
        .map((char) => `${char}${char}`)
        .join("")
        .toLowerCase()}`;
    }

    if (/^[0-9a-fA-F]{6}$/.test(hex)) {
      return `#${hex.toLowerCase()}`;
    }

    return "#6e7781";
  }

  function textColor(backgroundColor: string): string {
    const hex = normalizeColor(backgroundColor).slice(1);
    const red = Number.parseInt(hex.slice(0, 2), 16);
    const green = Number.parseInt(hex.slice(2, 4), 16);
    const blue = Number.parseInt(hex.slice(4, 6), 16);

    const channel = (value: number): number => {
      const normalized = value / 255;
      return normalized <= 0.03928
        ? normalized / 12.92
        : ((normalized + 0.055) / 1.055) ** 2.4;
    };

    const luminance =
      0.2126 * channel(red)
      + 0.7152 * channel(green)
      + 0.0722 * channel(blue);

    const contrastRatio = (foreground: string): number => {
      const foregroundHex = normalizeColor(foreground).slice(1);
      const foregroundLuminance =
        0.2126 * channel(Number.parseInt(foregroundHex.slice(0, 2), 16))
        + 0.7152 * channel(Number.parseInt(foregroundHex.slice(2, 4), 16))
        + 0.0722 * channel(Number.parseInt(foregroundHex.slice(4, 6), 16));

      const lighter = Math.max(luminance, foregroundLuminance);
      const darker = Math.min(luminance, foregroundLuminance);
      return (lighter + 0.05) / (darker + 0.05);
    };

    return contrastRatio(DARK_TEXT_COLOR) >= contrastRatio(WHITE_TEXT_COLOR)
      ? DARK_TEXT_COLOR
      : WHITE_TEXT_COLOR;
  }

  function labelStyle(color: string): string {
    const background = normalizeColor(color);

    return `background-color: ${background}; color: ${textColor(background)}; border-color: ${background};`;
  }

  const resolvedLabels = $derived(labels ?? []);
  const visibleLabels = $derived(mode === "compact" ? resolvedLabels.slice(0, 2) : resolvedLabels);
  const overflowCount = $derived(mode === "compact" ? Math.max(0, resolvedLabels.length - 2) : 0);
</script>

{#if resolvedLabels.length > 0}
  <div class="github-labels" class:github-labels--compact={mode === "compact"} class:github-labels--full={mode === "full"}>
    {#each visibleLabels as label (label.name)}
      <span class="label-pill" style={labelStyle(label.color)}>{label.name}</span>
    {/each}
    {#if overflowCount > 0}
      <span class="label-more">+{overflowCount}</span>
    {/if}
  </div>
{/if}

<style>
  .github-labels {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }

  .github-labels--compact {
    overflow: hidden;
  }

  .github-labels--full {
    flex-wrap: wrap;
  }

  .label-pill {
    display: inline-flex;
    align-items: center;
    min-width: 0;
    max-width: 100%;
    border: 1px solid transparent;
    border-radius: 999px;
    padding: 1px 8px;
    font-size: var(--font-size-xs);
    font-weight: 600;
    line-height: 1.5;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .github-labels--compact .label-pill {
    max-width: 120px;
    font-size: var(--font-size-2xs);
    padding: 1px 6px;
  }

  .label-more {
    flex-shrink: 0;
    color: var(--text-muted);
    font-size: var(--font-size-2xs);
  }
</style>
