import { setGlobalRepo } from "../stores/filter.svelte.js";

// Bridge: repo filter (module-scope, not workspace-specific)
window.__middleman_set_repo_filter = (
  repo: { owner: string; name: string } | null,
) => {
  setGlobalRepo(
    repo ? `${repo.owner}/${repo.name}` : undefined,
  );
};

export interface ActionHook {
  id: string;
  label: string;
  handler: (context: ActionContext) => void | Promise<void>;
}

export interface ActionContext {
  surface: string;
  owner: string;
  name: string;
  number: number;
  meta?: Record<string, unknown>;
}

interface UIDefaults {
  hideSync: boolean;
  hideRepoSelector: boolean;
  hideStar: boolean;
  sidebarCollapsed: boolean | undefined;
  repo: { owner: string; name: string } | undefined;
}

const UI_DEFAULTS: UIDefaults = {
  hideSync: false,
  hideRepoSelector: false,
  hideStar: false,
  sidebarCollapsed: undefined,
  repo: undefined,
};

let _generation = $state(0);

function readConfig(): MiddlemanConfig | undefined {
  void _generation; // reactive dependency
  return window.__middleman_config;
}

// Install the notify function on window. The embedder calls this
// after mutating window.__middleman_config.
window.__middleman_notify_config_changed = () => {
  _generation++;
};

export function isEmbedded(): boolean {
  return readConfig() !== undefined;
}

export function isHeaderHidden(): boolean {
  return readConfig()?.embed?.hideHeader === true;
}

export function isStatusBarHidden(): boolean {
  return readConfig()?.embed?.hideStatusBar === true;
}

export function getThemeMode():
  "light" | "dark" | "system" | undefined {
  return readConfig()?.theme?.mode;
}

export function getThemeColors():
  NonNullable<NonNullable<MiddlemanConfig["theme"]>["colors"]>
  | undefined {
  return readConfig()?.theme?.colors;
}

export function getThemeFonts():
  NonNullable<NonNullable<MiddlemanConfig["theme"]>["fonts"]>
  | undefined {
  return readConfig()?.theme?.fonts;
}

export function getThemeRadii():
  NonNullable<NonNullable<MiddlemanConfig["theme"]>["radii"]>
  | undefined {
  return readConfig()?.theme?.radii;
}

export function getUIConfig(): UIDefaults {
  const ui = readConfig()?.ui;
  if (!ui) return UI_DEFAULTS;
  return {
    hideSync: ui.hideSync ?? false,
    hideRepoSelector: ui.hideRepoSelector ?? false,
    hideStar: ui.hideStar ?? false,
    sidebarCollapsed: ui.sidebarCollapsed,
    repo: ui.repo,
  };
}

export function getActiveWorktreeKey(): string | undefined {
  return readConfig()?.ui?.activeWorktreeKey;
}

export function getHost(): string | undefined {
  return readConfig()?.ui?.host;
}

export function getPullRequestActions(): ActionHook[] {
  return readConfig()?.actions?.pullRequest ?? [];
}

export function getIssueActions(): ActionHook[] {
  return readConfig()?.actions?.issue ?? [];
}

export function getOnNavigate():
  ((event: MiddlemanNavigateEvent) => void) | undefined {
  return readConfig()?.onNavigate;
}

export function getOnRouteChange():
  ((event: MiddlemanNavigateEvent) => void) | undefined {
  return readConfig()?.onRouteChange;
}

export function invokeAction(
  action: ActionHook,
  context: ActionContext,
): void {
  try {
    const result = action.handler(context);
    Promise.resolve(result).catch((err: unknown) => {
      console.error("Embedding action error:", err);
    });
  } catch (err) {
    console.error("Embedding action error:", err);
  }
}

export function getInitialRoute(): string | undefined {
  return readConfig()?.embed?.initialRoute;
}

export function getSidebarWidth(): number | undefined {
  return readConfig()?.embed?.sidebarWidth;
}

export function getOnLayoutChanged():
  MiddlemanConfig["onLayoutChanged"] | undefined {
  return readConfig()?.onLayoutChanged;
}

let layoutTimer: ReturnType<typeof setTimeout> | undefined;

export function emitLayoutChanged(layout: {
  sidebar: { width: number };
  pinnedPanel: { width: number; visible: boolean };
}): void {
  clearTimeout(layoutTimer);
  layoutTimer = setTimeout(() => {
    const handler = getOnLayoutChanged();
    if (handler) {
      try {
        handler(layout);
      } catch (e) {
        console.error("[middleman] onLayoutChanged error:", e);
      }
    }
  }, 150);
}

export function getWorkspaceData():
  WorkspaceData | undefined {
  return readConfig()?.workspace;
}

export function getOnWorkspaceCommand():
  WorkspaceCommandHandler | undefined {
  return readConfig()?.onWorkspaceCommand;
}

export async function emitWorkspaceCommand(
  command: string,
  payload: Record<string, unknown>,
): Promise<CommandResult> {
  const handler = getOnWorkspaceCommand();
  if (!handler) {
    return { ok: true };
  }
  try {
    const result = await handler(command, payload);
    if (result && typeof result === "object" && "ok" in result) {
      return result;
    }
    return { ok: true };
  } catch (e) {
    const message =
      e instanceof Error ? e.message : String(e);
    console.error(
      `[middleman] workspace command "${command}" failed:`,
      e,
    );
    return { ok: false, message };
  }
}

export function initWorkspaceBridge(): void {
  window.__middleman_update_workspace = (
    data: WorkspaceData,
  ) => {
    const config = window.__middleman_config;
    if (config) {
      config.workspace = data;
      window.__middleman_notify_config_changed?.();
    }
  };
  window.__middleman_update_selection = (
    selection: {
      hostKey?: string | null;
      worktreeKey?: string | null;
    },
  ) => {
    const config = window.__middleman_config;
    if (!config?.workspace) return;
    const changingHost =
      "hostKey" in selection &&
      selection.hostKey !== config.workspace.selectedHostKey;
    if ("hostKey" in selection) {
      config.workspace.selectedHostKey = selection.hostKey ?? null;
    }
    if ("worktreeKey" in selection) {
      config.workspace.selectedWorktreeKey =
        selection.worktreeKey ?? null;
    } else if (changingHost) {
      config.workspace.selectedWorktreeKey = null;
    }
    window.__middleman_notify_config_changed?.();
  };
  window.__middleman_update_host_state = (
    hostKey: string,
    patch: {
      connectionState?: WorkspaceHost["connectionState"];
      resources?: WorkspaceResources | null;
    },
  ) => {
    const config = window.__middleman_config;
    const host = config?.workspace?.hosts.find(
      (h) => h.key === hostKey,
    );
    if (!host) return;
    if ("connectionState" in patch) {
      host.connectionState = patch.connectionState!;
    }
    if ("resources" in patch) {
      host.resources = patch.resources ?? null;
    }
    window.__middleman_notify_config_changed?.();
  };
}
