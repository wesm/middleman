import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/svelte";
import {
  afterEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";

const {
  mockSetTerminalFontFamily,
  mockUpdateSettings,
} = vi.hoisted(() => ({
  mockSetTerminalFontFamily: vi.fn(),
  mockUpdateSettings: vi.fn(),
}));

vi.mock("@middleman/ui", () => ({
  getStores: () => ({
    settings: {
      setTerminalFontFamily: mockSetTerminalFontFamily,
    },
  }),
}));

vi.mock("../../api/settings.js", () => ({
  updateSettings: mockUpdateSettings,
}));

vi.mock("../../stores/embed-config.svelte.js", () => ({
  isEmbedded: () => false,
}));

import TerminalSettings from "./TerminalSettings.svelte";

describe("TerminalSettings", () => {
  afterEach(() => {
    cleanup();
    mockSetTerminalFontFamily.mockReset();
    mockUpdateSettings.mockReset();
  });

  it("enables save after editing and persists the font family", async () => {
    mockUpdateSettings.mockResolvedValue({
      terminal: {
        font_family: "\"Iosevka Term\", monospace",
      },
    });
    const onUpdate = vi.fn();

    render(TerminalSettings, {
      props: {
        terminal: { font_family: "" },
        onUpdate,
      },
    });

    const input = screen.getByLabelText("Monospace font family");
    const saveButton = screen.getByRole("button", { name: "Save" });

    await fireEvent.input(input, {
      target: { value: "\"Iosevka Term\", monospace" },
    });

    await waitFor(() => {
      expect(
        (saveButton as HTMLButtonElement).disabled,
      ).toBe(false);
    });

    await fireEvent.click(saveButton);

    await waitFor(() => {
      expect(mockUpdateSettings).toHaveBeenCalledWith({
        terminal: {
          font_family: "\"Iosevka Term\", monospace",
        },
      });
    });
    expect(onUpdate).toHaveBeenCalledWith({
      font_family: "\"Iosevka Term\", monospace",
    });
    expect(mockSetTerminalFontFamily).toHaveBeenCalledWith(
      "\"Iosevka Term\", monospace",
    );
  });
});
