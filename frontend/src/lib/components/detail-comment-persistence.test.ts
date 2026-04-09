import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import { STORES_KEY } from "../../../../packages/ui/src/context.js";
import CommentBox from "../../../../packages/ui/src/components/detail/CommentBox.svelte";
import IssueCommentBox from "../../../../packages/ui/src/components/detail/IssueCommentBox.svelte";

function renderPullCommentBox(owner = "octo", name = "repo", number = 1) {
  return render(CommentBox, {
    props: { owner, name, number },
    context: new Map([
      [STORES_KEY, {
        detail: {
          submitComment: async () => {},
          getDetailError: () => null,
        },
      }],
    ]),
  });
}

function renderIssueCommentBox(owner = "octo", name = "repo", number = 1) {
  return render(IssueCommentBox, {
    props: { owner, name, number },
    context: new Map([
      [STORES_KEY, {
        issues: {
          submitIssueComment: async () => {},
          getIssueDetailError: () => null,
        },
      }],
    ]),
  });
}

describe("comment draft persistence", () => {
  afterEach(() => {
    cleanup();
  });

  it("keeps the pull request comment draft when the box remounts", async () => {
    const firstRender = renderPullCommentBox("octo", "repo", 1);

    const textarea = screen.getByPlaceholderText(
      "Write a comment... (Cmd+Enter to submit)",
    ) as HTMLTextAreaElement;
    await fireEvent.input(textarea, { target: { value: "draft review note" } });

    firstRender.unmount();
    renderPullCommentBox("octo", "repo", 1);

    expect(
      (screen.getByPlaceholderText(
        "Write a comment... (Cmd+Enter to submit)",
      ) as HTMLTextAreaElement).value,
    ).toBe("draft review note");
  });

  it("keeps the issue comment draft when the box remounts", async () => {
    const firstRender = renderIssueCommentBox("octo", "repo", 2);

    const textarea = screen.getByPlaceholderText(
      "Write a comment... (Cmd+Enter to submit)",
    ) as HTMLTextAreaElement;
    await fireEvent.input(textarea, { target: { value: "draft issue note" } });

    firstRender.unmount();
    renderIssueCommentBox("octo", "repo", 2);

    expect(
      (screen.getByPlaceholderText(
        "Write a comment... (Cmd+Enter to submit)",
      ) as HTMLTextAreaElement).value,
    ).toBe("draft issue note");
  });
});
