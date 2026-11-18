import { defineStore } from 'pinia';
import { ref } from 'vue';

import { useComputersStore } from './computers';
import { useWorkspaceStore } from './workspace';

import { hasEnded } from '@/services/messages';
import type { CreateSessionRequest, Session } from '@/types/api';

export const useSessionsStore = defineStore('sessions', () => {
  const byComputer = ref<Record<string, Session[]>>({});
  const computers = useComputersStore();
  const workspace = useWorkspaceStore();

  async function refresh(computerId: string): Promise<Session[]> {
    const sessions = await (await computers.apiFor(computerId)).listSessions();
    byComputer.value[computerId] = sessions;
    // A tab whose session ended (or vanished) has nothing left to show.
    const running = new Set(sessions.filter((x) => !hasEnded(x)).map((x) => x.id));
    for (const id of workspace.list(computerId)) {
      if (!running.has(id)) workspace.close(computerId, id);
    }
    return sessions;
  }

  async function create(computerId: string, req: CreateSessionRequest): Promise<Session> {
    const session = await (await computers.apiFor(computerId)).createSession(req);
    byComputer.value[computerId] = [...(byComputer.value[computerId] ?? []), session];
    return session;
  }

  async function close(computerId: string, sessionId: string): Promise<void> {
    await (await computers.apiFor(computerId)).closeSession(sessionId);
    byComputer.value[computerId] = (byComputer.value[computerId] ?? []).filter((s) => s.id !== sessionId);
  }

  return { byComputer, refresh, create, close };
});
