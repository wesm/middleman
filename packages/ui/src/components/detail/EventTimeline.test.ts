import { cleanup, render, screen } from "@testing-library/svelte";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import EventTimeline from "./EventTimeline.svelte";
import type { PREvent } from "../../api/types.js";

function makeEvent(overrides: Partial<PREvent> = {}): PREvent {
  return {
    ID: 1,
    MergeRequestID: 42,
    PlatformID: null,
    EventType: "force_push",
    Author: "alice",
    Body: "",
    Summary: "aaaaaaa -> bbbbbbb",
    MetadataJSON: JSON.stringify({
      before_sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      after_sha: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    }),
    DedupeKey: "force-push-1",
    CreatedAt: "2024-06-01T12:00:00Z",
    ...overrides,
  } as PREvent;
}

function findComponentStyleRule(selector: string): string {
  const source = readFileSync(
    resolve(process.cwd(), "../packages/ui/src/components/detail/EventTimeline.svelte"),
    "utf8",
  );
  return source.match(new RegExp(`\\${selector}\\s*\\{[^}]*\\}`))?.[0] ?? "";
}

describe("EventTimeline", () => {
  afterEach(() => {
    cleanup();
  });

  it("renders force-push label, actor, and SHA transition", () => {
    render(EventTimeline, {
      props: {
        events: [makeEvent()],
      },
    });

    const label = screen.getByText("Force-pushed");
    expect(label).toBeTruthy();
    expect(label.getAttribute("style")).toContain("var(--accent-red)");
    expect(screen.getByText("alice")).toBeTruthy();
    expect(screen.getByText("aaaaaaa -> bbbbbbb")).toBeTruthy();
  });

  it("keeps the timeline entry card while rendering body content without a nested card surface", () => {
    const { container } = render(EventTimeline, {
      props: {
        events: [
          makeEvent({
            Body: "Timeline body text",
            EventType: "issue_comment",
          }),
        ],
      },
    });

    const wrapper = container.querySelector(".event-card");
    const body = container.querySelector(".event-body");
    expect(wrapper).toBeInstanceOf(HTMLElement);
    expect(body).toBeInstanceOf(HTMLElement);

    const eventCardRule = findComponentStyleRule(".event-card");
    const eventBodyRule = findComponentStyleRule(".event-body");

    expect(eventCardRule).toContain("background:");
    expect(eventCardRule).toContain("border:");
    expect(eventCardRule).toContain("border-radius:");
    expect(eventBodyRule).not.toContain("background:");
    expect(eventBodyRule).not.toContain("border:");
    expect(eventBodyRule).not.toContain("border-radius:");
  });
});
