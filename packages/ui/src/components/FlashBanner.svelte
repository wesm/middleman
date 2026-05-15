<script lang="ts">
  import { getFlashMessage, dismissFlash } from "../stores/flash.svelte.js";
</script>

{#if getFlashMessage()}
  <div class="flash-banner">
    <span class="flash-text">{getFlashMessage()}</span>
    <button class="flash-dismiss" onclick={dismissFlash} title="Dismiss">x</button>
  </div>
{/if}

<style>
  .flash-banner {
    position: fixed;
    top: 44px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 1000;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 16px;
    background: var(--bg-surface);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
    font-size: var(--font-size-md);
    color: var(--text-primary);
    max-width: 480px;
  }

  .flash-text {
    flex: 1;
  }

  .flash-dismiss {
    flex-shrink: 0;
    background: none;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    font-size: var(--font-size-lg);
    padding: 0 2px;
    line-height: 1;
  }

  .flash-dismiss:hover {
    color: var(--text-primary);
  }
</style>
