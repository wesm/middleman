<script lang="ts">
  import { client } from "../../api/runtime.js";

  interface Props {
    owner: string;
    name: string;
    number: number;
    prTitle: string;
    prBody: string;
    prAuthor: string;
    prAuthorDisplayName: string;
    allowSquash: boolean;
    allowMerge: boolean;
    allowRebase: boolean;
    onclose: () => void;
    onmerged: () => void;
  }

  const {
    owner, name, number, prTitle, prBody,
    prAuthor, prAuthorDisplayName,
    allowSquash, allowMerge, allowRebase,
    onclose, onmerged,
  }: Props = $props();

  type Method = "merge" | "squash" | "rebase";
  type MethodOption = { value: Method; label: string };
  type MergeParams = {
    commit_title: string;
    commit_message: string;
    method: Method;
  };

  // Props are stable for the lifetime of this modal, so we
  // intentionally capture their initial values for editable fields.
  const methods: MethodOption[] = $derived.by(() => {
    const out: MethodOption[] = [];
    if (allowSquash) {
      out.push({ value: "squash", label: "Squash and merge" });
    }
    if (allowMerge) {
      out.push({
        value: "merge",
        label: "Create a merge commit",
      });
    }
    if (allowRebase) {
      out.push({ value: "rebase", label: "Rebase and merge" });
    }
    return out;
  });

  const coAuthorName = $derived(prAuthorDisplayName || prAuthor);
  const coAuthor = $derived(
    `Co-authored-by: ${coAuthorName} <${prAuthor}@users.noreply.github.com>`
  );

  let selectedMethod = $state<Method>("squash");
  let commitTitle = $state("");
  let commitMessage = $state("");
  let initialized = $state(false);

  // Seed editable fields once on first render.
  $effect(() => {
    if (initialized) return;
    selectedMethod = methods[0]?.value ?? "squash";
    commitTitle = `${prTitle} (#${number})`;
    commitMessage = prBody
      ? `${prBody}\n\n${coAuthor}`
      : coAuthor;
    initialized = true;
  });

  let merging = $state(false);
  let error = $state<string | null>(null);

  async function handleMerge(): Promise<void> {
    merging = true;
    error = null;
    try {
      const params: MergeParams = {
        commit_title: commitTitle,
        commit_message: commitMessage,
        method: selectedMethod,
      };
      const { error } = await client.POST("/repos/{owner}/{name}/pulls/{number}/merge", {
        params: { path: { owner, name, number } },
        body: params,
      });
      if (error) {
        throw new Error(error.detail ?? error.title ?? "failed to merge pull request");
      }
      onmerged();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      merging = false;
    }
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === "Escape") {
      e.preventDefault();
      onclose();
    }
  }

  function methodLabel(): string {
    return (
      methods.find(m => m.value === selectedMethod)?.label
      ?? "Merge"
    );
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="modal-overlay"
  onclick={onclose}
  onkeydown={handleKeydown}
>
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal" onclick={(e) => e.stopPropagation()}>
    <div class="modal-header">
      <h3 class="modal-title">Merge Pull Request</h3>
      <button
        class="modal-close"
        onclick={onclose}
        title="Cancel (Esc)"
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 16 16"
          fill="currentColor"
        >
          <path d="M3.72 3.72a.75.75 0 011.06 0L8 6.94l3.22-3.22a.75.75 0 111.06 1.06L9.06 8l3.22 3.22a.75.75 0 11-1.06 1.06L8 9.06l-3.22 3.22a.75.75 0 01-1.06-1.06L6.94 8 3.72 4.78a.75.75 0 010-1.06z"/>
        </svg>
      </button>
    </div>

    <div class="modal-body">
      {#if methods.length > 1}
        <div class="field" role="group" aria-label="Merge method">
          <span class="field-label">Merge method</span>
          <div class="method-options">
            {#each methods as m}
              <label
                class="method-option"
                class:method-option--active={selectedMethod === m.value}
              >
                <input
                  type="radio"
                  name="merge-method"
                  value={m.value}
                  bind:group={selectedMethod}
                />
                {m.label}
              </label>
            {/each}
          </div>
        </div>
      {/if}

      <div class="field">
        <label class="field-label" for="commit-title">
          Commit title
        </label>
        <input
          id="commit-title"
          class="field-input"
          type="text"
          bind:value={commitTitle}
        />
      </div>

      <div class="field">
        <label class="field-label" for="commit-message">
          Commit message
        </label>
        <textarea
          id="commit-message"
          class="field-textarea"
          bind:value={commitMessage}
          rows={8}
        ></textarea>
      </div>

      {#if error}
        <p class="merge-error">{error}</p>
      {/if}
    </div>

    <div class="modal-footer">
      <button
        class="btn btn--secondary"
        onclick={onclose}
        disabled={merging}
      >
        Cancel
      </button>
      <button
        class="btn btn--primary btn--green"
        onclick={() => void handleMerge()}
        disabled={merging}
      >
        {merging ? "Merging\u2026" : methodLabel()}
      </button>
    </div>
  </div>
</div>

<style>
  .modal-overlay {
    position: fixed;
    inset: 0;
    background: var(--overlay-bg);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 50;
    animation: fade-in 0.12s ease-out;
  }

  @keyframes fade-in {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  .modal {
    width: min(560px, 92vw);
    background: var(--bg-surface);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    display: flex;
    flex-direction: column;
    animation: scale-in 0.12s ease-out;
  }

  @keyframes scale-in {
    from { opacity: 0; transform: scale(0.96); }
    to { opacity: 1; transform: scale(1); }
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 16px;
    border-bottom: 1px solid var(--border-muted);
  }

  .modal-title {
    font-size: 14px;
    font-weight: 600;
  }

  .modal-close {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    transition: background 0.1s, color 0.1s;
  }
  .modal-close:hover {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .modal-body {
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 14px;
    max-height: 60vh;
    overflow-y: auto;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .field-label {
    font-size: 12px;
    font-weight: 500;
    color: var(--text-secondary);
  }

  .field-input {
    font-size: 13px;
    padding: 6px 10px;
    background: var(--bg-inset);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    color: var(--text-primary);
  }
  .field-input:focus {
    border-color: var(--accent-blue);
    outline: none;
  }

  .field-textarea {
    font-size: 13px;
    padding: 8px 10px;
    background: var(--bg-inset);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    color: var(--text-primary);
    resize: vertical;
    line-height: 1.5;
    font-family: var(--font-mono);
    max-height: 300px;
  }
  .field-textarea:focus {
    border-color: var(--accent-blue);
    outline: none;
  }

  .method-options {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }

  .method-option {
    font-size: 12px;
    font-weight: 500;
    padding: 5px 12px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-default);
    background: var(--bg-inset);
    color: var(--text-secondary);
    cursor: pointer;
    transition: all 0.1s;
  }
  .method-option input { display: none; }
  .method-option:hover {
    border-color: var(--accent-blue);
    color: var(--text-primary);
  }
  .method-option--active {
    background: color-mix(
      in srgb, var(--accent-blue) 12%, transparent
    );
    border-color: var(--accent-blue);
    color: var(--accent-blue);
  }

  .merge-error {
    font-size: 12px;
    color: var(--accent-red);
    padding: 8px 10px;
    background: color-mix(
      in srgb, var(--accent-red) 8%, transparent
    );
    border-radius: var(--radius-sm);
  }

  .modal-footer {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 16px;
    border-top: 1px solid var(--border-muted);
  }

  .btn {
    font-size: 13px;
    font-weight: 500;
    padding: 6px 16px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: opacity 0.1s, background 0.1s;
  }
  .btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .btn--secondary {
    background: var(--bg-inset);
    color: var(--text-secondary);
    border: 1px solid var(--border-default);
  }
  .btn--secondary:hover:not(:disabled) {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .btn--primary {
    color: #e6ffe6;
    border: none;
  }

  .btn--green {
    background: #1a7f37;
    color: #e6ffe6;
  }
  .btn--green:hover:not(:disabled) {
    background: #176b2e;
  }
</style>
