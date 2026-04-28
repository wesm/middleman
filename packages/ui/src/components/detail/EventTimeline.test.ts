import { cleanup, render, screen } from "@testing-library/svelte";
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

function findTimelineWrapperRule(): string {
  for (const sheet of Array.from(document.styleSheets)) {
    for (const rule of Array.from(sheet.cssRules)) {
      if (rule instanceof CSSStyleRule && rule.selectorText.includes(".event-card")) {
        return rule.cssText;
      }
    }
  }
  return "";
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

  it("uses the event wrapper for spacing without adding a nested card surface", () => {
    const { container } = render(EventTimeline, {
      props: {
        events: [makeEvent()],
      },
    });

    const wrapper = container.querySelector(".event-card");
    expect(wrapper).toBeInstanceOf(HTMLElement);

    const eventCardRule = findTimelineWrapperRule();

    expect(eventCardRule).not.toContain("background:");
    expect(eventCardRule).not.toContain("border:");
    expect(eventCardRule).not.toContain("border-radius:");
  });
});
