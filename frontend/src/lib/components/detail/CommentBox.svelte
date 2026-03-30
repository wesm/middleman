<script lang="ts">
  import { submitComment, getDetailError, isDetailLoading } from "../../stores/detail.svelte.js";

  interface Props {
    owner: string;
    name: string;
    number: number;
  }

  const { owner, name, number }: Props = $props();

  let body = $state("");
  let posting = $state(false);
  let localError = $state<string | null>(null);

  const isEmpty = $derived(body.trim() === "");

  async function handleSubmit(): Promise<void> {
    if (isEmpty || posting) return;
    posting = true;
    localError = null;
    await submitComment(owner, name, number, body.trim());
    posting = false;
    const storeError = getDetailError();
    if (storeError !== null) {
      localError = storeError;
    } else {
      body = "";
    }
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      void handleSubmit();
    }
  }
</script>

<div class="comment-box">
  <textarea
    class="comment-textarea"
    placeholder="Write a comment... (Cmd+Enter to submit)"
    bind:value={body}
    onkeydown={handleKeydown}
    disabled={posting}
    rows={4}
  ></textarea>
  {#if localError !== null}
    <p class="error-msg">{localError}</p>
  {/if}
  <div class="comment-actions">
    <button
      class="submit-btn"
      onclick={() => void handleSubmit()}
      disabled={isEmpty || posting}
    >
      {posting ? "Posting…" : "Comment"}
    </button>
  </div>
</div>

<style>
  .comment-box {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .comment-textarea {
    width: 100%;
    resize: vertical;
    font-size: 13px;
    line-height: 1.5;
    background: var(--bg-surface);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    padding: 8px 10px;
    color: var(--text-primary);
    outline: none;
    min-height: 80px;
    max-height: 200px;
  }

  .comment-textarea:focus {
    border-color: var(--accent-blue);
  }

  .comment-textarea:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .error-msg {
    font-size: 12px;
    color: var(--accent-red);
  }

  .comment-actions {
    display: flex;
    justify-content: flex-end;
  }

  .submit-btn {
    font-size: 13px;
    font-weight: 500;
    padding: 6px 14px;
    background: var(--accent-blue);
    color: #fff;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: opacity 0.15s;
  }

  .submit-btn:hover:not(:disabled) {
    opacity: 0.85;
  }

  .submit-btn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
</style>
