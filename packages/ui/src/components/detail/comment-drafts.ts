type CommentDraftTarget = "issue" | "pull";

const drafts = new Map<string, string>();

function draftKey(
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
  return drafts.get(draftKey(target, owner, name, number)) ?? "";
}

export function setCommentDraft(
  target: CommentDraftTarget,
  owner: string,
  name: string,
  number: number,
  body: string,
): void {
  const key = draftKey(target, owner, name, number);
  if (body === "") {
    drafts.delete(key);
    return;
  }
  drafts.set(key, body);
}

export function clearCommentDraft(
  target: CommentDraftTarget,
  owner: string,
  name: string,
  number: number,
): void {
  drafts.delete(draftKey(target, owner, name, number));
}
