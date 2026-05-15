<script lang="ts">
  import { tick } from "svelte";
  import { getClient } from "../context.js";

  const client = getClient();
  import type { Repo } from "../api/types.js";

  interface Props {
    selected: string | undefined;
    onchange: (repo: string | undefined) => void;
  }

  let { selected, onchange }: Props = $props();

  let repos = $state<Repo[]>([]);
  let query = $state("");
  let open = $state(false);
  let highlightIndex = $state(0);
  let inputEl = $state<HTMLInputElement>();
  let containerEl = $state<HTMLDivElement>();

  $effect(() => {
    void client.GET("/repos").then(({ data, error }) => {
      if (error) return;
      repos = data ?? [];
    });
  });

  const options = $derived.by(() => {
    return repos.map((r) => ({
      value: `${r.PlatformHost}/${r.Owner}/${r.Name}`,
      owner: r.Owner,
      name: r.Name,
    }));
  });

  const filtered = $derived.by(() => {
    if (!query) return options;
    const q = query.toLowerCase();
    return options.filter(
      (o) => o.value.toLowerCase().includes(q),
    );
  });

  const displayValue = $derived(
    selected ?? "All repos",
  );

  async function openDropdown() {
    query = "";
    open = true;
    highlightIndex = 0;
    await tick();
    inputEl?.focus();
  }

  function closeDropdown() {
    open = false;
    query = "";
  }

  function select(value: string | undefined) {
    onchange(value);
    closeDropdown();
  }

  function handleKeydown(e: KeyboardEvent) {
    const total = filtered.length + 1;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      highlightIndex = Math.min(highlightIndex + 1, total - 1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      highlightIndex = Math.max(highlightIndex - 1, 0);
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (highlightIndex === 0) {
        select(undefined);
      } else {
        const item = filtered[highlightIndex - 1];
        if (item) select(item.value);
      }
    } else if (e.key === "Escape") {
      closeDropdown();
    }
  }

  function handleInput() {
    highlightIndex = 0;
  }

  function highlightSegments(
    text: string, q: string,
  ): { text: string; match: boolean }[] {
    if (!q) return [{ text, match: false }];
    const idx = text.toLowerCase().indexOf(q.toLowerCase());
    if (idx === -1) return [{ text, match: false }];
    return [
      ...(idx > 0
        ? [{ text: text.slice(0, idx), match: false }]
        : []),
      { text: text.slice(idx, idx + q.length), match: true },
      ...(idx + q.length < text.length
        ? [{ text: text.slice(idx + q.length), match: false }]
        : []),
    ];
  }

  function handleBlur(e: FocusEvent) {
    const related = e.relatedTarget as Node | null;
    if (containerEl && related && containerEl.contains(related)) {
      return;
    }
    closeDropdown();
  }

  function preventBlur(e: MouseEvent) {
    e.preventDefault();
  }
</script>

<div class="typeahead" bind:this={containerEl}>
  {#if open}
    <input
      bind:this={inputEl}
      class="typeahead-input"
      type="text"
      bind:value={query}
      oninput={handleInput}
      onkeydown={handleKeydown}
      onblur={handleBlur}
      placeholder="Filter repos..."
      aria-label="Filter repos"
      autocomplete="off"
    />
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <ul class="typeahead-list" role="listbox" onmousedown={preventBlur}>
      <li
        class="typeahead-option"
        class:highlighted={highlightIndex === 0}
        class:selected={selected === undefined}
        role="option"
        aria-selected={selected === undefined}
        onmousedown={() => select(undefined)}
        onmouseenter={() => (highlightIndex = 0)}
      >All repos</li>
      {#each filtered as option, i (option.value)}
        <li
          class="typeahead-option"
          class:highlighted={i + 1 === highlightIndex}
          class:selected={option.value === selected}
          role="option"
          aria-selected={option.value === selected}
          onmousedown={() => select(option.value)}
          onmouseenter={() => (highlightIndex = i + 1)}
        >
          {#each highlightSegments(option.value, query) as seg, segmentIndex (`${segmentIndex}:${seg.text}`)}{#if seg.match}<mark class="match">{seg.text}</mark>{:else}{seg.text}{/if}{/each}
        </li>
      {:else}
        <li class="typeahead-empty">No matching repos</li>
      {/each}
    </ul>
  {:else}
    <button class="typeahead-trigger" onclick={openDropdown} title="Select repository">
      <span class="typeahead-value">{displayValue}</span>
      <svg class="typeahead-chevron" width="10" height="10" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
        <path d="M1.646 4.646a.5.5 0 01.708 0L8 10.293l5.646-5.647a.5.5 0 01.708.708l-6 6a.5.5 0 01-.708 0l-6-6a.5.5 0 010-.708z"/>
      </svg>
    </button>
  {/if}
</div>

<style>
  .typeahead {
    position: relative;
    min-width: 160px;
    max-width: 260px;
  }

  .typeahead-trigger {
    height: 26px;
    width: 100%;
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 0 8px;
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
    font-size: var(--font-size-xs);
    color: var(--text-secondary);
    cursor: pointer;
    transition: border-color 0.15s;
    text-align: left;
  }

  .typeahead-trigger:hover {
    border-color: var(--border-default);
  }

  .typeahead-value {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .typeahead-chevron {
    flex-shrink: 0;
    opacity: 0.5;
  }

  .typeahead-input {
    height: 26px;
    width: 100%;
    padding: 0 8px;
    background: var(--bg-inset);
    border: 1px solid var(--accent-blue);
    border-radius: var(--radius-sm);
    font-size: var(--font-size-xs);
    color: var(--text-primary);
    outline: none;
    box-sizing: border-box;
  }

  .typeahead-input::placeholder {
    color: var(--text-muted);
  }

  .typeahead-list {
    position: absolute;
    top: 100%;
    left: 0;
    right: 0;
    margin-top: 2px;
    max-height: 50vh;
    overflow-y: auto;
    background: var(--bg-surface);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-md);
    z-index: 100;
    list-style: none;
    padding: 2px;
  }

  .typeahead-option {
    padding: 4px 8px;
    font-size: var(--font-size-xs);
    color: var(--text-secondary);
    cursor: pointer;
    border-radius: 3px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .typeahead-option.highlighted {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .typeahead-option.selected {
    color: var(--accent-blue);
    font-weight: 600;
  }

  .match {
    background: color-mix(in srgb, var(--accent-blue) 40%, transparent);
    color: var(--accent-blue);
    font-weight: 600;
    border-radius: 1px;
  }

  .typeahead-empty {
    padding: 6px 8px;
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    font-style: italic;
  }
</style>
