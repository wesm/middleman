/// <reference types="vite/client" />

interface MiddlemanConfig {
  theme?: {
    mode?: "light" | "dark" | "system";
    colors?: Partial<{
      bgPrimary: string;
      bgSurface: string;
      bgSurfaceHover: string;
      bgInset: string;
      borderDefault: string;
      borderMuted: string;
      textPrimary: string;
      textSecondary: string;
      textMuted: string;
      accentBlue: string;
      accentAmber: string;
      accentPurple: string;
      accentGreen: string;
      accentRed: string;
      accentTeal: string;
      overlayBg: string;
      shadowSm: string;
      shadowMd: string;
      shadowLg: string;
      kanbanNew: string;
      kanbanReviewing: string;
      kanbanWaiting: string;
      kanbanAwaitingMerge: string;
    }>;
    fonts?: Partial<{
      sans: string;
      mono: string;
    }>;
    radii?: Partial<{
      sm: string;
      md: string;
      lg: string;
    }>;
  };
  ui?: {
    hideSync?: boolean;
    hideRepoSelector?: boolean;
    hideStar?: boolean;
    sidebarCollapsed?: boolean;
    repo?: { owner: string; name: string };
    activeWorktreeKey?: string;
  };
  actions?: {
    pullRequest?: ActionHookDef[];
    issue?: ActionHookDef[];
  };
  onNavigate?: (event: MiddlemanNavigateEvent) => void;
  onRouteChange?: (event: MiddlemanNavigateEvent) => void;
}

interface ActionHookDef {
  id: string;
  label: string;
  handler: (context: {
    surface: string;
    owner: string;
    name: string;
    number: number;
    meta?: Record<string, unknown>;
  }) => void | Promise<void>;
}

interface MiddlemanNavigateEvent {
  type: "pull" | "issue" | "activity" | "board";
  owner?: string;
  name?: string;
  number?: number;
  focus: boolean;
  view: string;
}

interface Window {
  __BASE_PATH__?: string;
  __middleman_config?: MiddlemanConfig;
  __middleman_notify_config_changed?: () => void;

}
