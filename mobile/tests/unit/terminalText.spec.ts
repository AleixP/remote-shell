import { describe, expect, test } from 'vitest';

import { type BufferLike, extendSelection, readLineAt, readRows, wordAt } from '@/services/terminalText';

/** A buffer from rows; a row starting with "~" continues the previous one (wrapped). */
function buffer(rows: string[]): BufferLike {
  return {
    length: rows.length,
    getLine: (y) => {
      const raw = rows[y];
      if (raw === undefined) return undefined;
      const wrapped = raw.startsWith('~');
      const text = wrapped ? raw.slice(1) : raw;
      return { isWrapped: wrapped, translateToString: (trim) => (trim ? text.trimEnd() : text) };
    },
  };
}

describe('wordAt', () => {
  const line = 'git commit  -m "fix"   ';
  test('finds the run of non-blank characters around a column', () => {
    expect(wordAt(line, 5)).toEqual({ start: 4, length: 6 }); // "commit"
    expect(wordAt(line, 4)).toEqual({ start: 4, length: 6 });
    expect(wordAt(line, 9)).toEqual({ start: 4, length: 6 });
    expect(wordAt(line, 0)).toEqual({ start: 0, length: 3 });
  });
  test('treats paths and URLs as one word', () => {
    expect(wordAt('open https://a.b/c?d=1 now', 10)).toEqual({ start: 5, length: 17 });
  });
  test('returns null on blanks and outside the line', () => {
    expect(wordAt(line, 3)).toBeNull();
    expect(wordAt(line, 10)).toBeNull();
    expect(wordAt(line, -1)).toBeNull();
    expect(wordAt(line, 99)).toBeNull();
    expect(wordAt('', 0)).toBeNull();
  });
});

describe('readRows', () => {
  test('joins rows with line breaks and trims trailing blanks', () => {
    expect(readRows(buffer(['$ ls   ', 'a  b', '$ ']), 0, 2)).toBe('$ ls\na  b\n$');
  });
  test('drops empty rows at the end only', () => {
    expect(readRows(buffer(['a', '', 'b', '', '']), 0, 4)).toBe('a\n\nb');
  });
  test('joins wrapped rows into one line', () => {
    expect(readRows(buffer(['aaaa', '~bbbb', '~cc', 'next']), 0, 3)).toBe('aaaabbbbcc\nnext');
  });
  test('keeps a space that falls at the edge of a wrapped row', () => {
    expect(readRows(buffer(['hello ', '~world']), 0, 1)).toBe('hello world');
  });
  test('clamps the range to the buffer', () => {
    expect(readRows(buffer(['a', 'b']), -5, 99)).toBe('a\nb');
    expect(readRows(buffer([]), 0, 10)).toBe('');
  });
});

describe('readLineAt', () => {
  const buf = buffer(['first', 'long ', '~line ', '~here', 'last']);
  test('returns the whole logical line from any of its rows', () => {
    expect(readLineAt(buf, 1)).toBe('long line here');
    expect(readLineAt(buf, 2)).toBe('long line here');
    expect(readLineAt(buf, 3)).toBe('long line here');
  });
  test('returns a single row when it is not wrapped', () => {
    expect(readLineAt(buf, 0)).toBe('first');
    expect(readLineAt(buf, 4)).toBe('last');
  });
});

describe('extendSelection', () => {
  const COLS = 80;
  const word = { start: { col: 10, row: 5 }, end: { col: 14, row: 5 } };
  const sel = (col: number, row: number) => extendSelection(word.start, word.end, { col, row }, COLS);

  test('keeps just the word while the finger stays inside it', () => {
    expect(sel(10, 5)).toEqual({ col: 10, row: 5, length: 5 });
    expect(sel(12, 5)).toEqual({ col: 10, row: 5, length: 5 });
    expect(sel(14, 5)).toEqual({ col: 10, row: 5, length: 5 });
  });
  test('extends forward on the same row', () => {
    expect(sel(20, 5)).toEqual({ col: 10, row: 5, length: 11 });
  });
  test('extends backward on the same row', () => {
    expect(sel(3, 5)).toEqual({ col: 3, row: 5, length: 12 });
  });
  test('extends down across rows', () => {
    expect(sel(2, 7)).toEqual({ col: 10, row: 5, length: 2 * COLS + (2 - 10) + 1 });
  });
  test('extends up across rows', () => {
    expect(sel(40, 3)).toEqual({ col: 40, row: 3, length: 2 * COLS + (14 - 40) + 1 });
  });
  test('a finger on the row above but to the right still extends backwards', () => {
    const s = sel(79, 4);
    expect(s).toEqual({ col: 79, row: 4, length: COLS + (14 - 79) + 1 });
    expect(s.length).toBeGreaterThan(0);
  });
});
