<script lang="ts">
  import { ActionButton } from "@middleman/ui";

  interface Props {
    title: string;
    body: string;
    error?: string | null;
    submitting?: boolean;
    ontitlechange: (value: string) => void;
    onbodychange: (value: string) => void;
    oncancel: () => void;
    onsubmitissue: () => void;
  }

  let {
    title,
    body,
    error = null,
    submitting = false,
    ontitlechange,
    onbodychange,
    oncancel,
    onsubmitissue,
  }: Props = $props();
</script>

<form
  class="issue-composer"
  onsubmit={(event) => {
    event.preventDefault();
    onsubmitissue();
  }}
>
  <div class="issue-composer__head">
    <h2>Create issue</h2>
    <ActionButton size="sm" onclick={oncancel}>Cancel</ActionButton>
  </div>

  <input
    class="issue-composer__input"
    type="text"
    placeholder="Issue title"
    value={title}
    oninput={(event) =>
      ontitlechange(event.currentTarget.value)}
  />
  <textarea
    class="issue-composer__textarea"
    rows="4"
    placeholder="Describe the problem, context, or follow-up work"
    value={body}
    oninput={(event) =>
      onbodychange(event.currentTarget.value)}
  ></textarea>

  {#if error}
    <p class="issue-composer__error">{error}</p>
  {/if}

  <div class="issue-composer__actions">
    <ActionButton
      type="submit"
      tone="info"
      surface="soft"
      disabled={submitting}
    >
      {submitting ? "Creating..." : "Create issue"}
    </ActionButton>
  </div>
</form>

<style>
  .issue-composer {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px;
    border-top: 1px solid var(--border-muted);
    background: var(--bg-inset);
  }

  .issue-composer__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .issue-composer__head h2 {
    color: var(--text-primary);
    font-size: 13px;
    font-weight: 600;
  }

  .issue-composer__input,
  .issue-composer__textarea {
    width: 100%;
    background: var(--bg-surface);
  }

  .issue-composer__textarea {
    min-height: 96px;
    resize: vertical;
  }

  .issue-composer__error {
    color: var(--accent-red);
    font-size: 12px;
  }

  .issue-composer__actions {
    display: flex;
    justify-content: flex-end;
  }
</style>
