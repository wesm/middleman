<script lang="ts">
  import { client } from "../../api/runtime.js";

  interface Props {
    selected: string | undefined;
    onchange: (repo: string | undefined) => void;
  }

  const { selected, onchange }: Props = $props();

  let repos = $state<string[]>([]);
  let open = $state(false);
  let search = $state("");
  let dropdownRef: HTMLDivElement | undefined;

  $effect(() => {
    client.GET("/repos").then(({ data, error }) => {
      if (error || !data) return;
      repos = data.map((repo) => `${repo.Owner}/${repo.Name}`);
    });
  });

  // Close on outside click
  $effect(() => {
    if (!open) return;
    function handleClick(e: MouseEvent) {
      if (dropdownRef && !dropdownRef.contains(e.target as Node)) {
        open = false;
        search = "";
      }
    }
    document.addEventListener("mousedown", handleClick);
    return () => document.removeEventListener("mousedown", handleClick);
  });

  const filtered = $derived(
    search.trim() === ""
      ? repos
      : repos.filter((r) => r.toLowerCase().includes(search.toLowerCase())),
  );

  function select(repo: string | undefined): void {
    onchange(repo);
    open = false;
    search = "";
  }

  function displayName(repo: string): string {
    const parts = repo.split("/");
    return parts.length > 1 ? parts[1]! : repo;
  }
</script>

<div class="repo-selector" bind:this={dropdownRef}>
  <button class="selector-btn" onclick={() => { open = !open; }}>
    {#if selected}
      <span class="selector-label">{displayName(selected)}</span>
      <button
        class="selector-clear"
        onclick={(e) => { e.stopPropagation(); select(undefined); }}
        title="Show all repos"
      >×</button>
    {:else}
      <span class="selector-label selector-label--muted">All repos</span>
    {/if}
    <span class="selector-chevron" class:selector-chevron--open={open}>▾</span>
  </button>

  {#if open}
    <div class="selector-dropdown">
      {#if repos.length > 4}
        <div class="selector-search-wrap">
          <!-- svelte-ignore a11y_autofocus -->
          <input
            class="selector-search"
            type="text"
            placeholder="Filter repos..."
            bind:value={search}
            autofocus
          />
        </div>
      {/if}
      <div class="selector-options">
        <button
          class="selector-option"
          class:selector-option--active={selected === undefined}
          onclick={() => select(undefined)}
        >
          All repos
        </button>
        {#each filtered as repo}
          <button
            class="selector-option"
            class:selector-option--active={selected === repo}
            onclick={() => select(repo)}
          >
            <span class="selector-option-name">{displayName(repo)}</span>
            <span class="selector-option-owner">{repo.split("/")[0]}</span>
          </button>
        {/each}
        {#if filtered.length === 0}
          <p class="selector-empty">No matching repos</p>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .repo-selector {
    position: relative;
    flex-shrink: 0;
  }

  .selector-btn {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 3px 8px;
    font-size: 11px;
    font-weight: 500;
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
    background: var(--bg-inset);
    color: var(--text-secondary);
    cursor: pointer;
    transition: border-color 0.1s;
    white-space: nowrap;
    max-width: 140px;
  }

  .selector-btn:hover {
    border-color: var(--border-default);
  }

  .selector-label {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .selector-label--muted {
    color: var(--text-muted);
  }

  .selector-clear {
    font-size: 13px;
    line-height: 1;
    color: var(--text-muted);
    cursor: pointer;
    padding: 0 2px;
  }
  .selector-clear:hover {
    color: var(--text-primary);
  }

  .selector-chevron {
    font-size: 9px;
    color: var(--text-muted);
    transition: transform 0.12s;
    margin-left: auto;
  }
  .selector-chevron--open {
    transform: rotate(180deg);
  }

  .selector-dropdown {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    width: 200px;
    background: var(--bg-surface);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
    z-index: 20;
    animation: dropdown-in 0.1s ease-out;
  }

  @keyframes dropdown-in {
    from { opacity: 0; transform: translateY(-2px); }
    to { opacity: 1; transform: translateY(0); }
  }

  .selector-search-wrap {
    padding: 6px;
    border-bottom: 1px solid var(--border-muted);
  }

  .selector-search {
    width: 100%;
    font-size: 12px;
    padding: 4px 8px;
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
  }
  .selector-search:focus {
    border-color: var(--accent-blue);
    outline: none;
  }

  .selector-options {
    max-height: 200px;
    overflow-y: auto;
    padding: 4px 0;
  }

  .selector-option {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    text-align: left;
    padding: 5px 10px;
    font-size: 12px;
    color: var(--text-primary);
    cursor: pointer;
    transition: background 0.08s;
  }
  .selector-option:hover {
    background: var(--bg-surface-hover);
  }
  .selector-option--active {
    color: var(--accent-blue);
    font-weight: 500;
  }

  .selector-option-owner {
    font-size: 10px;
    color: var(--text-muted);
  }

  .selector-empty {
    padding: 8px 10px;
    font-size: 12px;
    color: var(--text-muted);
    text-align: center;
  }
</style>
