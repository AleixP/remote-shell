// Reading text back out of a terminal buffer, for copying.

/** The part of xterm's buffer these helpers need. */
export interface BufferLike {
  length: number;
  getLine(y: number): { translateToString(trimRight?: boolean): string; isWrapped: boolean } | undefined;
}

/** The run of non-blank characters around `col`, or null when it is blank. */
export function wordAt(line: string, col: number): { start: number; length: number } | null {
  if (col < 0 || col >= line.length || /\s/.test(line[col])) return null;
  let start = col;
  let end = col;
  while (start > 0 && !/\s/.test(line[start - 1])) start--;
  while (end < line.length - 1 && !/\s/.test(line[end + 1])) end++;
  return { start, length: end - start + 1 };
}

/**
 * The text of rows `from` to `to` (inclusive), one line each. A row the
 * terminal wrapped is joined to the one before it, so long lines copy whole.
 */
export function readRows(buffer: BufferLike, from: number, to: number): string {
  const lines: string[] = [];
  const last = Math.min(to, buffer.length - 1);
  for (let y = Math.max(0, from); y <= last; y++) {
    const row = buffer.getLine(y);
    if (!row) continue;
    // Only the last row of a line is trimmed: on a wrapped row, trailing blanks
    // are real (a space can fall exactly at the edge) and belong to the text.
    const continues = buffer.getLine(y + 1)?.isWrapped === true;
    const text = row.translateToString(!continues);
    if (row.isWrapped && lines.length > 0) lines[lines.length - 1] += text;
    else lines.push(text);
  }
  while (lines.length > 0 && lines[lines.length - 1] === '') lines.pop();
  return lines.join('\n');
}

/** The full logical line (every wrapped row) that contains row `y`. */
export function readLineAt(buffer: BufferLike, y: number): string {
  let first = y;
  while (first > 0 && buffer.getLine(first)?.isWrapped) first--;
  let last = y;
  while (last + 1 < buffer.length && buffer.getLine(last + 1)?.isWrapped) last++;
  return readRows(buffer, first, last);
}

export interface Cell {
  col: number;
  row: number;
}

/** A selection between two cells, both included, with `start` not after `end`. */
export interface Range {
  start: Cell;
  end: Cell;
}

const isBefore = (a: Cell, b: Cell) => a.row < b.row || (a.row === b.row && a.col < b.col);

/** The range between two cells given in any order. */
export function orderRange(a: Cell, b: Cell): Range {
  return isBefore(b, a) ? { start: b, end: a } : { start: a, end: b };
}

/**
 * The range when a press-and-hold on a word is dragged to `current`: the word
 * itself while the finger stays inside it, otherwise the word plus everything
 * up to the finger, forwards or backwards, across rows.
 */
export function extendRange(wordStart: Cell, wordEnd: Cell, current: Cell): Range {
  if (isBefore(current, wordStart)) return { start: current, end: wordEnd };
  if (isBefore(wordEnd, current)) return { start: wordStart, end: current };
  return { start: wordStart, end: wordEnd };
}

/** The arguments of xterm's `select(col, row, length)` for a range. */
export function selectArgs(range: Range, cols: number): { col: number; row: number; length: number } {
  const { start, end } = range;
  return { col: start.col, row: start.row, length: (end.row - start.row) * cols + (end.col - start.col) + 1 };
}

/** `extendRange`, as the arguments of xterm's `select`. */
export function extendSelection(
  wordStart: Cell,
  wordEnd: Cell,
  current: Cell,
  cols: number,
): { col: number; row: number; length: number } {
  return selectArgs(extendRange(wordStart, wordEnd, current), cols);
}

/**
 * Where a selection handle's edge is, in pixels, and the cell it belongs to.
 * `left` is the left edge of the first column, `cellW` the width of a column.
 * A handle sits on a boundary between columns: the start handle on the left
 * edge of its cell, the end handle on the right edge of its cell.
 */
export function handleColumn(role: 'start' | 'end', edgeX: number, left: number, cellW: number, cols: number): number {
  const boundary = Math.max(0, Math.min(cols, Math.round((edgeX - left) / cellW)));
  return role === 'start' ? Math.min(boundary, cols - 1) : Math.max(boundary - 1, 0);
}

/**
 * Selected text as it should be copied: without the blanks that pad the end of
 * each terminal row, and without the empty rows after the last line of text
 * (selecting everything also selects the unused rows below the prompt).
 */
export function trimSelection(text: string): string {
  return text
    .split('\n')
    .map((line) => line.replace(/\s+$/, ''))
    .join('\n')
    .replace(/\n+$/, '');
}
