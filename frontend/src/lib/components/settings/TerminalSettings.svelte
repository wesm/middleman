<script lang="ts">
  import { onMount } from "svelte";
  import { getStores } from "@middleman/ui";
  import type { TerminalSettings as TerminalSettingsType } from "@middleman/ui/api/types";
  import { updateSettings } from "../../api/settings.js";

  import { isEmbedded } from "../../stores/embed-config.svelte.js";

  interface Props {
    terminal: TerminalSettingsType;
    onUpdate: (terminal: TerminalSettingsType) => void;
  }

  let { terminal, onUpdate }: Props = $props();

  const { settings: settingsStore } = getStores();
  const embedded = isEmbedded();

  let saving = $state(false);
  let draft = $state("");

  function normalizeFontFamily(value: string): string {
    return value.trim();
  }

  const currentFontFamily = $derived(terminal.font_family);
  const normalizedDraft = $derived(normalizeFontFamily(draft));
  const isDirty = $derived(
    normalizedDraft !== currentFontFamily,
  );
  const canSave = $derived(
    !saving && isDirty,
  );

  onMount(() => {
    draft = terminal.font_family;
  });

  async function save(): Promise<void> {
    if (embedded) return;
    draft = normalizedDraft;
    if (normalizedDraft === currentFontFamily) return;

    saving = true;
    try {
      const settings = await updateSettings({
        terminal: {
          font_family: normalizedDraft,
        },
      });
      draft = settings.terminal.font_family;
      onUpdate(settings.terminal);
      settingsStore.setTerminalFontFamily(
        settings.terminal.font_family,
      );
    } catch (err) {
      draft = currentFontFamily;
      console.warn("Failed to save terminal settings:", err);
    } finally {
      saving = false;
    }
  }

  function reset(): void {
    draft = "";
    void save();
  }

  function handleKeydown(event: KeyboardEvent): void {
    if (event.key === "Enter") {
      event.preventDefault();
      void save();
    } else if (event.key === "Escape") {
      draft = currentFontFamily;
    }
  }
</script>

<div class="terminal-settings">
  <label class="font-field" for="terminal-font-family">
    <span class="setting-label">Monospace font family</span>
    <input
      id="terminal-font-family"
      class="font-input"
      type="text"
      bind:value={draft}
      placeholder='"JetBrains Mono", "SF Mono", Menlo, Consolas, monospace'
      disabled={saving}
      onkeydown={handleKeydown}
    />
  </label>

  <div class="setting-actions">
    <p class="setting-help">
      Leave blank to use the app default monospace stack.
    </p>
    <div class="button-row">
      <button
        class="save-btn"
        type="button"
        disabled={!canSave}
        onclick={() => void save()}
      >
        {saving ? "Saving..." : "Save"}
      </button>
      <button
        class="reset-btn"
        type="button"
        disabled={saving || !currentFontFamily}
        onclick={reset}
      >
        Reset
      </button>
    </div>
  </div>
</div>

<style>
  .terminal-settings {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .font-field {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .setting-label {
    font-size: 13px;
    color: var(--text-secondary);
  }

  .font-input {
    width: 100%;
    font-family: var(--font-mono);
  }

  .setting-actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .button-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .setting-help {
    font-size: 12px;
    color: var(--text-muted);
  }

  .save-btn,
  .reset-btn {
    padding: 5px 10px;
    font-size: 12px;
    font-weight: 500;
    border-radius: var(--radius-sm);
    transition: background 0.12s, color 0.12s, opacity 0.12s,
      border-color 0.12s;
  }

  .save-btn {
    color: white;
    background: var(--accent-blue);
  }

  .save-btn:hover:not(:disabled) {
    opacity: 0.9;
  }

  .save-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .reset-btn {
    color: var(--text-secondary);
    border: 1px solid var(--border-muted);
  }

  .reset-btn:hover:not(:disabled) {
    background: var(--bg-surface-hover);
    color: var(--text-primary);
  }

  .reset-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
