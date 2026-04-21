import type { StoreInstances } from "@middleman/ui";
import type { Settings } from "@middleman/ui/api/types";

export interface AppStartupDeps {
  getSettings: () => Promise<Settings>;
  getStores: () => StoreInstances | undefined;
  onReady: () => void;
}

/**
 * runAppStartup kicks off the async initialization work App.svelte
 * performs during onMount: fetching settings, hydrating store
 * defaults, and wiring live-update subscriptions once both have
 * resolved.
 *
 * It returns a cancel function that must be called from the
 * component's cleanup path. If cancellation fires before the
 * settings fetch resolves, no post-await side effects run, so
 * the component cannot leak an EventSource or start polling
 * after it has already unmounted.
 */
export function runAppStartup(deps: AppStartupDeps): () => void {
  let cancelled = false;
  void (async () => {
    try {
      const settings = await deps.getSettings();
      if (cancelled) return;
      const stores = deps.getStores();
      if (stores) {
        stores.settings.setConfiguredRepos(settings.repos);
        stores.settings.setTerminalFontFamily(
          settings.terminal.font_family,
        );
        stores.activity.hydrateDefaults(settings.activity);
      }
    } catch (err) {
      console.warn(
        "Failed to load settings, using defaults:",
        err,
      );
    }
    if (cancelled) return;
    deps.onReady();
    const stores = deps.getStores();
    if (stores) {
      stores.sync.startPolling();
      void stores.pulls.loadPulls();
      void stores.issues.loadIssues();
      stores.events.connect();
    }
  })();
  return () => {
    cancelled = true;
  };
}
