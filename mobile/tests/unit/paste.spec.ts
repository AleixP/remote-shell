import { describe, expect, test } from 'vitest';

import { checkPaste, MAX_PASTE_BYTES, sanitizePaste } from '@/services/paste';

describe('sanitizePaste', () => {
  test('keeps ordinary text, tabs and line breaks', () => {
    expect(sanitizePaste('ls -la\tfoo\nbar\r\nbaz ñ 日本語 😀')).toBe('ls -la\tfoo\nbar\r\nbaz ñ 日本語 😀');
  });

  test('removes escape sequences, so pasted text cannot end a bracketed paste early', () => {
    expect(sanitizePaste('safe\x1b[201~rm -rf ~')).toBe('safe[201~rm -rf ~');
    expect(sanitizePaste('\x1b[200~x')).toBe('[200~x');
  });

  test('removes other control characters, including C1 ones and DEL', () => {
    expect(sanitizePaste('a\x00b\x07c\x08d\x7fe\x9bf\x85g')).toBe('abcdefg');
  });
});

describe('checkPaste', () => {
  test('an empty clipboard is reported as empty', () => {
    expect(checkPaste('', true).empty).toBe(true);
    expect(checkPaste('\x1b\x07', true).empty).toBe(true);
  });

  test('a single line never needs confirmation, with or without a trailing newline', () => {
    expect(checkPaste('echo hi', false)).toMatchObject({ lines: 1, needsConfirm: false });
    expect(checkPaste('echo hi\n', false)).toMatchObject({ lines: 1, needsConfirm: false });
  });

  test('several lines need confirmation only when the shell lacks bracketed paste', () => {
    expect(checkPaste('a\nb\nc', false)).toMatchObject({ lines: 3, needsConfirm: true });
    expect(checkPaste('a\r\nb', false)).toMatchObject({ lines: 2, needsConfirm: true });
    expect(checkPaste('a\nb\nc', true)).toMatchObject({ lines: 3, needsConfirm: false });
  });

  test('blank trailing lines do not count', () => {
    expect(checkPaste('a\n\n\n', false)).toMatchObject({ lines: 1, needsConfirm: false });
  });

  test('text over the limit is refused, measured in bytes', () => {
    expect(checkPaste('a'.repeat(MAX_PASTE_BYTES), true).tooLarge).toBe(false);
    expect(checkPaste('a'.repeat(MAX_PASTE_BYTES + 1), true).tooLarge).toBe(true);
    // Each of these is 3 bytes in UTF-8, so a third of the characters is enough.
    expect(checkPaste('日'.repeat(Math.floor(MAX_PASTE_BYTES / 3) + 1), true).tooLarge).toBe(true);
  });

  test('returns the sanitized text', () => {
    expect(checkPaste('ok\x1b[201~', true).text).toBe('ok[201~');
  });
});
