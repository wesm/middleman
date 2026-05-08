import type {
  ConfigRepo,
  TerminalRenderer,
  TerminalSettings,
} from "../api/types.js";

export function createSettingsStore() {
  let repos = $state<ConfigRepo[]>([]);
  let terminalFontFamily = $state("");
  let terminalRenderer = $state<TerminalRenderer>("xterm");
  let notificationsFeatureEnabled = $state(false);
  let loaded = $state(false);

  function getConfiguredRepos(): ConfigRepo[] {
    return repos;
  }

  function setConfiguredRepos(r: ConfigRepo[]): void {
    repos = r ?? [];
    loaded = true;
  }

  function getTerminalFontFamily(): string {
    return terminalFontFamily;
  }

  function setTerminalFontFamily(
    fontFamily: TerminalSettings["font_family"] | null | undefined,
  ): void {
    terminalFontFamily = fontFamily ?? "";
  }

  function getTerminalRenderer(): TerminalRenderer {
    return terminalRenderer;
  }

  function setTerminalRenderer(
    renderer: TerminalSettings["renderer"] | null | undefined,
  ): void {
    terminalRenderer = renderer === "ghostty-web" ? "ghostty-web" : "xterm";
  }

  function setNotificationsEnabled(enabled: boolean): void {
    notificationsFeatureEnabled = enabled;
  }

  function notificationsEnabled(): boolean {
    return notificationsFeatureEnabled;
  }

  function hasConfiguredRepos(): boolean {
    return repos.length > 0;
  }

  function isSettingsLoaded(): boolean {
    return loaded;
  }

  return {
    getConfiguredRepos,
    setConfiguredRepos,
    getTerminalFontFamily,
    setTerminalFontFamily,
    getTerminalRenderer,
    setTerminalRenderer,
    setNotificationsEnabled,
    notificationsEnabled,
    hasConfiguredRepos,
    isSettingsLoaded,
  };
}

export type SettingsStore = ReturnType<
  typeof createSettingsStore
>;
