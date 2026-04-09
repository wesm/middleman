import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import { STORES_KEY } from "../../../../packages/ui/src/context.js";
import CommentBox from "../../../../packages/ui/src/components/detail/CommentBox.svelte";
import IssueCommentBox from "../../../../packages/ui/src/components/detail/IssueCommentBox.svelte";
import {
  clearCommentSubmitError,
  finishCommentSubmit,
  getCommentDraft,
  getCommentSubmitError,
  isCommentSubmitPending,
  setCommentDraft,
} from "../../../../packages/ui/src/components/detail/comment-drafts.svelte.js";
import CommentBoxContextHarness from "./CommentBoxContextHarness.svelte";

function deferred(): {
  promise: Promise<void>;
  resolve: () => void;
} {
  let resolve = () => {};
  const promise = new Promise<void>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

function deferredByNumber(numbers: number[]): Map<number, ReturnType<typeof deferred>> {
  return new Map(numbers.map((number) => [number, deferred()]));
}

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
    setCommentDraft("pull", "octo", "repo", 1, "");
    setCommentDraft("pull", "octo", "repo", 2, "");
    setCommentDraft("issue", "octo", "repo", 1, "");
    setCommentDraft("issue", "octo", "repo", 2, "");
    clearCommentSubmitError("pull", "octo", "repo", 1);
    clearCommentSubmitError("pull", "octo", "repo", 2);
    clearCommentSubmitError("issue", "octo", "repo", 1);
    clearCommentSubmitError("issue", "octo", "repo", 2);
    finishCommentSubmit("pull", "octo", "repo", 1);
    finishCommentSubmit("pull", "octo", "repo", 2);
    finishCommentSubmit("issue", "octo", "repo", 1);
    finishCommentSubmit("issue", "octo", "repo", 2);
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

  it("does not clear the newly selected pull request draft when an earlier submit resolves", async () => {
    const submit = deferred();
    const { rerender } = render(CommentBoxContextHarness, {
      props: {
        kind: "pull",
        owner: "octo",
        name: "repo",
        number: 1,
        submitComment: async () => submit.promise,
      },
    });

    await fireEvent.input(screen.getByPlaceholderText(
      "Write a comment... (Cmd+Enter to submit)",
    ), { target: { value: "old pull draft" } });
    await fireEvent.click(screen.getByRole("button", { name: "Comment" }));

    setCommentDraft("pull", "octo", "repo", 2, "new pull draft");
    await rerender({
      kind: "pull",
      owner: "octo",
      name: "repo",
      number: 2,
      submitComment: async () => submit.promise,
    });

    expect(
      (screen.getByPlaceholderText(
        "Write a comment... (Cmd+Enter to submit)",
      ) as HTMLTextAreaElement).disabled,
    ).toBe(false);
    expect(isCommentSubmitPending("pull", "octo", "repo", 2)).toBe(false);
    expect(
      (screen.getByRole("button", { name: "Comment" }) as HTMLButtonElement).disabled,
    ).toBe(false);

    submit.resolve();

    await waitFor(() => {
      expect(getCommentDraft("pull", "octo", "repo", 1)).toBe("");
    });

    expect(
      (screen.getByPlaceholderText(
        "Write a comment... (Cmd+Enter to submit)",
      ) as HTMLTextAreaElement).value,
    ).toBe("new pull draft");
    expect(getCommentDraft("pull", "octo", "repo", 2)).toBe("new pull draft");
  });

  it("does not clear the newly selected issue draft when an earlier submit resolves", async () => {
    const submit = deferred();
    const { rerender } = render(CommentBoxContextHarness, {
      props: {
        kind: "issue",
        owner: "octo",
        name: "repo",
        number: 1,
        submitComment: async () => submit.promise,
      },
    });

    await fireEvent.input(screen.getByPlaceholderText(
      "Write a comment... (Cmd+Enter to submit)",
    ), { target: { value: "old issue draft" } });
    await fireEvent.click(screen.getByRole("button", { name: "Comment" }));

    setCommentDraft("issue", "octo", "repo", 2, "new issue draft");
    await rerender({
      kind: "issue",
      owner: "octo",
      name: "repo",
      number: 2,
      submitComment: async () => submit.promise,
    });

    expect(
      (screen.getByPlaceholderText(
        "Write a comment... (Cmd+Enter to submit)",
      ) as HTMLTextAreaElement).disabled,
    ).toBe(false);
    expect(isCommentSubmitPending("issue", "octo", "repo", 2)).toBe(false);
    expect(
      (screen.getByRole("button", { name: "Comment" }) as HTMLButtonElement).disabled,
    ).toBe(false);

    submit.resolve();

    await waitFor(() => {
      expect(getCommentDraft("issue", "octo", "repo", 1)).toBe("");
    });

    expect(
      (screen.getByPlaceholderText(
        "Write a comment... (Cmd+Enter to submit)",
      ) as HTMLTextAreaElement).value,
    ).toBe("new issue draft");
    expect(getCommentDraft("issue", "octo", "repo", 2)).toBe("new issue draft");
  });

  it("keeps the original pull request disabled when returning to it before its submit resolves", async () => {
    const submits = deferredByNumber([1, 2]);
    const submitComment = async (_owner: string, _name: string, number: number) => {
      await submits.get(number)?.promise;
    };
    const { rerender } = render(CommentBoxContextHarness, {
      props: {
        kind: "pull",
        owner: "octo",
        name: "repo",
        number: 1,
        submitComment,
      },
    });

    await fireEvent.input(screen.getByPlaceholderText(
      "Write a comment... (Cmd+Enter to submit)",
    ), { target: { value: "old pull draft" } });
    await fireEvent.click(screen.getByRole("button", { name: "Comment" }));

    setCommentDraft("pull", "octo", "repo", 2, "new pull draft");
    await rerender({ kind: "pull", owner: "octo", name: "repo", number: 2, submitComment });
    await fireEvent.click(screen.getByRole("button", { name: "Comment" }));

    await rerender({ kind: "pull", owner: "octo", name: "repo", number: 1, submitComment });

    expect(
      (screen.getByPlaceholderText(
        "Write a comment... (Cmd+Enter to submit)",
      ) as HTMLTextAreaElement).disabled,
    ).toBe(true);
    expect(isCommentSubmitPending("pull", "octo", "repo", 1)).toBe(true);
    expect(
      (screen.getByRole("button", { name: "Posting…" }) as HTMLButtonElement).disabled,
    ).toBe(true);

    submits.get(2)?.resolve();
    await waitFor(() => {
      expect(getCommentDraft("pull", "octo", "repo", 2)).toBe("");
    });

    expect(
      (screen.getByPlaceholderText(
        "Write a comment... (Cmd+Enter to submit)",
      ) as HTMLTextAreaElement).disabled,
    ).toBe(true);

    submits.get(1)?.resolve();
    await waitFor(() => {
      expect(getCommentDraft("pull", "octo", "repo", 1)).toBe("");
    });

    expect(
      (screen.getByPlaceholderText(
        "Write a comment... (Cmd+Enter to submit)",
      ) as HTMLTextAreaElement).disabled,
    ).toBe(false);
    expect(isCommentSubmitPending("pull", "octo", "repo", 1)).toBe(false);
  });

  it("keeps the original issue disabled when returning to it before its submit resolves", async () => {
    const submits = deferredByNumber([1, 2]);
    const submitComment = async (_owner: string, _name: string, number: number) => {
      await submits.get(number)?.promise;
    };
    const { rerender } = render(CommentBoxContextHarness, {
      props: {
        kind: "issue",
        owner: "octo",
        name: "repo",
        number: 1,
        submitComment,
      },
    });

    await fireEvent.input(screen.getByPlaceholderText(
      "Write a comment... (Cmd+Enter to submit)",
    ), { target: { value: "old issue draft" } });
    await fireEvent.click(screen.getByRole("button", { name: "Comment" }));

    setCommentDraft("issue", "octo", "repo", 2, "new issue draft");
    await rerender({ kind: "issue", owner: "octo", name: "repo", number: 2, submitComment });
    await fireEvent.click(screen.getByRole("button", { name: "Comment" }));

    await rerender({ kind: "issue", owner: "octo", name: "repo", number: 1, submitComment });

    expect(
      (screen.getByPlaceholderText(
        "Write a comment... (Cmd+Enter to submit)",
      ) as HTMLTextAreaElement).disabled,
    ).toBe(true);
    expect(isCommentSubmitPending("issue", "octo", "repo", 1)).toBe(true);
    expect(
      (screen.getByRole("button", { name: "Posting…" }) as HTMLButtonElement).disabled,
    ).toBe(true);

    submits.get(2)?.resolve();
    await waitFor(() => {
      expect(getCommentDraft("issue", "octo", "repo", 2)).toBe("");
    });

    expect(
      (screen.getByPlaceholderText(
        "Write a comment... (Cmd+Enter to submit)",
      ) as HTMLTextAreaElement).disabled,
    ).toBe(true);

    submits.get(1)?.resolve();
    await waitFor(() => {
      expect(getCommentDraft("issue", "octo", "repo", 1)).toBe("");
    });

    expect(
      (screen.getByPlaceholderText(
        "Write a comment... (Cmd+Enter to submit)",
      ) as HTMLTextAreaElement).disabled,
    ).toBe(false);
    expect(isCommentSubmitPending("issue", "octo", "repo", 1)).toBe(false);
  });

  it("keeps a pull request pending submit disabled across remounts", async () => {
    const submit = deferred();
    const firstRender = render(CommentBoxContextHarness, {
      props: {
        kind: "pull",
        owner: "octo",
        name: "repo",
        number: 1,
        submitComment: async () => submit.promise,
      },
    });

    await fireEvent.input(screen.getByPlaceholderText(
      "Write a comment... (Cmd+Enter to submit)",
    ), { target: { value: "draft review note" } });
    await fireEvent.click(screen.getByRole("button", { name: "Comment" }));
    expect(isCommentSubmitPending("pull", "octo", "repo", 1)).toBe(true);

    firstRender.unmount();
    render(CommentBoxContextHarness, {
      props: {
        kind: "pull",
        owner: "octo",
        name: "repo",
        number: 1,
        submitComment: async () => submit.promise,
      },
    });

    expect(
      (screen.getByPlaceholderText(
        "Write a comment... (Cmd+Enter to submit)",
      ) as HTMLTextAreaElement).disabled,
    ).toBe(true);

    submit.resolve();
    await waitFor(() => {
      expect(isCommentSubmitPending("pull", "octo", "repo", 1)).toBe(false);
    });
  });

  it("keeps an issue pending submit disabled across remounts", async () => {
    const submit = deferred();
    const firstRender = render(CommentBoxContextHarness, {
      props: {
        kind: "issue",
        owner: "octo",
        name: "repo",
        number: 1,
        submitComment: async () => submit.promise,
      },
    });

    await fireEvent.input(screen.getByPlaceholderText(
      "Write a comment... (Cmd+Enter to submit)",
    ), { target: { value: "draft issue note" } });
    await fireEvent.click(screen.getByRole("button", { name: "Comment" }));
    expect(isCommentSubmitPending("issue", "octo", "repo", 1)).toBe(true);

    firstRender.unmount();
    render(CommentBoxContextHarness, {
      props: {
        kind: "issue",
        owner: "octo",
        name: "repo",
        number: 1,
        submitComment: async () => submit.promise,
      },
    });

    expect(
      (screen.getByPlaceholderText(
        "Write a comment... (Cmd+Enter to submit)",
      ) as HTMLTextAreaElement).disabled,
    ).toBe(true);

    submit.resolve();
    await waitFor(() => {
      expect(isCommentSubmitPending("issue", "octo", "repo", 1)).toBe(false);
    });
  });

  it("keeps a pull request submit error visible across remounts", async () => {
    const submit = deferred();
    const firstRender = render(CommentBoxContextHarness, {
      props: {
        kind: "pull",
        owner: "octo",
        name: "repo",
        number: 1,
        submitComment: async () => submit.promise,
        getError: () => "pull submit failed",
      },
    });

    await fireEvent.input(screen.getByPlaceholderText(
      "Write a comment... (Cmd+Enter to submit)",
    ), { target: { value: "draft review note" } });
    await fireEvent.click(screen.getByRole("button", { name: "Comment" }));

    submit.resolve();
    await waitFor(() => {
      expect(getCommentSubmitError("pull", "octo", "repo", 1)).toBe("pull submit failed");
    });

    firstRender.unmount();
    render(CommentBoxContextHarness, {
      props: {
        kind: "pull",
        owner: "octo",
        name: "repo",
        number: 1,
        submitComment: async () => Promise.resolve(),
        getError: () => "pull submit failed",
      },
    });

    expect(screen.getByText("pull submit failed")).toBeTruthy();
  });

  it("keeps an issue submit error visible across remounts", async () => {
    const submit = deferred();
    const firstRender = render(CommentBoxContextHarness, {
      props: {
        kind: "issue",
        owner: "octo",
        name: "repo",
        number: 1,
        submitComment: async () => submit.promise,
        getError: () => "issue submit failed",
      },
    });

    await fireEvent.input(screen.getByPlaceholderText(
      "Write a comment... (Cmd+Enter to submit)",
    ), { target: { value: "draft issue note" } });
    await fireEvent.click(screen.getByRole("button", { name: "Comment" }));

    submit.resolve();
    await waitFor(() => {
      expect(getCommentSubmitError("issue", "octo", "repo", 1)).toBe("issue submit failed");
    });

    firstRender.unmount();
    render(CommentBoxContextHarness, {
      props: {
        kind: "issue",
        owner: "octo",
        name: "repo",
        number: 1,
        submitComment: async () => Promise.resolve(),
        getError: () => "issue submit failed",
      },
    });

    expect(screen.getByText("issue submit failed")).toBeTruthy();
  });
});
