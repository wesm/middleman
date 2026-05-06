<script lang="ts">
  import { tick } from "svelte";
  import { getStores } from "@middleman/ui";
  import type { ConfigRepo } from "@middleman/ui/api/types";
  import { addRepo, removeRepo, getSettings, refreshRepo } from "../../api/settings.js";
  import RepoImportModal from "./RepoImportModal.svelte";

  const { sync } = getStores();

  interface Props {
    repos: ConfigRepo[];
    onUpdate: (repos: ConfigRepo[]) => void;
  }

  let { repos, onUpdate }: Props = $props();

  import { isEmbedded } from "../../stores/embed-config.svelte.js";
  const embedded = isEmbedded();

  let importOpen = $state(false);
  let importTrigger = $state<HTMLButtonElement | null>(null);
  let inputValue = $state("");
  let adding = $state(false);
  let addError = $state<string | null>(null);
  let confirmingRemove = $state<string | null>(null);
  let removeError = $state<string | null>(null);
  let refreshingByKey = $state<Record<string, boolean>>({});
  let refreshErrors = $state<Record<string, string>>({});

  function repoKey(repo: ConfigRepo): string {
    return `${repo.provider}/${repo.platform_host}/${repo.repo_path || `${repo.owner}/${repo.name}`}`.toLowerCase();
  }

  function repoLabel(repo: ConfigRepo): string {
    return repo.repo_path || `${repo.owner}/${repo.name}`;
  }

  async function handleAdd(): Promise<void> {
    if (embedded) return;
    const trimmed = inputValue.trim();
    if (!trimmed) return;
    const parts = trimmed.split("/");
    if (parts.length !== 3 || !parts[0] || !parts[1] || !parts[2]) {
      addError = "Format: provider/owner/name";
      return;
    }
    adding = true;
    addError = null;
    try {
      const settings = await addRepo(parts[1], parts[2], {
        provider: parts[0],
      });
      inputValue = "";
      onUpdate(settings.repos);
      void sync.refreshSyncStatus();
    } catch (err) {
      addError = err instanceof Error ? err.message : String(err);
    } finally {
      adding = false;
    }
  }

  async function handleRemove(repo: ConfigRepo): Promise<void> {
    if (embedded) return;
    removeError = null;
    try {
      await removeRepo(repo.owner, repo.name, {
        provider: repo.provider,
        host: repo.platform_host,
      });
      confirmingRemove = null;
      const settings = await getSettings();
      onUpdate(settings.repos);
      void sync.refreshSyncStatus();
    } catch (err) {
      removeError = err instanceof Error ? err.message : String(err);
    }
  }

  async function handleRefresh(repo: ConfigRepo): Promise<void> {
    if (embedded) return;
    const key = repoKey(repo);
    refreshingByKey = { ...refreshingByKey, [key]: true };
    if (refreshErrors[key]) {
      const nextErrors = { ...refreshErrors };
      delete nextErrors[key];
      refreshErrors = nextErrors;
    }
    try {
      const settings = await refreshRepo(repo.owner, repo.name, {
        provider: repo.provider,
        host: repo.platform_host,
      });
      onUpdate(settings.repos);
      void sync.refreshSyncStatus();
    } catch (err) {
      refreshErrors = {
        ...refreshErrors,
        [key]: err instanceof Error ? err.message : String(err),
      };
    } finally {
      refreshingByKey = { ...refreshingByKey, [key]: false };
    }
  }

  function handleInputKeydown(e: KeyboardEvent): void {
    if (e.key === "Enter") {
      e.preventDefault();
      void handleAdd();
    }
  }

  async function closeImportModal(): Promise<void> {
    importOpen = false;
    await tick();
    importTrigger?.focus();
  }
</script>

