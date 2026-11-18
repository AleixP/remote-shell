import { describe, expect, test, vi } from 'vitest';

import { createSecureStore } from '@/services/storage';

// A Capacitor plugin is a Proxy: every property exists and is a function, so
// the object looks like a thenable. Returning it from a promise/async function
// would make JavaScript call its "then" and hang forever.
const calls: string[] = [];
const store = new Map<string, string>();
const plugin = new Proxy(
  {},
  {
    get: (_t, name: string) => {
      if (name === 'setKeyPrefix' || name === 'setSynchronize') {
        return async (...args: unknown[]) => void calls.push(`${name}:${args.join(',')}`);
      }
      if (name === 'get') return async (key: string) => store.get(key) ?? null;
      if (name === 'set') return async (key: string, value: string) => void store.set(key, value);
      if (name === 'remove') return async (key: string) => store.delete(key);
      return () => new Promise(() => {}); // anything else (including "then") never settles
    },
  },
);

vi.mock('@capacitor/core', () => ({ Capacitor: { isNativePlatform: () => true } }));
vi.mock('@aparajita/capacitor-secure-storage', () => ({ SecureStorage: plugin }));

describe('native secure store', () => {
  test('reads, writes and removes without hanging on the thenable plugin', async () => {
    const secure = createSecureStore();
    await secure.set('token_1', 'secret');
    expect(await secure.get('token_1')).toBe('secret');
    await secure.remove('token_1');
    expect(await secure.get('token_1')).toBeNull();
  });

  test('configures the plugin once: own prefix and no iCloud sync', async () => {
    calls.length = 0;
    const secure = createSecureStore();
    await secure.set('a', '1');
    await secure.set('b', '2');
    expect(calls).toEqual(['setKeyPrefix:remoteshell_', 'setSynchronize:false']);
  });
});
