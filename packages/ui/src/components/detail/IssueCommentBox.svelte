<script lang="ts">
  import { getStores } from "../../context.js";
  import CommentEditor from "./CommentEditor.svelte";
  import {
    beginCommentSubmit,
    clearCommentSubmitError,
    clearCommentDraft,
    finishCommentSubmit,
    getCommentDraft,
    getCommentDraftKey,
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
    platformHost?: string | undefined;
    disabled?: boolean;
  }

  const {
    owner,
    name,
    number,
    platformHost,
    disabled = false,
  }: Props = $props();

  const currentDraftKey = $derived(
    getCommentDraftKey("issue", owner, name, number, platformHost),
  );
  const body = $derived(
    getCommentDraft("issue", owner, name, number, platformHost),
  );

  const isEmpty = $derived(body.trim() === "");
  const visibleError = $derived(
    getCommentSubmitError("issue", owner, name, number, platformHost),
  );
  const isPostingCurrent = $derived(
    isCommentSubmitPending("issue", owner, name, number, platformHost),
  );

  async function handleSubmit(): Promise<void> {
    if (isEmpty || isPostingCurrent || disabled) return;
    const submittedOwner = owner;
    const submittedName = name;
    const submittedNumber = number;
    const submittedBody = body.trim();
    const submittedPlatformHost = platformHost;
    beginCommentSubmit(
      "issue",
      submittedOwner,
      submittedName,
      submittedNumber,
      submittedPlatformHost,
    );
    clearCommentSubmitError(
      "issue",
      submittedOwner,
      submittedName,
      submittedNumber,
      submittedPlatformHost,
    );
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
          submittedPlatformHost,
        );
      } else {
        clearCommentDraft(
          "issue",
          submittedOwner,
          submittedName,
          submittedNumber,
          submittedPlatformHost,
        );
        clearCommentSubmitError(
          "issue",
          submittedOwner,
          submittedName,
          submittedNumber,
          submittedPlatformHost,
        );
      }
    } finally {
      finishCommentSubmit(
        "issue",
        submittedOwner,
        submittedName,
        submittedNumber,
        submittedPlatformHost,
      );
    }
  }
</script>

<div class="comment-box">
  {#key `issue:${owner}/${name}/${number}`}
    <div class="comment-editor-shell">
      <CommentEditor
        {owner}
        {name}
        {platformHost}
        value={body}
        disabled={isPostingCurrent || disabled}
        oninput={(nextBody) => {
          setCommentDraft("issue", owner, name, number, nextBody, platformHost);
        }}
        onsubmit={() => {
          void handleSubmit();
        }}
      />
      <button
        class="submit-btn"
        onclick={() => void handleSubmit()}
        disabled={isEmpty || isPostingCurrent || disabled}
      >
        {isPostingCurrent ? "Posting\u2026" : "Comment"}
      </button>
    </div>
  {/key}
  {#if visibleError !== null}
    <p class="error-msg">{visibleError}</p>
  {/if}
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

  .comment-editor-shell {
    position: relative;
  }

  .comment-editor-shell :global(.comment-editor-input) {
    min-height: 112px;
    max-height: 75dvh;
    padding-bottom: 46px;
  }

  .submit-btn {
    position: absolute;
    right: 8px;
    bottom: 8px;
    font-size: 13px;
    font-weight: 500;
    padding: 6px 14px;
    background: var(--accent-blue);
    color: #fff;
    border-radius: var(--radius-sm);
    cursor: pointer;
    z-index: 1;
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
