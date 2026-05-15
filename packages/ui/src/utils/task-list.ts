// GFM task list helpers. Mirrors the same recognition rules the marked
// renderer applies — see RenderMarkdownOpts.interactiveTasks — so that
// data-task-index attributes emitted there line up with the source-
// position-based toggling here.

// A line is a task-list item when it begins with optional leading
// whitespace, a bullet (-, *, +) or ordered marker (1.), at least one
// space, and then a checkbox token `[ ]`, `[x]`, or `[X]`. Anything
// inside a fenced code block is ignored so `[ ]` shown in code samples
// doesn't shift indices.
const TASK_LINE = /^([\t ]*(?:[-*+]|\d+\.)[\t ]+\[)([ xX])(\])/;
const FENCE_LINE = /^[\t ]*(```|~~~)/;

export interface TaskItem {
  // Zero-based index of this task in document order.
  index: number;
  // Whether the checkbox is currently checked.
  checked: boolean;
  // Line number (zero-based) where the checkbox lives.
  line: number;
}

export function listTaskItems(source: string): TaskItem[] {
  if (!source) return [];
  const out: TaskItem[] = [];
  let inFence = false;
  let count = 0;
  const lines = source.split("\n");
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    if (FENCE_LINE.test(line)) {
      inFence = !inFence;
      continue;
    }
    if (inFence) continue;
    const m = line.match(TASK_LINE);
    if (!m) continue;
    out.push({
      index: count++,
      checked: m[2] !== " ",
      line: i,
    });
  }
  return out;
}

function leadingWhitespaceCount(line: string): number {
  let i = 0;
  while (i < line.length && (line[i] === " " || line[i] === "\t")) i++;
  return i;
}

// A task block spans the task line plus any immediately-following
// continuation lines that belong to the same item — more-indented
// content (multi-line description, nested sub-tasks) is carried along
// with the task when it moves. The block ends at the first line that
// is blank or at the same/lower indent than the bullet.
function findTaskBlockEnd(
  lines: string[],
  start: number,
): number {
  const bulletIndent = leadingWhitespaceCount(lines[start]!);
  let end = start + 1;
  while (end < lines.length) {
    const line = lines[end]!;
    if (line.trim() === "") break;
    if (leadingWhitespaceCount(line) <= bulletIndent) break;
    end++;
  }
  return end;
}

// Returns a new source string with the Nth task-list item moved to
// the position currently occupied by the Mth task-list item. The
// moved item carries its continuation lines and nested sub-tasks
// with it. If either index is out of range, or they're equal, the
// source is returned unchanged.
export function moveTaskListItem(
  source: string,
  fromIndex: number,
  toIndex: number,
): string {
  if (!source) return source;
  if (fromIndex === toIndex) return source;
  if (fromIndex < 0 || toIndex < 0) return source;
  const lines = source.split("\n");
  const taskLines: number[] = [];
  let inFence = false;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    if (FENCE_LINE.test(line)) {
      inFence = !inFence;
      continue;
    }
    if (inFence) continue;
    if (TASK_LINE.test(line)) taskLines.push(i);
  }
  if (fromIndex >= taskLines.length) return source;
  if (toIndex >= taskLines.length) return source;
  const fromStart = taskLines[fromIndex]!;
  const fromEnd = findTaskBlockEnd(lines, fromStart);
  const toStart = taskLines[toIndex]!;
  const toEnd = findTaskBlockEnd(lines, toStart);
  // Refuse no-ops: dragging a task onto something inside its own
  // block would either be a no-op or self-overlap, both of which we
  // pass through unchanged.
  if (toStart >= fromStart && toStart < fromEnd) return source;
  const moved = lines.slice(fromStart, fromEnd);
  const without = [
    ...lines.slice(0, fromStart),
    ...lines.slice(fromEnd),
  ];
  // Where to insert depends on direction: moving down lands the block
  // where the target block ended (minus the removed block's length);
  // moving up lands it where the target block started.
  let insertAt: number;
  if (fromIndex < toIndex) {
    insertAt = toEnd - (fromEnd - fromStart);
  } else {
    insertAt = toStart;
  }
  return [
    ...without.slice(0, insertAt),
    ...moved,
    ...without.slice(insertAt),
  ].join("\n");
}

// Returns a new source string with the Nth task-list checkbox toggled.
// If `index` is out of range, the source is returned unchanged.
export function toggleTaskListItem(source: string, index: number): string {
  if (!source) return source;
  if (index < 0) return source;
  const lines = source.split("\n");
  let inFence = false;
  let count = 0;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    if (FENCE_LINE.test(line)) {
      inFence = !inFence;
      continue;
    }
    if (inFence) continue;
    const m = line.match(TASK_LINE);
    if (!m) continue;
    if (count === index) {
      const prefix = m[1]!;
      const ch = m[2]!;
      const next = ch === " " ? "x" : " ";
      lines[i] = `${prefix}${next}${line.slice(prefix.length + 1)}`;
      return lines.join("\n");
    }
    count++;
  }
  return source;
}
