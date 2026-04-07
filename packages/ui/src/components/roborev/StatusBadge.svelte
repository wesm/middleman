<script lang="ts">
  interface Props {
    status: string;
  }
  let { status }: Props = $props();
</script>

<span class="status-badge status-{status}">
  <span class="dot"></span>
  {status}
</span>

<style>
  .status-badge {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 8px;
    border-radius: 999px;
    font-size: 11px;
    font-weight: 500;
    line-height: 1.4;
    white-space: nowrap;
  }

  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .status-queued {
    color: var(--review-queued);
    background: color-mix(
      in srgb, var(--review-queued) 12%, transparent
    );
  }
  .status-queued .dot { background: var(--review-queued); }

  .status-running {
    color: var(--review-running);
    background: color-mix(
      in srgb, var(--review-running) 12%, transparent
    );
  }
  .status-running .dot { background: var(--review-running); }

  .status-done {
    color: var(--review-done);
    background: color-mix(
      in srgb, var(--review-done) 12%, transparent
    );
  }
  .status-done .dot { background: var(--review-done); }

  .status-failed {
    color: var(--review-failed);
    background: color-mix(
      in srgb, var(--review-failed) 12%, transparent
    );
  }
  .status-failed .dot { background: var(--review-failed); }

  .status-canceled {
    color: var(--review-canceled);
    background: color-mix(
      in srgb, var(--review-canceled) 12%, transparent
    );
  }
  .status-canceled .dot { background: var(--review-canceled); }
</style>
