<script lang="ts">
  import { onMount } from "svelte";
  import type { Editor as CoreEditor } from "@tiptap/core";
  import Document from "@tiptap/extension-document";
  import HardBreak from "@tiptap/extension-hard-break";
  import Paragraph from "@tiptap/extension-paragraph";
  import Placeholder from "@tiptap/extension-placeholder";
  import Text from "@tiptap/extension-text";
  import { Editor, EditorContent } from "svelte-tiptap";

  import { getClient } from "../../context.js";

  interface Props {
    owner: string;
    name: string;
    value: string;
    disabled?: boolean;
    placeholder?: string;
    oninput: (value: string) => void;
    onsubmit: () => void;
  }

  interface ActiveToken {
    trigger: "@" | "#";
    from: number;
    to: number;
    query: string;
  }

  interface SuggestionItem {
    id: string;
    label: string;
    detail: string;
    insertText: string;
  }

  interface CommentAutocompleteReference {
    kind: string;
    number: number;
    title: string;
    state: string;
  }

  interface CommentAutocompleteResponse {
    users?: string[] | null;
    references?: CommentAutocompleteReference[] | null;
  }

  const client = getClient();

  let {
    owner,
    name,
    value,
    disabled = false,
    placeholder = "Write a comment... (Cmd+Enter to submit)",
    oninput,
    onsubmit,
  }: Props = $props();

  let editor = $state<Editor | null>(null);
  let menuOpen = $state(false);
  let suggestions = $state<SuggestionItem[]>([]);
  let highlightedIndex = $state(0);
  let activeToken = $state<ActiveToken | null>(null);
  let syncingFromProps = false;
  let requestSequence = 0;

  function escapeHTML(text: string): string {
    return text
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#39;");
  }

  function htmlFromPlainText(text: string): string {
    if (text === "") return "<p></p>";
    return `<p>${escapeHTML(text).replaceAll("\n", "<br>")}</p>`;
  }

  function plainTextFromEditor(current: CoreEditor): string {
    return current.getText({ blockSeparator: "\n" });
  }

  function closeMenu(): void {
    requestSequence += 1;
    menuOpen = false;
    suggestions = [];
    highlightedIndex = 0;
    activeToken = null;
  }

  function detectActiveToken(current: CoreEditor): ActiveToken | null {
    const { from, to } = current.state.selection;
    if (from !== to) return null;

    const prefix = current.state.doc.textBetween(0, from, "\n", "\n");
    const match = prefix.match(/(?:^|[\s([])([@#])([A-Za-z0-9_-]*)$/);
    if (!match) return null;

    const trigger = match[1] as "@" | "#";
    const query = match[2] ?? "";
    const tokenLength = 1 + query.length;

    return {
      trigger,
      query,
      from: Math.max(1, from - tokenLength),
      to: from,
    };
  }

  function toSuggestionItems(
    trigger: "@" | "#",
    response: CommentAutocompleteResponse,
  ): SuggestionItem[] {
    if (trigger === "@") {
      return (response.users ?? []).map((login: string) => ({
        id: `user:${login}`,
        label: `@${login}`,
        detail: "User",
        insertText: `@${login}`,
      }));
    }

    return (response.references ?? []).map((reference: CommentAutocompleteReference) => ({
      id: `${reference.kind}:${reference.number}`,
      label: `#${reference.number}`,
      detail: `${reference.kind === "pull" ? "PR" : "Issue"} · ${reference.title}`,
      insertText: `#${reference.number}`,
    }));
  }

  async function refreshSuggestions(): Promise<void> {
    if (!editor || disabled) {
      closeMenu();
      return;
    }

    const token = detectActiveToken(editor);
    activeToken = token;
    if (token === null) {
      closeMenu();
      return;
    }

    const sequence = ++requestSequence;
    const { data, error } = await client.GET(
      "/repos/{owner}/{name}/comment-autocomplete",
      {
        params: {
          path: { owner, name },
          query: {
            trigger: token.trigger,
            q: token.query,
            limit: 8,
          },
        },
      },
    );

    if (sequence !== requestSequence) return;
    if (error || data === undefined) {
      suggestions = [];
      menuOpen = false;
      return;
    }

    suggestions = toSuggestionItems(token.trigger, data);
    highlightedIndex = 0;
    menuOpen = suggestions.length > 0;
  }

  function acceptSuggestion(item: SuggestionItem): void {
    if (!editor || activeToken === null) return;

    editor
      .chain()
      .focus()
      .insertContentAt(
        { from: activeToken.from, to: activeToken.to },
        `${item.insertText} `,
      )
      .run();

    closeMenu();
  }

  function acceptHighlightedSuggestion(): boolean {
    const item = suggestions[highlightedIndex];
    if (!menuOpen || !item) return false;
    acceptSuggestion(item);
    return true;
  }

  function moveHighlight(delta: number): void {
    if (!menuOpen || suggestions.length === 0) return;
    highlightedIndex = (highlightedIndex + delta + suggestions.length) % suggestions.length;
  }

  function handleEditorKeydown(event: KeyboardEvent): boolean {
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      onsubmit();
      return true;
    }

    if (menuOpen) {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        moveHighlight(1);
        return true;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        moveHighlight(-1);
        return true;
      }
      if (event.key === "Enter" || event.key === "Tab") {
        event.preventDefault();
        if (acceptHighlightedSuggestion()) return true;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        closeMenu();
        return true;
      }
    }

    if (event.key === "Enter") {
      event.preventDefault();
      editor?.commands.setHardBreak();
      return true;
    }

    return false;
  }

  onMount(() => {
    const current = new Editor({
      extensions: [
        Document,
        Paragraph,
        Text,
        HardBreak,
        Placeholder.configure({
          placeholder,
          emptyEditorClass: "is-editor-empty",
        }),
      ],
      content: htmlFromPlainText(value),
      editable: !disabled,
      editorProps: {
        attributes: {
          class: "comment-editor-input",
          role: "textbox",
          "aria-label": placeholder,
          "aria-multiline": "true",
          "data-placeholder": placeholder,
          spellcheck: "true",
        },
        handleDOMEvents: {
          focus: () => {
            void refreshSuggestions();
            return false;
          },
        },
        handleKeyDown: (_view, event) => handleEditorKeydown(event),
      },
      onUpdate: ({ editor: updated }) => {
        const nextValue = plainTextFromEditor(updated);
        if (syncingFromProps) {
          syncingFromProps = false;
          return;
        }
        if (nextValue === value) return;
        if (!updated.isFocused) return;
        oninput(nextValue);
        void refreshSuggestions();
      },
      onSelectionUpdate: () => {
        void refreshSuggestions();
      },
    });

    editor = current;

    return () => {
      current.destroy();
      editor = null;
    };
  });

  $effect(() => {
    if (!editor) return;
    if (editor.isEditable !== !disabled) {
      editor.setEditable(!disabled);
    }
    if (disabled) closeMenu();
  });

  $effect(() => {
    if (!editor) return;
    const currentValue = plainTextFromEditor(editor);
    if (currentValue === value) return;
    syncingFromProps = true;
    editor.commands.setContent(htmlFromPlainText(value));
  });
</script>

<div class="comment-editor">
  {#if editor}
    <EditorContent {editor} />
  {/if}

  {#if menuOpen}
    <ul class="comment-editor-menu" role="listbox" aria-label="Comment autocomplete suggestions">
      {#each suggestions as item, index (item.id)}
        <li>
          <button
            type="button"
            class="comment-editor-option"
            class:is-highlighted={index === highlightedIndex}
            role="option"
            aria-selected={index === highlightedIndex}
            onclick={() => acceptSuggestion(item)}
            onmouseenter={() => {
              highlightedIndex = index;
            }}
          >
            <span class="comment-editor-option-label">{item.label}</span>
            <span class="comment-editor-option-detail">{item.detail}</span>
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .comment-editor {
    position: relative;
  }

  :global(.comment-editor-input) {
    width: 100%;
    min-height: 80px;
    max-height: 200px;
    overflow-y: auto;
    resize: vertical;
    font-size: 13px;
    line-height: 1.5;
    background: var(--bg-surface);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    padding: 8px 10px;
    color: var(--text-primary);
    outline: none;
    white-space: pre-wrap;
    word-break: break-word;
  }

  :global(.comment-editor-input:focus) {
    border-color: var(--accent-blue);
  }

  :global(.comment-editor-input p) {
    margin: 0;
  }

  :global(.comment-editor-input.is-editor-empty:first-child::before) {
    content: attr(data-placeholder);
    color: var(--text-muted);
    pointer-events: none;
    float: left;
    height: 0;
  }

  .comment-editor-menu {
    position: absolute;
    left: 0;
    right: 0;
    bottom: calc(100% + 6px);
    max-height: 220px;
    overflow-y: auto;
    list-style: none;
    margin: 0;
    padding: 4px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    background: var(--bg-surface);
    box-shadow: var(--shadow-md);
    z-index: 20;
  }

  .comment-editor-option {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 6px 8px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 12px;
    width: 100%;
    border: 0;
    background: transparent;
    text-align: left;
  }

  .comment-editor-option.is-highlighted {
    background: var(--bg-surface-hover);
  }

  .comment-editor-option-label {
    color: var(--text-primary);
    font-weight: 600;
  }

  .comment-editor-option-detail {
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
