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

// Returns a new source string with the Nth task-list line moved to
// the position currently occupied by the Mth task-list line. Other
// lines shift accordingly, like Array.prototype.splice. Only the task
// line itself moves — continuation lines under a multi-line task item
// are NOT carried along (a known limitation). If either index is out
// of range, or they're equal, the source is returned unchanged.
export function moveTaskListItem(
  source: string,
  fromIndex: number,
  toIndex: number,
): string {
  if (!source) return source;
  if (fromIndex === toIndex) return source;
  if (fromIndex < 0 || toIndex < 0) return source;
  const lines = source.split("\n");
  const positions: number[] = [];
  let inFence = false;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    if (FENCE_LINE.test(line)) {
      inFence = !inFence;
      continue;
    }
    if (inFence) continue;
    if (TASK_LINE.test(line)) positions.push(i);
  }
  if (fromIndex >= positions.length) return source;
  if (toIndex >= positions.length) return source;
  const fromLine = positions[fromIndex]!;
  const toLine = positions[toIndex]!;
  const moved = lines[fromLine]!;
  const out: string[] = [];
  for (let i = 0; i < lines.length; i++) {
    if (i === fromLine) continue;
    if (fromIndex < toIndex) {
      out.push(lines[i]!);
      if (i === toLine) out.push(moved);
    } else {
      if (i === toLine) out.push(moved);
      out.push(lines[i]!);
    }
  }
  return out.join("\n");
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
