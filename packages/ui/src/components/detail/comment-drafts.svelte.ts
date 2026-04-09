type CommentDraftTarget = "issue" | "pull";

let drafts = $state<Record<string, string>>({});

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
    const { [key]: _removed, ...rest } = drafts;
    drafts = rest;
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
  const { [key]: _removed, ...rest } = drafts;
  drafts = rest;
}
