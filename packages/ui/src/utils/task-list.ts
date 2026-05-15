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
const BULLET_LINE = /^[\t ]*(?:[-*+]|\d+\.)[\t ]+/;
const FENCE_LINE = /^[\t ]*(```|~~~)/;

export interface TaskItem {
  // Zero-based index of this task in document order.
  index: number;
  // Whether the checkbox is currently checked.
  checked: boolean;
  // Line number (zero-based) where the checkbox lives.
  line: number;
}

function leadingWhitespaceCount(line: string): number {
  let i = 0;
  while (i < line.length && (line[i] === " " || line[i] === "\t")) i++;
  return i;
}

// CommonMark indented code block start: 4+ leading spaces or a leading
// tab, at a position where a code block can begin (i.e. outside a list
// context). Inside a list, the same indentation continues the list
// item rather than starting a code block.
function isIndentedCodeStart(line: string): boolean {
  if (line.startsWith("\t")) return true;
  if (line.startsWith("    ")) return true;
  return false;
}

type TaskLineVisitor = (
  match: RegExpMatchArray,
  lineIndex: number,
  taskIndex: number,
) => void;

// Walks `lines` and invokes `visitor` for every task-list line that
// the markdown renderer would treat as such. Skips lines inside
// fenced code blocks and top-level indented code blocks. List
// context is tracked so an indented task line under a parent bullet
// is recognized as a nested task rather than a code-block line.
function walkTaskLines(lines: string[], visitor: TaskLineVisitor): void {
  let inFence = false;
  let listIndent: number | null = null;
  let taskIndex = 0;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    if (FENCE_LINE.test(line)) {
      inFence = !inFence;
      // A fenced block at the same indent as the list bullet ends
      // the list — Markdown treats the fence as block-level content.
      if (!inFence && listIndent !== null
        && leadingWhitespaceCount(line) <= listIndent) {
        listIndent = null;
      }
      continue;
    }
    if (inFence) continue;
    if (line.trim() === "") continue;

    const indent = leadingWhitespaceCount(line);
    // Outside a list, a 4-space (or tab) indent opens a code block:
    // any task-shaped line in it is plain code, not a task.
    if (listIndent === null && isIndentedCodeStart(line)) continue;

    // Dedent out of the list when a non-blank line sits at or below
    // the list bullet indent and isn't itself a bullet at that level.
    if (
      listIndent !== null &&
      indent < listIndent
    ) {
      listIndent = null;
    }

    const taskMatch = line.match(TASK_LINE);
    if (taskMatch) {
      visitor(taskMatch, i, taskIndex++);
      if (listIndent === null || indent < listIndent) {
        listIndent = indent;
      }
      continue;
    }

    // Non-task bullet still establishes/preserves list context so
    // a subsequent indented task line is recognized as nested.
    if (BULLET_LINE.test(line)) {
      if (listIndent === null) listIndent = indent;
      continue;
    }

    // Non-bullet line at or below list indent terminates the list.
    if (listIndent !== null && indent <= listIndent) {
      listIndent = null;
    }
  }
}

export function listTaskItems(source: string): TaskItem[] {
  if (!source) return [];
  const out: TaskItem[] = [];
  walkTaskLines(source.split("\n"), (m, lineIndex, taskIndex) => {
    out.push({
      index: taskIndex,
      checked: m[2] !== " ",
      line: lineIndex,
    });
  });
  return out;
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
  walkTaskLines(lines, (_m, lineIndex) => {
    taskLines.push(lineIndex);
  });
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
  let result = source;
  let mutated = false;
  walkTaskLines(lines, (m, lineIndex, taskIndex) => {
    if (mutated) return;
    if (taskIndex !== index) return;
    const prefix = m[1]!;
    const ch = m[2]!;
    const next = ch === " " ? "x" : " ";
    const original = lines[lineIndex]!;
    lines[lineIndex] = `${prefix}${next}${original.slice(prefix.length + 1)}`;
    result = lines.join("\n");
    mutated = true;
  });
  return result;
}
