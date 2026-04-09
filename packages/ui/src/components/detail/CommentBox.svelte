<script lang="ts">
  import { getStores } from "../../context.js";
  import {
    clearCommentDraft,
    getCommentDraft,
    setCommentDraft,
  } from "./comment-drafts.js";

  const { detail } = getStores();

  interface Props {
    owner: string;
    name: string;
    number: number;
  }

  const { owner, name, number }: Props = $props();

  let currentDraftKey = $state("");
  let body = $state("");
  let postingDraftKey = $state<string | null>(null);
  let localError = $state<string | null>(null);
  let submitGeneration = 0;

  $effect(() => {
    const nextDraftKey = `pull:${owner}/${name}/${number}`;
    if (nextDraftKey === currentDraftKey) return;
    currentDraftKey = nextDraftKey;
    body = getCommentDraft("pull", owner, name, number);
    localError = null;
  });

  const isEmpty = $derived(body.trim() === "");
  const isPostingCurrent = $derived(
    postingDraftKey === currentDraftKey,
  );

  function handleInput(e: Event): void {
    body = (e.currentTarget as HTMLTextAreaElement).value;
    setCommentDraft("pull", owner, name, number, body);
  }

  async function handleSubmit(): Promise<void> {
    if (isEmpty || isPostingCurrent) return;
    const submittedOwner = owner;
    const submittedName = name;
    const submittedNumber = number;
    const submittedDraftKey = currentDraftKey;
    const submittedBody = body.trim();
    const submittedGeneration = ++submitGeneration;
    postingDraftKey = submittedDraftKey;
    localError = null;
    await detail.submitComment(
      submittedOwner,
      submittedName,
      submittedNumber,
      submittedBody,
    );
    const storeError = detail.getDetailError();
    if (storeError !== null) {
      if (
        currentDraftKey === submittedDraftKey &&
        submitGeneration === submittedGeneration
      ) {
        localError = storeError;
      }
    } else {
      clearCommentDraft(
        "pull",
        submittedOwner,
        submittedName,
        submittedNumber,
      );
      if (
        currentDraftKey === submittedDraftKey &&
        submitGeneration === submittedGeneration
      ) {
        body = "";
      }
    }
    if (
      postingDraftKey === submittedDraftKey &&
      submitGeneration === submittedGeneration
    ) {
      postingDraftKey = null;
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
    value={body}
    oninput={handleInput}
    onkeydown={handleKeydown}
    disabled={isPostingCurrent}
    rows={4}
  ></textarea>
  {#if localError !== null}
    <p class="error-msg">{localError}</p>
  {/if}
  <div class="comment-actions">
    <button
      class="submit-btn"
      onclick={() => void handleSubmit()}
      disabled={isEmpty || isPostingCurrent}
    >
      {isPostingCurrent ? "Posting…" : "Comment"}
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