{#if !embedded}
  <div class="repo-import-entry">
    <button bind:this={importTrigger} class="primary-import-btn" type="button" onclick={() => { importOpen = true; }}>Add repositories…</button>
    <p>Preview a glob, filter results, and add selected repositories as exact entries.</p>
  </div>
{/if}

<RepoImportModal
  open={importOpen}
  onClose={() => { void closeImportModal(); }}
  onImported={(settings) => {
    onUpdate(settings.repos);
    void sync.refreshSyncStatus();
  }}
/>

<div class="repo-list">
  {#each repos as repo (repoKey(repo))}
    {@const key = repoKey(repo)}
    {@const label = repoLabel(repo)}
    <div class="repo-row">
      <div class="repo-main">
        <span class="repo-name">
          {label}
          {#if repo.is_glob}
            <span class="repo-count">({repo.matched_repo_count})</span>
          {/if}
        </span>
        {#if refreshErrors[key]}
          <div class="error-msg row-error">{refreshErrors[key]}</div>
        {/if}
      </div>
      {#if confirmingRemove === key}
        <span class="confirm-prompt">
          Remove?
          <button class="confirm-btn confirm-yes" onclick={() => void handleRemove(repo)}>Yes</button>
          <button class="confirm-btn confirm-no" onclick={() => { confirmingRemove = null; removeError = null; }}>No</button>
        </span>
      {:else}
        <div class="repo-actions">
          {#if repo.is_glob}
            <button
              class="refresh-btn"
              onclick={() => void handleRefresh(repo)}
              disabled={Boolean(refreshingByKey[key])}
            >
              {refreshingByKey[key] ? "Refreshing..." : "Refresh"}
            </button>
          {/if}
          <button
            class="remove-btn"
            title={`Remove ${key}`}
            onclick={() => {
              confirmingRemove = key;
              removeError = null;
              if (refreshErrors[key]) {
                const nextErrors = { ...refreshErrors };
                delete nextErrors[key];
                refreshErrors = nextErrors;
              }
            }}
          >&times;</button>
        </div>
      {/if}
    </div>
  {/each}
</div>

{#if removeError}
  <div class="error-msg">{removeError}</div>
{/if}

{#if !embedded}
  <details class="advanced-add">
    <summary>Advanced: add exact repo or tracking glob directly</summary>
    <div class="advanced-body">
      <div class="add-form">
        <input class="add-input" type="text" placeholder="owner/name" bind:value={inputValue} onkeydown={handleInputKeydown} disabled={adding} />
        <button class="add-btn" onclick={() => void handleAdd()} disabled={adding || !inputValue.trim()}>
          {adding ? "Adding..." : "Add"}
        </button>
      </div>

      {#if addError}
        <div class="error-msg">{addError}</div>
      {/if}
    </div>
  </details>
{/if}

<style>
  .repo-import-entry { display: flex; flex-direction: column; gap: 4px; padding-bottom: 12px; border-bottom: 1px solid var(--border-muted); }
  .primary-import-btn { align-self: flex-start; padding: 6px 14px; font-size: 13px; font-weight: 600; color: white; background: var(--accent-blue); border-radius: var(--radius-sm); }
  .repo-import-entry p { margin: 0; color: var(--text-muted); font-size: 12px; }
  .advanced-add { padding-top: 8px; }
  .advanced-add summary { cursor: pointer; color: var(--text-secondary); font-size: 12px; }
  .advanced-body { padding-top: 8px; display: flex; flex-direction: column; gap: 6px; }
  .repo-list { display: flex; flex-direction: column; }
  .repo-row {
    display: flex; align-items: center; justify-content: space-between;
    padding: 8px 0; border-bottom: 1px solid var(--border-muted);
    gap: 12px;
  }
  .repo-row:last-child { border-bottom: none; }
  .repo-main { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
  .repo-name { font-size: 13px; color: var(--text-primary); font-weight: 500; }
  .repo-count { color: var(--text-muted); margin-left: 4px; }
  .repo-actions { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
  .refresh-btn {
    padding: 4px 10px; font-size: 12px; font-weight: 500;
    color: var(--accent-blue); border: 1px solid color-mix(in srgb, var(--accent-blue) 35%, var(--border-muted));
    border-radius: var(--radius-sm); transition: background 0.12s, opacity 0.12s;
  }
  .refresh-btn:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent-blue) 10%, transparent);
  }
  .refresh-btn:disabled { opacity: 0.5; cursor: not-allowed; }
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
  .row-error { padding: 0; }
</style>
