import { createPreferencesStore, createSecureStore, type KeyValueStore } from './storage';

/** Platform services, replaceable in tests. */
export interface Services {
  /** Credentials (Keychain / Keystore). */
  secure: KeyValueStore;
  /** Non-sensitive state. */
  prefs: KeyValueStore;
  fetch: (input: string, init?: RequestInit) => Promise<Response>;
  /** Name shown on the computer for this phone. */
  deviceName: () => Promise<string>;
}

async function defaultDeviceName(): Promise<string> {
  try {
    const { Device } = await import('@capacitor/device');
    const info = await Device.getInfo();
    const name = info.name ?? info.model;
    // The agent rejects control characters in device names.
    const clean = [...(name ?? '')].filter((ch) => ch >= ' ' && ch !== '\u007f').join('');
    return (clean || 'Phone').slice(0, 64);
  } catch {
    return 'Phone';
  }
}

let services: Services | null = null;

export function getServices(): Services {
  services ??= {
    secure: createSecureStore(),
    prefs: createPreferencesStore(),
    fetch: (input, init) => fetch(input, init),
    deviceName: defaultDeviceName,
  };
  return services;
}

/** Replaces the platform services (tests). */
export function setServices(s: Services | null): void {
  services = s;
}
