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
    expect(screen.getByText("Selected 2 of 3")).toBeTruthy();
    expect((screen.getByRole("checkbox", { name: "Select acme/widget" }) as HTMLInputElement).disabled).toBe(true);
    expect(screen.getAllByText("Already added").length).toBeGreaterThan(1);
  });

  it("filters, deselects visible rows, and submits remaining selected rows", async () => {
    const onImported = vi.fn();
    preview.mockResolvedValue({ owner: "acme", pattern: "*", repos: rows });
    bulk.mockResolvedValue({ repos: [], activity: { view_mode: "threaded", time_range: "7d", hide_closed: false, hide_bots: false }, terminal: { font_family: "" } });
    render(RepoImportModal, { props: { open: true, onClose: vi.fn(), onImported } });

    await fireEvent.input(screen.getByLabelText("Repository pattern"), { target: { value: "acme/*" } });
    await fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await screen.findByText("acme/api");

    await fireEvent.input(screen.getByLabelText("Filter repositories"), { target: { value: "worker" } });
    await fireEvent.click(screen.getByRole("button", { name: "None" }));
    await fireEvent.input(screen.getByLabelText("Filter repositories"), { target: { value: "" } });
    await fireEvent.click(screen.getByRole("button", { name: "Add selected repositories" }));

    await waitFor(() => expect(bulk).toHaveBeenCalledWith([{ owner: "acme", name: "api" }]));
    expect(onImported).toHaveBeenCalled();
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
    expect(screen.getAllByText("Preview repositories before adding them.").length).toBeGreaterThan(0);
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

    await screen.findByText("GitHub API error: boom");
    expect(screen.queryByText("acme/api")).toBeNull();
  });
});
