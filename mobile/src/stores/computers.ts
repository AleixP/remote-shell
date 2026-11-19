import { defineStore } from 'pinia';
import { ref } from 'vue';

import { AgentApi } from '@/services/api';
import { getServices } from '@/services/container';
import { AgentError } from '@/services/errors';
import type { PairingInfo } from '@/services/pairing';
import { withTimeout } from '@/services/timeout';
import { useWorkspaceStore } from '@/stores/workspace';

/** A paired computer. Its token lives in secure storage, never here. */
export interface Computer {
  /** The device id the agent assigned to this phone; unique per computer. */
  id: string;
  /** The name the computer reported when it was paired. */
  name: string;
  /** A name chosen on this phone; it is shown instead of `name`. */
  alias?: string;
  baseUrl: string;
  pairedAt: string;
}

export const MAX_NAME_LENGTH = 64;

/** What to call a computer in the interface. */
export function computerLabel(c: Pick<Computer, 'name' | 'alias'>): string {
  return c.alias || c.name;
}

export type ComputerStatus = 'unknown' | 'checking' | 'online' | 'offline' | 'revoked';

const COMPUTERS_KEY = 'computers';
const tokenKey = (id: string) => `token_${id}`;

function isComputer(v: unknown): v is Computer {
  const c = v as Computer;
  return (
    !!c &&
    typeof c.id === 'string' &&
    typeof c.name === 'string' &&
    (c.alias === undefined || typeof c.alias === 'string') &&
    typeof c.baseUrl === 'string' &&
    c.baseUrl.startsWith('https://')
  );
}

export const useComputersStore = defineStore('computers', () => {
  const computers = ref<Computer[]>([]);
  const status = ref<Record<string, ComputerStatus>>({});
  const loaded = ref(false);
  // Tokens are cached in memory after the first read from secure storage.
  const tokens = new Map<string, string>();

  async function persist(): Promise<void> {
    await getServices().prefs.set(COMPUTERS_KEY, JSON.stringify(computers.value));
  }

  async function load(): Promise<void> {
    const raw = await getServices().prefs.get(COMPUTERS_KEY);
    try {
      const parsed: unknown = raw ? JSON.parse(raw) : [];
      computers.value = Array.isArray(parsed) ? parsed.filter(isComputer) : [];
    } catch {
      computers.value = [];
    }
    loaded.value = true;
  }

  function find(id: string): Computer {
    const c = computers.value.find((x) => x.id === id);
    if (!c) throw new Error('Unknown computer');
    return c;
  }

  /** An API client for a paired computer, with its token loaded. */
  async function apiFor(id: string): Promise<AgentApi> {
    const computer = find(id);
    if (!tokens.has(id)) {
      const token = await getServices().secure.get(tokenKey(id));
      if (token) tokens.set(id, token);
    }
    return new AgentApi(computer.baseUrl, () => tokens.get(id), getServices().fetch);
  }

  /** Pairs this phone with a computer using the code from its QR. */
  async function pair(info: PairingInfo): Promise<Computer> {
    const services = getServices();
    const api = new AgentApi(info.baseUrl, () => undefined, services.fetch);
    const result = await api.pair(info.code, await services.deviceName());

    // Store the credential first: if this fails the computer is not listed, and
    // the device just registered on the agent is revoked so it does not linger.
    try {
      await withTimeout(
        services.secure.set(tokenKey(result.device_id), result.access_token),
        10_000,
        'Saving the credential on this phone',
      );
    } catch (err) {
      const authed = new AgentApi(info.baseUrl, () => result.access_token, services.fetch);
      await authed.revokeDevice(result.device_id).catch(() => undefined);
      throw err;
    }
    tokens.set(result.device_id, result.access_token);

    // Pairing the same computer again replaces the old entry, keeping its alias.
    const previous = computers.value.find((c) => c.baseUrl === info.baseUrl);
    for (const old of computers.value.filter((c) => c.baseUrl === info.baseUrl)) {
      await forget(old.id);
    }
    const computer: Computer = {
      id: result.device_id,
      name: info.name,
      ...(previous?.alias ? { alias: previous.alias } : {}),
      baseUrl: info.baseUrl,
      pairedAt: new Date().toISOString(),
    };
    computers.value.push(computer);
    await persist();
    return computer;
  }

  /** Sets the name shown for a computer on this phone. An empty name restores the original. */
  async function rename(id: string, alias: string): Promise<void> {
    const computer = find(id);
    const clean = alias.trim().slice(0, MAX_NAME_LENGTH);
    if (clean && clean !== computer.name) computer.alias = clean;
    else delete computer.alias;
    await persist();
  }

  async function forget(id: string): Promise<void> {
    tokens.delete(id);
    await getServices().secure.remove(tokenKey(id));
    computers.value = computers.value.filter((c) => c.id !== id);
    delete status.value[id];
  }

  /**
   * Removes a computer from the app. With `revoke`, this phone is also revoked
   * on the agent first (removing local data alone does not revoke access).
   * Returns false when revocation was requested but could not be completed.
   */
  async function remove(id: string, opts: { revoke: boolean }): Promise<boolean> {
    let revoked = true;
    if (opts.revoke) {
      try {
        await (await apiFor(id)).revokeDevice(id);
      } catch (err) {
        // Already revoked on the agent counts as done.
        revoked = err instanceof AgentError && (err.code === 'DEVICE_REVOKED' || err.code === 'DEVICE_NOT_FOUND');
      }
    }
    await forget(id);
    useWorkspaceStore().clear(id);
    await persist();
    return revoked;
  }

  /** Checks that a computer is reachable and still authorizes this phone. */
  async function checkStatus(id: string): Promise<ComputerStatus> {
    status.value[id] = 'checking';
    let result: ComputerStatus;
    try {
      await (await apiFor(id)).listSessions();
      result = 'online';
    } catch (err) {
      // No valid credential (revoked, or the token is missing) means the phone
      // must be paired again; anything else is a connectivity problem.
      const noAccess = err instanceof AgentError && (err.code === 'DEVICE_REVOKED' || err.code === 'UNAUTHORIZED');
      result = noAccess ? 'revoked' : 'offline';
    }
    status.value[id] = result;
    return result;
  }

  return { computers, status, loaded, load, apiFor, pair, rename, remove, checkStatus };
});
