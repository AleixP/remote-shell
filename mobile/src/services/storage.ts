import type { SecureStorage } from '@aparajita/capacitor-secure-storage';
import { Capacitor } from '@capacitor/core';

/** Minimal async key-value store. */
export interface KeyValueStore {
  get(key: string): Promise<string | null>;
  set(key: string, value: string): Promise<void>;
  remove(key: string): Promise<void>;
}

/** In-memory store: nothing survives a restart. Used on the web and in tests. */
export class MemoryStore implements KeyValueStore {
  private readonly data = new Map<string, string>();
  async get(key: string): Promise<string | null> {
    return this.data.get(key) ?? null;
  }
  async set(key: string, value: string): Promise<void> {
    this.data.set(key, value);
  }
  async remove(key: string): Promise<void> {
    this.data.delete(key);
  }
}

/**
 * Store for credentials: the iOS Keychain / Android Keystore. Credentials are
 * never written to localStorage, so outside a native app they only live in
 * memory (development in a browser).
 */
export function createSecureStore(): KeyValueStore {
  if (!Capacitor.isNativePlatform()) {
    return new MemoryStore();
  }
  let configured: Promise<void> | null = null;

  // Capacitor plugins are Proxies that look like thenables (every property,
  // "then" included, exists). Returning one from a promise or async function
  // makes JavaScript call its "then" and hang forever, so the plugin is only
  // ever handed to a callback and only plain values leave this function.
  async function withPlugin<T>(use: (plugin: typeof SecureStorage) => Promise<T>): Promise<T> {
    const { SecureStorage: plugin } = await import('@aparajita/capacitor-secure-storage');
    configured ??= (async () => {
      await plugin.setKeyPrefix('remoteshell_');
      // Keep tokens on this device: no iCloud Keychain synchronization.
      await plugin.setSynchronize(false);
    })();
    await configured;
    return use(plugin);
  }

  return {
    async get(key) {
      const value = await withPlugin((p) => p.get(key, false, false));
      return typeof value === 'string' ? value : null;
    },
    async set(key, value) {
      await withPlugin((p) => p.set(key, value, false, false));
    },
    async remove(key) {
      await withPlugin((p) => p.remove(key, false));
    },
  };
}

/** Store for non-sensitive app state (the list of computers, preferences). */
export function createPreferencesStore(): KeyValueStore {
  return {
    async get(key) {
      const { Preferences } = await import('@capacitor/preferences');
      return (await Preferences.get({ key })).value;
    },
    async set(key, value) {
      const { Preferences } = await import('@capacitor/preferences');
      await Preferences.set({ key, value });
    },
    async remove(key) {
      const { Preferences } = await import('@capacitor/preferences');
      await Preferences.remove({ key });
    },
  };
}
