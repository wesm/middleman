<script lang="ts">
  import { getStores } from "../../context.js";
  import RepoTreePicker from "./RepoTreePicker.svelte";

  interface Props {
    onHelpClick?: () => void;
    disabled?: boolean;
  }
  let { onHelpClick, disabled = false }: Props = $props();

  const stores = getStores();
  const jobsStore = stores.roborevJobs;

  const statusOptions = [
    { value: "", label: "All statuses" },
    { value: "queued", label: "Queued" },
    { value: "running", label: "Running" },
    { value: "done", label: "Done" },
    { value: "failed", label: "Failed" },
    { value: "canceled", label: "Canceled" },
  ];

  let searchValue = $state(
    jobsStore?.getFilterSearch() ?? "",
  );
  let searchTimeout: ReturnType<typeof setTimeout> | undefined;

  function onStatusChange(
    e: Event & { currentTarget: HTMLSelectElement },
  ): void {
    const val = e.currentTarget.value || undefined;
    jobsStore?.setFilter("status", val);
  }

  function onSearchInput(
    e: Event & { currentTarget: HTMLInputElement },
  ): void {
    searchValue = e.currentTarget.value;
    clearTimeout(searchTimeout);
    searchTimeout = setTimeout(() => {
      jobsStore?.setFilter(
        "search",
        searchValue || undefined,
      );
    }, 300);
  }

  function onHideClosedChange(
    e: Event & { currentTarget: HTMLInputElement },
  ): void {
    jobsStore?.setFilter(
      "hideClosed",
      e.currentTarget.checked,
    );
  }
</script>

<div class="filter-bar">
  <div class:filter-disabled={disabled}>
    <RepoTreePicker />
  </div>

  <select
    class="status-select"
    value={jobsStore?.getFilterStatus() ?? ""}
    onchange={onStatusChange}
    {disabled}
  >
    {#each statusOptions as opt (opt.value)}
      <option value={opt.value}>{opt.label}</option>
    {/each}
  </select>

  <input
    class="search-input"
    type="text"
    placeholder="Search by ref..."
    value={searchValue}
    oninput={onSearchInput}
    {disabled}
  />

  <label class="hide-closed">
    <input
      type="checkbox"
      checked={jobsStore?.getFilterHideClosed() ?? false}
      onchange={onHideClosedChange}
      {disabled}
    />
    Hide closed
  </label>

  <button
    class="help-btn"
    title="Keyboard shortcuts"
    onclick={onHelpClick}
  >
    ?
  </button>
</div>

<style>
  .filter-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border-muted);
    background: var(--bg-surface);
    flex-shrink: 0;
    flex-wrap: wrap;
  }

  .status-select {
    padding: 4px 8px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    background: var(--bg-surface);
    color: var(--text-primary);
    font-size: 12px;
    cursor: pointer;
    outline: none;
  }

  .status-select:hover {
    background: var(--bg-surface-hover);
  }

  .search-input {
    padding: 4px 8px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    background: var(--bg-surface);
    color: var(--text-primary);
    font-size: 12px;
    outline: none;
    min-width: 140px;
    flex: 1;
    max-width: 220px;
  }

  .search-input::placeholder {
    color: var(--text-muted);
  }

  .search-input:focus {
    border-color: var(--accent-blue);
  }

  .hide-closed {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    color: var(--text-secondary);
    cursor: pointer;
    white-space: nowrap;
    user-select: none;
  }

  .hide-closed input {
    cursor: pointer;
  }

  .help-btn {
    width: 24px;
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: center;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    background: var(--bg-surface);
    color: var(--text-muted);
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
    flex-shrink: 0;
    margin-left: auto;
  }

  .help-btn:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .filter-disabled {
    pointer-events: none;
    opacity: 0.5;
  }
</style>
