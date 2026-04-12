<script lang="ts">
  import { getStores } from "../../context.js";
  import CommentEditor from "./CommentEditor.svelte";
  import {
    beginCommentSubmit,
    clearCommentSubmitError,
    clearCommentDraft,
    finishCommentSubmit,
    getCommentDraft,
    getCommentDraftByKey,
    getCommentDraftKey,
    getCommentSubmitError,
    isCommentSubmitPending,
    setCommentSubmitError,
    setCommentDraft,
  } from "./comment-drafts.svelte.js";

  const { detail } = getStores();

  interface Props {
    owner: string;
    name: string;
    number: number;
  }

  const { owner, name, number }: Props = $props();

  const currentDraftKey = $derived(
    getCommentDraftKey("pull", owner, name, number),
  );
  const body = $derived(getCommentDraftByKey(currentDraftKey));

  const isEmpty = $derived(body.trim() === "");
  const visibleError = $derived(
    getCommentSubmitError("pull", owner, name, number),
  );
  const isPostingCurrent = $derived(
    isCommentSubmitPending("pull", owner, name, number),
  );

  async function handleSubmit(): Promise<void> {
    if (isEmpty || isPostingCurrent) return;
    const submittedOwner = owner;
    const submittedName = name;
    const submittedNumber = number;
    const submittedBody = body.trim();
    beginCommentSubmit("pull", submittedOwner, submittedName, submittedNumber);
    clearCommentSubmitError("pull", submittedOwner, submittedName, submittedNumber);
    try {
      await detail.submitComment(
        submittedOwner,
        submittedName,
        submittedNumber,
        submittedBody,
      );
      const storeError = detail.getDetailError();
      if (storeError !== null) {
        setCommentSubmitError(
          "pull",
          submittedOwner,
          submittedName,
          submittedNumber,
          storeError,
        );
      } else {
        clearCommentDraft(
          "pull",
          submittedOwner,
          submittedName,
          submittedNumber,
        );
        clearCommentSubmitError(
          "pull",
          submittedOwner,
          submittedName,
          submittedNumber,
        );
      }
    } finally {
      finishCommentSubmit(
        "pull",
        submittedOwner,
        submittedName,
        submittedNumber,
      );
    }
  }
</script>

<div class="comment-box">
  {#key `pull:${owner}/${name}/${number}`}
    <CommentEditor
      {owner}
      {name}
      value={body}
      disabled={isPostingCurrent}
      oninput={(nextBody) => {
        setCommentDraft("pull", owner, name, number, nextBody);
      }}
      onsubmit={() => {
        void handleSubmit();
      }}
    />
  {/key}
  {#if visibleError !== null}
    <p class="error-msg">{visibleError}</p>
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
