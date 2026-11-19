import { defineStore } from 'pinia';
import { ref } from 'vue';

import { useComputersStore } from './computers';
import { useWorkspaceStore } from './workspace';

import { getServices } from '@/services/container';
import { hasEnded } from '@/services/messages';
import type { CreateSessionRequest, Session } from '@/types/api';

const ALIASES_KEY = 'session_aliases';
export const MAX_SESSION_NAME = 64;

/**
 * The first name not yet in `used`: `base`, then `base 2`, `base 3`...
 * Two terminals opened with the same shell would otherwise look identical.
 */
export function uniqueName(base: string, used: string[]): string {
  const taken = new Set(used);
  if (!taken.has(base)) return base;
  for (let n = 2; ; n++) {
    const candidate = `${base} ${n}`;
    if (!taken.has(candidate)) return candidate;
  }
}

export const useSessionsStore = defineStore('sessions', () => {
  const byComputer = ref<Record<string, Session[]>>({});
  /** Names chosen on this phone, by computer and then session id. */
  const aliases = ref<Record<string, Record<string, string>>>({});
  const aliasesLoaded = ref(false);
  const computers = useComputersStore();
  const workspace = useWorkspaceStore();

  async function persistAliases(): Promise<void> {
    await getServices().prefs.set(ALIASES_KEY, JSON.stringify(aliases.value));
  }

  async function loadAliases(): Promise<void> {
    if (aliasesLoaded.value) return;
    aliasesLoaded.value = true;
    try {
      const raw = await getServices().prefs.get(ALIASES_KEY);
      const parsed: unknown = raw ? JSON.parse(raw) : {};
      const clean: Record<string, Record<string, string>> = {};
      if (parsed && typeof parsed === 'object') {
        for (const [computerId, map] of Object.entries(parsed)) {
          if (!map || typeof map !== 'object') continue;
          const names = Object.entries(map).filter(([, v]) => typeof v === 'string' && v);
          if (names.length) clean[computerId] = Object.fromEntries(names) as Record<string, string>;
        }
      }
      // Keep anything set while loading.
      for (const [c, map] of Object.entries(aliases.value)) clean[c] = { ...clean[c], ...map };
      aliases.value = clean;
    } catch {
      /* unreadable aliases are just lost: sessions fall back to their own names */
    }
  }

  /** What to call a session in the interface. */
  function label(computerId: string, sessionId: string): string {
    const alias = aliases.value[computerId]?.[sessionId];
    if (alias) return alias;
    return byComputer.value[computerId]?.find((s) => s.id === sessionId)?.name ?? 'Terminal';
  }

  function setAlias(computerId: string, sessionId: string, alias: string | null): void {
    const map = { ...aliases.value[computerId] };
    if (alias) map[sessionId] = alias;
    else delete map[sessionId];
    if (Object.keys(map).length) aliases.value[computerId] = map;
    else delete aliases.value[computerId];
  }

  /** Names a session on this phone. An empty name restores the one the computer gave it. */
  async function rename(computerId: string, sessionId: string, alias: string): Promise<void> {
    await loadAliases();
    const session = byComputer.value[computerId]?.find((s) => s.id === sessionId);
    const clean = alias.trim().slice(0, MAX_SESSION_NAME);
    setAlias(computerId, sessionId, clean && clean !== session?.name ? clean : null);
    await persistAliases();
  }

  /** Names of the sessions still alive, which a new one must not repeat. */
  function liveLabels(computerId: string): string[] {
    return (byComputer.value[computerId] ?? []).filter((s) => !hasEnded(s)).map((s) => label(computerId, s.id));
  }

  async function refresh(computerId: string): Promise<Session[]> {
    await loadAliases();
    const sessions = await (await computers.apiFor(computerId)).listSessions();
    byComputer.value[computerId] = sessions;
    // A tab whose session ended (or vanished) has nothing left to show.
    const running = new Set(sessions.filter((x) => !hasEnded(x)).map((x) => x.id));
    for (const id of workspace.list(computerId)) {
      if (!running.has(id)) workspace.close(computerId, id);
    }
    // Forget the names of sessions the computer no longer lists.
    const known = new Set(sessions.map((x) => x.id));
    const stale = Object.keys(aliases.value[computerId] ?? {}).filter((id) => !known.has(id));
    if (stale.length) {
      for (const id of stale) setAlias(computerId, id, null);
      await persistAliases();
    }
    return sessions;
  }

  async function create(computerId: string, req: CreateSessionRequest): Promise<Session> {
    await loadAliases();
    const used = liveLabels(computerId);
    const session = await (await computers.apiFor(computerId)).createSession(req);
    byComputer.value[computerId] = [...(byComputer.value[computerId] ?? []), session];
    // "zsh", then "zsh 2", "zsh 3"...: tell terminals of the same shell apart.
    const name = uniqueName(session.name, used);
    if (name !== session.name) {
      setAlias(computerId, session.id, name);
      await persistAliases();
    }
    return session;
  }

  async function close(computerId: string, sessionId: string): Promise<void> {
    await (await computers.apiFor(computerId)).closeSession(sessionId);
    byComputer.value[computerId] = (byComputer.value[computerId] ?? []).filter((s) => s.id !== sessionId);
    if (aliases.value[computerId]?.[sessionId]) {
      setAlias(computerId, sessionId, null);
      await persistAliases();
    }
  }

  return { byComputer, aliases, label, rename, loadAliases, refresh, create, close };
});
