<script lang="ts">
  import { tick } from "svelte";
  import type { Settings } from "@middleman/ui/api/types";
  import { bulkAddRepos, previewRepos, type RepoPreviewRow } from "../../api/settings.js";
  import RepoPreviewTable from "./RepoPreviewTable.svelte";
  import {
    applyRangeSelection,
    filterRows,
    parseImportPattern,
    rowKey,
    selectedRowsForSubmit,
    setAllVisible,
    sortRows,
    type SortState,
    type StatusFilter,
  } from "./repoImportSelection.js";

  interface Props {
    open: boolean;
    onClose: () => void;
    onImported: (settings: Settings) => void;
  }

  let { open, onClose, onImported }: Props = $props();

  let patternInput = $state("");
  let rows = $state.raw<RepoPreviewRow[]>([]);
  let selected = $state<Set<string>>(new Set());
  let filterText = $state("");
  let statusFilter = $state<StatusFilter>("all");
  let hideForks = $state(false);
  let hidePrivate = $state(false);
  let sort = $state<SortState>({ field: "pushed_at", direction: "desc" });
  let anchorKey = $state<string | null>(null);
  let loading = $state(false);
  let submitting = $state(false);
  let error = $state<string | null>(null);
  let requestToken = 0;
  let inputEl = $state<HTMLInputElement | null>(null);

  const sortedRows = $derived(sortRows(rows, sort));
  const visibilityFilters = $derived({ hideForks, hidePrivate });
  const visibleRows = $derived(filterRows(sortedRows, filterText, statusFilter, selected, visibilityFilters));
  const selectableVisibleCount = $derived(visibleRows.filter((row) => !row.already_configured).length);
  const selectedCount = $derived(visibleRows.filter((row) => selected.has(rowKey(row)) && !row.already_configured).length);
  const submitRows = $derived(selectedRowsForSubmit(sortedRows, selected, visibilityFilters));

  $effect(() => {
    if (open) {
      void tick().then(() => inputEl?.focus());
    } else {
      resetAll();
    }
  });

  function resetPreviewState(): void {
    rows = [];
    selected = new Set();
    filterText = "";
    statusFilter = "all";
    hideForks = false;
    hidePrivate = false;
    sort = { field: "pushed_at", direction: "desc" };
    anchorKey = null;
  }

  function resetAll(): void {
    patternInput = "";
    resetPreviewState();
    error = null;
    loading = false;
    submitting = false;
    requestToken += 1;
  }

  function handlePatternInput(value: string): void {
    patternInput = value;
    requestToken += 1;
    resetPreviewState();
    error = null;
    loading = false;
  }

  async function handlePreview(): Promise<void> {
    if (loading) return;
    let parsed: { owner: string; pattern: string };
    try {
      parsed = parseImportPattern(patternInput);
    } catch (err) {
      resetPreviewState();
      error = err instanceof Error ? err.message : String(err);
      return;
    }
    const token = ++requestToken;
    loading = true;
    error = null;
    resetPreviewState();
    try {
      const resp = await previewRepos(parsed.owner, parsed.pattern);
      if (token !== requestToken) return;
      rows = resp.repos;
      selected = new Set(resp.repos.filter((row) => !row.already_configured).map(rowKey));
    } catch (err) {
      if (token !== requestToken) return;
      resetPreviewState();
      error = err instanceof Error ? err.message : String(err);
    } finally {
      if (token === requestToken) loading = false;
    }
  }

  async function handleSubmit(): Promise<void> {
    if (submitRows.length === 0) return;
    submitting = true;
    error = null;
    try {
      const settings = await bulkAddRepos(submitRows.map((row) => ({ owner: row.owner, name: row.name })));
      onImported(settings);
      onClose();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      submitting = false;
    }
  }

  function toggleSort(field: SortState["field"]): void {
    sort = sort.field === field
      ? { field, direction: sort.direction === "asc" ? "desc" : "asc" }
      : { field, direction: field === "pushed_at" ? "desc" : "asc" };
  }

  function toggleRow(row: RepoPreviewRow, checked: boolean, shiftKey: boolean): void {
    const key = rowKey(row);
    if (shiftKey) {
      const result = applyRangeSelection({ selected, visibleRows, anchorKey, clickedKey: key, checked });
      selected = result.selected;
      anchorKey = result.anchorKey;
      return;
    }
    const next = new Set(selected);
    if (checked) next.add(key);
    else next.delete(key);
    selected = next;
    anchorKey = key;
  }

  function closeIfAllowed(): void {
    if (!submitting) onClose();
  }

  function handleKeydown(event: KeyboardEvent): void {
    if (event.key === "Escape") {
      closeIfAllowed();
      return;
    }
    if (event.key !== "Tab") return;
    const container = event.currentTarget;
    if (!(container instanceof HTMLElement)) return;
    const modal = container.querySelector("[role='dialog']");
    if (!(modal instanceof HTMLElement)) return;
    const focusable = Array.from(
      modal.querySelectorAll<HTMLElement>(
        "button:not(:disabled), input:not(:disabled), select:not(:disabled), [tabindex]:not([tabindex='-1'])",
      ),
    );
    if (focusable.length === 0) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (!first || !last) return;
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }
</script>

