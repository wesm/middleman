export interface ActionHook {
  id: string;
  label: string;
  handler: (context: ActionContext) => void | Promise<void>;
}

export interface ActionContext {
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
