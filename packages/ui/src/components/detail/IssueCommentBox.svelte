<script lang="ts">
  import { getStores } from "../../context.js";
  import {
    beginCommentSubmit,
    clearCommentSubmitError,
    clearCommentDraft,
    finishCommentSubmit,
    getCommentDraft,
    getCommentSubmitError,
    isCommentSubmitPending,
    setCommentSubmitError,
    setCommentDraft,
  } from "./comment-drafts.svelte.js";

  const { issues } = getStores();

  interface Props {
    owner: string;
    name: string;
    number: number;
  }

  const { owner, name, number }: Props = $props();

  const body = $derived(getCommentDraft("issue", owner, name, number));

  const isEmpty = $derived(body.trim() === "");
  const visibleError = $derived(
    getCommentSubmitError("issue", owner, name, number),
  );
  const isPostingCurrent = $derived(
    isCommentSubmitPending("issue", owner, name, number),
  );

  function handleInput(e: Event): void {
    setCommentDraft(
      "issue",
      owner,
      name,
      number,
      (e.currentTarget as HTMLTextAreaElement).value,
    );
  }

  async function handleSubmit(): Promise<void> {
    if (isEmpty || isPostingCurrent) return;
    const submittedOwner = owner;
    const submittedName = name;
    const submittedNumber = number;
    const submittedBody = body.trim();
    beginCommentSubmit("issue", submittedOwner, submittedName, submittedNumber);
    clearCommentSubmitError("issue", submittedOwner, submittedName, submittedNumber);
    try {
      await issues.submitIssueComment(
        submittedOwner,
        submittedName,
        submittedNumber,
        submittedBody,
      );
      const storeError = issues.getIssueDetailError();
      if (storeError !== null) {
        setCommentSubmitError(
          "issue",
          submittedOwner,
          submittedName,
          submittedNumber,
          storeError,
        );
      } else {
        clearCommentDraft(
          "issue",
          submittedOwner,
          submittedName,
          submittedNumber,
        );
        clearCommentSubmitError(
          "issue",
          submittedOwner,
          submittedName,
          submittedNumber,
        );
      }
    } finally {
      finishCommentSubmit(
        "issue",
        submittedOwner,
        submittedName,
        submittedNumber,
      );
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
  {#if visibleError !== null}
    <p class="error-msg">{visibleError}</p>
  {/if}
  <div class="comment-actions">
    <button
      class="submit-btn"
      onclick={() => void handleSubmit()}
      disabled={isEmpty || isPostingCurrent}
    >
      {isPostingCurrent ? "Posting\u2026" : "Comment"}
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