{#if open}
  <div class="modal-backdrop" role="presentation" onkeydown={handleKeydown}>
    <div class="modal" role="dialog" aria-modal="true" aria-labelledby="repo-import-title">
      <header class="modal-header">
        <div>
          <h2 id="repo-import-title">Add repositories</h2>
          <p>Preview repositories before adding them.</p>
        </div>
        <button type="button" class="close-btn" aria-label="Close" onclick={closeIfAllowed}>×</button>
      </header>

      <div class="preview-form">
        <label>
          <span>Repository pattern</span>
          <input
            bind:this={inputEl}
            value={patternInput}
            placeholder="owner/pattern"
            oninput={(event) => handlePatternInput(event.currentTarget.value)}
            onkeydown={(event) => { if (event.key === "Enter" && !loading) void handlePreview(); }}
          />
        </label>
        <button class="preview-btn" type="button" onclick={() => void handlePreview()} disabled={loading || !patternInput.trim()}>
          {loading ? "Previewing…" : "Preview"}
        </button>
      </div>

      {#if error}
        <div class="error-msg" role="alert">{error}</div>
      {/if}

      {#if rows.length > 0}
        <RepoPreviewTable
          rows={visibleRows}
          {selected}
          {filterText}
          {statusFilter}
          {hideForks}
          {hidePrivate}
          {sort}
          onFilterText={(value) => { filterText = value; }}
          onStatusFilter={(value) => { statusFilter = value; }}
          onHideForks={(value) => { hideForks = value; }}
          onHidePrivate={(value) => { hidePrivate = value; }}
          onSort={toggleSort}
          onToggle={toggleRow}
          onSelectVisible={() => { selected = setAllVisible(selected, visibleRows, true); }}
          onDeselectVisible={() => { selected = setAllVisible(selected, visibleRows, false); }}
        />
      {:else if !loading && !error}
        <div class="empty-preview">Preview repositories before adding them.</div>
      {/if}

      <footer class="modal-footer">
        <span>Selected {selectedCount} of {selectableVisibleCount}</span>
        <div class="footer-actions">
          <button class="secondary-btn" type="button" onclick={closeIfAllowed} disabled={submitting}>Cancel</button>
          <button class="submit-btn" type="button" onclick={() => void handleSubmit()} disabled={submitting || selectedCount === 0}>
            {submitting ? "Adding…" : "Add selected repositories"}
          </button>
        </div>
      </footer>
    </div>
  </div>
{/if}

<style>
  .modal-backdrop { position: fixed; inset: 0; z-index: 40; display: flex; align-items: center; justify-content: center; padding: 24px; background: color-mix(in srgb, black 38%, transparent); }
  .modal { width: min(1040px, 100%); max-height: min(760px, 92vh); display: flex; flex-direction: column; gap: 14px; background: var(--bg-surface); color: var(--text-primary); border: 1px solid var(--border-default); border-radius: var(--radius-lg); box-shadow: 0 24px 80px rgb(0 0 0 / 35%); padding: 18px; }
  .modal-header, .modal-footer { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
  h2 { margin: 0; font-size: 16px; }
  p { margin: 4px 0 0; color: var(--text-muted); font-size: 12px; }
  .close-btn { color: var(--text-muted); font-size: 20px; }
  .preview-form { display: flex; gap: 10px; align-items: end; }
  label { flex: 1; display: flex; flex-direction: column; gap: 6px; font-size: 12px; color: var(--text-secondary); }
  input { font-size: 13px; padding: 7px 10px; color: var(--text-primary); background: var(--bg-inset); border: 1px solid var(--border-muted); border-radius: var(--radius-sm); }
  .preview-btn, .submit-btn { padding: 7px 14px; font-size: 13px; font-weight: 600; color: white; background: var(--accent-blue); border-radius: var(--radius-sm); }
  .secondary-btn { padding: 7px 14px; font-size: 13px; color: var(--text-secondary); background: var(--bg-inset); border: 1px solid var(--border-muted); border-radius: var(--radius-sm); }
  button:disabled { opacity: 0.5; cursor: not-allowed; }
  .error-msg { color: var(--accent-red); font-size: 12px; }
  .empty-preview { border: 1px dashed var(--border-muted); border-radius: var(--radius-md); padding: 28px; color: var(--text-muted); text-align: center; font-size: 13px; }
  .footer-actions { display: flex; gap: 8px; }
</style>
