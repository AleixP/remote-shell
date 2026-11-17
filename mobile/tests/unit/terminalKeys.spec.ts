import { describe, expect, test } from 'vitest';

import { applyCtrl, arrowSequence } from '@/services/terminalKeys';

describe('applyCtrl', () => {
  test.each([
    ['c', '\x03'],
    ['C', '\x03'],
    ['d', '\x04'],
    ['z', '\x1a'],
    ['l', '\x0c'],
    ['[', '\x1b'],
    ['\\', '\x1c'],
    [' ', '\x00'],
    ['?', '\x7f'],
  ])('Ctrl+%j is %j', (input, expected) => {
    expect(applyCtrl(input)).toBe(expected);
  });

  test('leaves text without a control equivalent unchanged', () => {
    expect(applyCtrl('1')).toBe('1');
    expect(applyCtrl('é')).toBe('é');
    expect(applyCtrl('abc')).toBe('abc'); // pasted text is not a key press
    expect(applyCtrl('')).toBe('');
  });
});

describe('arrowSequence', () => {
  test('uses CSI in normal mode and SS3 in application mode', () => {
    expect(arrowSequence('up', false)).toBe('\x1b[A');
    expect(arrowSequence('left', false)).toBe('\x1b[D');
    expect(arrowSequence('down', true)).toBe('\x1bOB');
    expect(arrowSequence('right', true)).toBe('\x1bOC');
  });
});
