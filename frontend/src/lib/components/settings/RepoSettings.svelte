<script lang="ts">
  import type { ConfigRepo } from "../../api/types.js";
  import { addRepo, removeRepo, getSettings } from "../../api/settings.js";
  import { refreshSyncStatus } from "../../stores/sync.svelte.js";

  interface Props {
    repos: ConfigRepo[];
    onUpdate: (repos: ConfigRepo[]) => void;
  }

  let { repos, onUpdate }: Props = $props();

  const embedded = typeof window !== "undefined" && window.__MIDDLEMAN_EMBEDDED__ === true;

  let inputValue = $state("");
  let adding = $state(false);
  let addError = $state<string | null>(null);
  let confirmingRemove = $state<string | null>(null);
  let removeError = $state<string | null>(null);

  async function handleAdd(): Promise<void> {
    if (embedded) return;
    const trimmed = inputValue.trim();
    if (!trimmed) return;
    const parts = trimmed.split("/");
    if (parts.length !== 2 || !parts[0] || !parts[1]) {
      addError = "Format: owner/name";
      return;
    }
    adding = true;
    addError = null;
    try {
      await addRepo(parts[0], parts[1]);
      inputValue = "";
      const settings = await getSettings();
      onUpdate(settings.repos);
      void refreshSyncStatus();
    } catch (err) {
      addError = err instanceof Error ? err.message : String(err);
    } finally {
      adding = false;
    }
  }

  async function handleRemove(
    owner: string,
    name: string,
  ): Promise<void> {
    if (embedded) return;
    removeError = null;
    try {
      await removeRepo(owner, name);
      confirmingRemove = null;
      const settings = await getSettings();
      onUpdate(settings.repos);
    } catch (err) {
      removeError = err instanceof Error ? err.message : String(err);
    }
  }

  function handleInputKeydown(e: KeyboardEvent): void {
    if (e.key === "Enter") {
      e.preventDefault();
      void handleAdd();
    }
  }
</script>

<div class="repo-list">
  {#each repos as repo (`${repo.owner}/${repo.name}`)}
    {@const key = `${repo.owner}/${repo.name}`}
    <div class="repo-row">
      <span class="repo-name">{repo.owner}/{repo.name}</span>
      {#if confirmingRemove === key}
        <span class="confirm-prompt">
          Remove?
          <button class="confirm-btn confirm-yes" onclick={() => void handleRemove(repo.owner, repo.name)}>Yes</button>
          <button class="confirm-btn confirm-no" onclick={() => { confirmingRemove = null; removeError = null; }}>No</button>
        </span>
      {:else}
        <button
          class="remove-btn"
          title={`Remove ${key}`}
          onclick={() => { confirmingRemove = key; removeError = null; }}
        >&times;</button>
      {/if}
    </div>
  {/each}
</div>

{#if removeError}
  <div class="error-msg">{removeError}</div>
{/if}

<div class="add-form">
  <input class="add-input" type="text" placeholder="owner/name" bind:value={inputValue} onkeydown={handleInputKeydown} disabled={adding} />
  <button class="add-btn" onclick={() => void handleAdd()} disabled={adding || !inputValue.trim()}>
    {adding ? "Adding..." : "Add"}
  </button>
</div>

{#if addError}
  <div class="error-msg">{addError}</div>
{/if}

<style>
  .repo-list { display: flex; flex-direction: column; }
  .repo-row {
    display: flex; align-items: center; justify-content: space-between;
    padding: 8px 0; border-bottom: 1px solid var(--border-muted);
  }
  .repo-row:last-child { border-bottom: none; }
  .repo-name { font-size: 13px; color: var(--text-primary); font-weight: 500; }
  .remove-btn {
    font-size: 16px; color: var(--text-muted); padding: 2px 6px;
    border-radius: var(--radius-sm); line-height: 1; transition: color 0.1s, background 0.1s;
  }
  .remove-btn:hover:not(:disabled) {
    color: var(--accent-red); background: color-mix(in srgb, var(--accent-red) 10%, transparent);
  }
  .remove-btn:disabled { opacity: 0.3; cursor: not-allowed; }
  .confirm-prompt { font-size: 12px; color: var(--text-secondary); display: flex; align-items: center; gap: 6px; }
  .confirm-btn { font-size: 11px; font-weight: 600; padding: 2px 8px; border-radius: var(--radius-sm); }
  .confirm-yes { color: var(--accent-red); border: 1px solid var(--accent-red); }
  .confirm-yes:hover { background: color-mix(in srgb, var(--accent-red) 10%, transparent); }
  .confirm-no { color: var(--text-muted); border: 1px solid var(--border-muted); }
  .confirm-no:hover { background: var(--bg-surface-hover); }
  .add-form { display: flex; gap: 8px; }
  .add-input {
    flex: 1; font-size: 13px; padding: 6px 10px;
    background: var(--bg-inset); border: 1px solid var(--border-muted); border-radius: var(--radius-sm);
  }
  .add-input:focus { border-color: var(--accent-blue); outline: none; }
  .add-btn {
    padding: 6px 14px; font-size: 13px; font-weight: 500; color: white;
    background: var(--accent-blue); border-radius: var(--radius-sm); transition: opacity 0.12s;
  }
  .add-btn:hover:not(:disabled) { opacity: 0.9; }
  .add-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .error-msg { font-size: 12px; color: var(--accent-red); padding: 4px 0; }
</style>
