type CommentDraftTarget = "issue" | "pull";

let drafts = $state<Record<string, string>>({});
let pendingSubmitCounts = $state<Record<string, number>>({});
let submitErrors = $state<Record<string, string>>({});

export function getCommentDraftKey(
  target: CommentDraftTarget,
  owner: string,
  name: string,
  number: number,
): string {
  return `${target}:${owner}/${name}/${number}`;
}

export function getCommentDraft(
  target: CommentDraftTarget,
  owner: string,
  name: string,
  number: number,
): string {
  return drafts[getCommentDraftKey(target, owner, name, number)] ?? "";
}

export function setCommentDraft(
  target: CommentDraftTarget,
  owner: string,
  name: string,
  number: number,
  body: string,
): void {
  const key = getCommentDraftKey(target, owner, name, number);
  if (body === "") {
    const next = { ...drafts };
    delete next[key];
    drafts = next;
    return;
  }
  drafts = {
    ...drafts,
    [key]: body,
  };
}

export function clearCommentDraft(
  target: CommentDraftTarget,
  owner: string,
  name: string,
  number: number,
): void {
  const key = getCommentDraftKey(target, owner, name, number);
  const next = { ...drafts };
  delete next[key];
  drafts = next;
}

export function isCommentSubmitPending(
  target: CommentDraftTarget,
  owner: string,
  name: string,
  number: number,
): boolean {
  return (pendingSubmitCounts[getCommentDraftKey(target, owner, name, number)] ?? 0) > 0;
}

export function beginCommentSubmit(
  target: CommentDraftTarget,
  owner: string,
  name: string,
  number: number,
): void {
  const key = getCommentDraftKey(target, owner, name, number);
  pendingSubmitCounts = {
    ...pendingSubmitCounts,
    [key]: (pendingSubmitCounts[key] ?? 0) + 1,
  };
}

export function finishCommentSubmit(
  target: CommentDraftTarget,
  owner: string,
  name: string,
  number: number,
): void {
  const key = getCommentDraftKey(target, owner, name, number);
  const nextCount = (pendingSubmitCounts[key] ?? 0) - 1;
  if (nextCount <= 0) {
    const next = { ...pendingSubmitCounts };
    delete next[key];
    pendingSubmitCounts = next;
    return;
  }
  pendingSubmitCounts = {
    ...pendingSubmitCounts,
    [key]: nextCount,
  };
}

export function getCommentSubmitError(
  target: CommentDraftTarget,
  owner: string,
  name: string,
  number: number,
): string | null {
  return submitErrors[getCommentDraftKey(target, owner, name, number)] ?? null;
}

export function setCommentSubmitError(
  target: CommentDraftTarget,
  owner: string,
  name: string,
  number: number,
  error: string,
): void {
  const key = getCommentDraftKey(target, owner, name, number);
  submitErrors = {
    ...submitErrors,
    [key]: error,
  };
}

export function clearCommentSubmitError(
  target: CommentDraftTarget,
  owner: string,
  name: string,
  number: number,
): void {
  const key = getCommentDraftKey(target, owner, name, number);
  const next = { ...submitErrors };
  delete next[key];
  submitErrors = next;
}
