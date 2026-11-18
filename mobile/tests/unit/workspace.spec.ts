import { createPinia, setActivePinia } from 'pinia';
import { beforeEach, describe, expect, test } from 'vitest';

import { useWorkspaceStore } from '@/stores/workspace';

beforeEach(() => setActivePinia(createPinia()));

describe('workspace store', () => {
  test('opens tabs in order and ignores duplicates', () => {
    const w = useWorkspaceStore();
    w.open('c1', 'a');
    w.open('c1', 'b');
    w.open('c1', 'a');
    expect(w.list('c1')).toEqual(['a', 'b']);
    expect(w.list('other')).toEqual([]);
  });

  test('keeps tabs separate per computer', () => {
    const w = useWorkspaceStore();
    w.open('c1', 'a');
    w.open('c2', 'b');
    expect(w.list('c1')).toEqual(['a']);
    expect(w.list('c2')).toEqual(['b']);
  });

  test('closing a tab activates its left neighbor, or the first, or nothing', () => {
    const w = useWorkspaceStore();
    ['a', 'b', 'c'].forEach((id) => w.open('c1', id));
    expect(w.close('c1', 'c')).toBe('b');
    expect(w.close('c1', 'a')).toBe('b'); // no left neighbor: the new first
    expect(w.list('c1')).toEqual(['b']);
    expect(w.close('c1', 'b')).toBeNull();
    expect(w.list('c1')).toEqual([]);
  });

  test('closing an unknown tab changes nothing', () => {
    const w = useWorkspaceStore();
    w.open('c1', 'a');
    expect(w.close('c1', 'zzz')).toBe('a');
    expect(w.list('c1')).toEqual(['a']);
  });

  test('clear forgets a computer', () => {
    const w = useWorkspaceStore();
    w.open('c1', 'a');
    w.clear('c1');
    expect(w.list('c1')).toEqual([]);
  });
});
