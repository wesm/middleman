import type {
  ConfigRepo,
  TerminalSettings,
} from "../api/types.js";

export function createSettingsStore() {
  let repos = $state<ConfigRepo[]>([]);
  let terminalFontFamily = $state("");
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
    hasConfiguredRepos,
    isSettingsLoaded,
  };
}

export type SettingsStore = ReturnType<
  typeof createSettingsStore
>;
