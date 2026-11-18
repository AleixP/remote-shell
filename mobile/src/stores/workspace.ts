import { defineStore } from 'pinia';
import { ref } from 'vue';

/**
 * The terminals each computer has open as tabs. Tabs are only references to
 * sessions: closing a tab disconnects, it does not end the session.
 */
export const useWorkspaceStore = defineStore('workspace', () => {
  const tabs = ref<Record<string, string[]>>({});

  function list(computerId: string): string[] {
    return tabs.value[computerId] ?? [];
  }

  /** Opens a tab (no-op if it is already open). */
  function open(computerId: string, sessionId: string): void {
    const current = list(computerId);
    if (!current.includes(sessionId)) {
      tabs.value[computerId] = [...current, sessionId];
    }
  }

  /**
   * Closes a tab and returns the session that should become active: the
   * neighbor to the left (or the first one), or null when no tabs are left.
   */
  function close(computerId: string, sessionId: string): string | null {
    const current = list(computerId);
    const index = current.indexOf(sessionId);
    if (index === -1) return current[0] ?? null;
    const next = current.filter((id) => id !== sessionId);
    tabs.value[computerId] = next;
    return next[Math.max(0, index - 1)] ?? null;
  }

  /** Forgets every tab of a computer (for example when it is removed). */
  function clear(computerId: string): void {
    delete tabs.value[computerId];
  }

  return { tabs, list, open, close, clear };
});
