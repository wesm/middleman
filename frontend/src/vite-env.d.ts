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
    host?: string;
    activeWorktreeKey?: string;
  };
  actions?: {
    pullRequest?: ActionHookDef[];
    issue?: ActionHookDef[];
  };
  workspace?: WorkspaceData;
  onWorkspaceCommand?: WorkspaceCommandHandler;
  embed?: {
    hideHeader?: boolean;
    hideStatusBar?: boolean;
    initialRoute?: string;
    sidebarWidth?: number;
  };
  onLayoutChanged?: (layout: {
    sidebar: { width: number };
    pinnedPanel: { width: number; visible: boolean };
  }) => void;
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

interface WorkspaceHost {
  key: string;
  label: string;
  connectionState:
    | "connected"
    | "connecting"
    | "disconnected"
    | "error";
  transport?: "ssh" | "local";
  platform?: string;
  projects: WorkspaceProject[];
  sessions: WorkspaceSession[];
  resources: WorkspaceResources | null;
}

interface WorkspaceProject {
  key: string;
  name: string;
  kind: "repository" | "scratch";
  repoKind: string;
  defaultBranch: string;
  platformRepo: string | null;
  platformURL?: string;
  worktrees: WorkspaceWorktree[];
}

interface WorkspaceWorktree {
  key: string;
  name: string;
  branch: string;
  isPrimary: boolean;
  isHidden: boolean;
  isStale: boolean;
  sessionBackend: string | null;
  linkedPR: WorkspaceLinkedPR | null;
  activity: WorkspaceActivity;
  diff: WorkspaceDiff | null;
}

interface WorkspaceLinkedPR {
  number: number;
  title: string;
  state: "open" | "closed" | "merged";
  checksStatus: string | null;
  updatedAt: string | null;
}

interface WorkspaceActivity {
  state: "idle" | "active" | "running" | "needsAttention";
  lastOutputAt: string | null;
}

interface WorkspaceDiff {
  added: number;
  removed: number;
}

interface WorkspaceSession {
  key: string;
  name: string;
  worktreeKey: string | null;
  isHidden: boolean;
}

interface WorkspaceResources {
  cpuPercent: number;
  residentMB: number;
}

interface WorkspaceData {
  hosts: WorkspaceHost[];
  selectedWorktreeKey: string | null;
  selectedHostKey: string | null;
}

interface CommandResult {
  ok: boolean;
  message?: string;
}

interface WorkspaceCommandHandler {
  (
    command: string,
    payload: Record<string, unknown>,
  ): CommandResult | Promise<CommandResult>;
}

interface WorkspaceDetailContext {
  worktree: WorkspaceWorktree | null;
  project: WorkspaceProject | null;
  host: WorkspaceHost | null;
}

interface MiddlemanNavigateEvent {
  type: "pull" | "issue" | "activity" | "board" | "reviews" | "workspaces";
  owner?: string;
  name?: string;
  number?: number;
  focus: boolean;
  view: string;
  repo?: string;
  host?: string;
}

interface Window {
  __BASE_PATH__?: string;
  __middleman_config?: MiddlemanConfig;
  __middleman_notify_config_changed?: () => void;
  __middleman_update_workspace?: (data: WorkspaceData) => void;
  __middleman_navigate_to_route?: (route: string) => void;
  __middleman_set_repo_filter?: (
    repo: { owner: string; name: string } | null,
  ) => void;
  __middleman_update_selection?: (
    selection: {
      hostKey?: string | null;
      worktreeKey?: string | null;
    },
  ) => void;
  __middleman_update_host_state?: (
    hostKey: string,
    patch: {
      connectionState?: WorkspaceHost["connectionState"];
      resources?: WorkspaceResources | null;
    },
  ) => void;
}
