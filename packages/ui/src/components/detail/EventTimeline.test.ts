import { cleanup, render, screen } from "@testing-library/svelte";
import { compile } from "svelte/compiler";
import { afterEach, describe, expect, it } from "vitest";
import componentSource from "./EventTimeline.svelte?raw";
import EventTimeline from "./EventTimeline.svelte";
import type { PREvent } from "../../api/types.js";

const compiledCss = compile(
  componentSource,
  { filename: "EventTimeline.svelte" },
).css?.code ?? "";

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

function findCompiledStyleRule(
  selector: string,
  exclude: string[] = [],
): CSSStyleDeclaration {
  const style = document.createElement("style");
  style.textContent = compiledCss;
  document.head.appendChild(style);

  for (const rule of Array.from(style.sheet?.cssRules ?? [])) {
    if (!("selectorText" in rule) || !("style" in rule)) continue;
    const selectorText = String(rule.selectorText);
    if (
      selectorText.includes(selector)
      && exclude.every((part) => !selectorText.includes(part))
    ) {
      return rule.style as CSSStyleDeclaration;
    }
  }
  throw new Error(`Could not find compiled style rule for ${selector}`);
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

    const cards = container.querySelectorAll(".event-card");
    const wrapper = cards[0];
    const body = container.querySelector(".event-body");
    const bodyWrap = container.querySelector(".event-body-wrap");
    expect(cards).toHaveLength(1);
    expect(wrapper).toBeInstanceOf(HTMLElement);
    expect(body).toBeInstanceOf(HTMLElement);
    expect(bodyWrap).toBeInstanceOf(HTMLElement);

    expect(wrapper!.contains(bodyWrap)).toBe(true);
    expect(bodyWrap!.contains(body)).toBe(true);
    expect(body!.classList.contains("event-card")).toBe(false);

    const cardStyle = findCompiledStyleRule(".event-card");
    const bodyStyle = findCompiledStyleRule(".event-body", [
      ".event-body-wrap",
      ".markdown-body",
    ]);

    expect(cardStyle.getPropertyValue("background")).toBe("var(--bg-surface)");
    expect(cardStyle.getPropertyValue("border")).toBe("1px solid var(--border-muted)");
    expect(cardStyle.getPropertyValue("border-radius")).toBe("var(--radius-md)");
    expect(bodyStyle.getPropertyValue("background")).toBe("");
    expect(bodyStyle.getPropertyValue("border")).toBe("");
    expect(bodyStyle.getPropertyValue("border-radius")).toBe("");
  });
});
