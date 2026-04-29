import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import RepoImportModal from "./RepoImportModal.svelte";
import { bulkAddRepos, previewRepos } from "../../api/settings.js";

vi.mock("../../api/settings.js", () => ({
  previewRepos: vi.fn(),
  bulkAddRepos: vi.fn(),
}));

const preview = vi.mocked(previewRepos);
const bulk = vi.mocked(bulkAddRepos);

const rows = [
  { owner: "acme", name: "worker", description: "Background jobs", private: false, pushed_at: "2026-04-20T00:00:00Z", already_configured: false },
  { owner: "acme", name: "api", description: "HTTP API", private: true, pushed_at: "2026-04-22T00:00:00Z", already_configured: false },
  { owner: "acme", name: "widget", description: "Configured", private: false, pushed_at: "2026-04-21T00:00:00Z", already_configured: true },
  { owner: "acme", name: "empty", description: null, private: false, pushed_at: null, already_configured: false },
];

describe("RepoImportModal", () => {
  afterEach(() => {
    cleanup();
    preview.mockReset();
    bulk.mockReset();
  });

  it("previews rows and defaults selectable rows to selected", async () => {
    preview.mockResolvedValue({ owner: "acme", pattern: "*", repos: rows });
    render(RepoImportModal, { props: { open: true, onClose: vi.fn(), onImported: vi.fn() } });

    await fireEvent.input(screen.getByLabelText("Repository pattern"), { target: { value: "acme/*" } });
    await fireEvent.click(screen.getByRole("button", { name: "Preview" }));

    await screen.findByText("acme/api");
    expect(screen.getByText("Selected 3 of 4")).toBeTruthy();
    expect((screen.getByRole("checkbox", { name: "Select acme/widget" }) as HTMLInputElement).disabled).toBe(true);
    expect(screen.getByText("Never pushed")).toBeTruthy();
  });

  it("filters, deselects visible rows, and submits remaining selected rows", async () => {
    const onImported = vi.fn();
    preview.mockResolvedValue({ owner: "acme", pattern: "*", repos: rows });
    bulk.mockResolvedValue({ repos: [], activity: { view_mode: "threaded", time_range: "7d", hide_closed: false, hide_bots: false }, terminal: { font_family: "" }, agents: [] });
    render(RepoImportModal, { props: { open: true, onClose: vi.fn(), onImported } });

    await fireEvent.input(screen.getByLabelText("Repository pattern"), { target: { value: "acme/*" } });
    await fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await screen.findByText("acme/api");

    await fireEvent.input(screen.getByLabelText("Filter repositories"), { target: { value: "worker" } });
    await fireEvent.click(screen.getByRole("button", { name: "None" }));
    await fireEvent.input(screen.getByLabelText("Filter repositories"), { target: { value: "" } });
    await fireEvent.click(screen.getByRole("button", { name: "Add selected repositories" }));

    await waitFor(() => expect(bulk).toHaveBeenCalledWith([
      { owner: "acme", name: "api" },
      { owner: "acme", name: "empty" },
    ]));
    expect(onImported).toHaveBeenCalled();
  });

  it("does not start duplicate previews while loading", async () => {
    preview.mockReturnValue(new Promise(() => {}));
    render(RepoImportModal, { props: { open: true, onClose: vi.fn(), onImported: vi.fn() } });

    const input = screen.getByLabelText("Repository pattern");
    await fireEvent.input(input, { target: { value: "acme/*" } });
    await fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await fireEvent.keyDown(input, { key: "Enter" });

    expect(preview).toHaveBeenCalledTimes(1);
  });

  it("keeps tab focus inside the modal", async () => {
    render(RepoImportModal, { props: { open: true, onClose: vi.fn(), onImported: vi.fn() } });

    const input = screen.getByLabelText("Repository pattern");
    await waitFor(() => expect(document.activeElement).toBe(input));
    const close = screen.getByRole("button", { name: "Close" });
    close.focus();
    await fireEvent.keyDown(close, { key: "Tab", shiftKey: true });

    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cancel" }));
  });

  it("ignores stale preview responses after input changes", async () => {
    let resolveFirst: (value: Awaited<ReturnType<typeof previewRepos>>) => void = () => {};
    preview.mockReturnValueOnce(new Promise((resolve) => { resolveFirst = resolve; }));
    render(RepoImportModal, { props: { open: true, onClose: vi.fn(), onImported: vi.fn() } });

    await fireEvent.input(screen.getByLabelText("Repository pattern"), { target: { value: "acme/*" } });
    await fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await fireEvent.input(screen.getByLabelText("Repository pattern"), { target: { value: "acme/api-*" } });
    resolveFirst({ owner: "acme", pattern: "*", repos: rows });

    await waitFor(() => expect(screen.queryByText("acme/api")).toBeNull());
    expect(screen.getByText("Selected 0 of 0")).toBeTruthy();
  });

  it("clears stale rows on failed preview", async () => {
    preview.mockResolvedValueOnce({ owner: "acme", pattern: "*", repos: rows });
    preview.mockRejectedValueOnce(new Error("GitHub API error: boom"));
    render(RepoImportModal, { props: { open: true, onClose: vi.fn(), onImported: vi.fn() } });

    await fireEvent.input(screen.getByLabelText("Repository pattern"), { target: { value: "acme/*" } });
    await fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await screen.findByText("acme/api");
    await fireEvent.input(screen.getByLabelText("Repository pattern"), { target: { value: "acme/worker*" } });
    await fireEvent.click(screen.getByRole("button", { name: "Preview" }));

    expect((await screen.findByRole("alert")).textContent).toContain("GitHub API error: boom");
    expect(screen.queryByText("acme/api")).toBeNull();
  });
});
