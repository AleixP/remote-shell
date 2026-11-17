import { defineStore } from 'pinia';
import { ref } from 'vue';

import { useComputersStore } from './computers';

import type { CreateSessionRequest, Session } from '@/types/api';

export const useSessionsStore = defineStore('sessions', () => {
  const byComputer = ref<Record<string, Session[]>>({});
  const computers = useComputersStore();

  async function refresh(computerId: string): Promise<Session[]> {
    const sessions = await (await computers.apiFor(computerId)).listSessions();
    byComputer.value[computerId] = sessions;
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
