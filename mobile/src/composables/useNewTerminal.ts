import { actionSheetController } from '@ionic/vue';

import { describeError } from '@/services/messages';
import { useSessionsStore } from '@/stores/sessions';
import { useWorkspaceStore } from '@/stores/workspace';
import type { Session, SessionType } from '@/types/api';

/** Asks which kind of terminal to open, creates it and opens its tab. */
export function useNewTerminal(
  computerId: () => string,
  callbacks: { onCreated: (session: Session) => void; onError: (message: string) => void },
): { chooseType: () => Promise<void> } {
  const sessions = useSessionsStore();
  const workspace = useWorkspaceStore();

  async function create(type: SessionType): Promise<void> {
    try {
      const session = await sessions.create(computerId(), { type });
      workspace.open(computerId(), session.id);
      callbacks.onCreated(session);
    } catch (err) {
      callbacks.onError(describeError(err));
    }
  }

  async function chooseType(): Promise<void> {
    const sheet = await actionSheetController.create({
      header: 'New terminal',
      buttons: [
        { text: 'Persistent (keeps running when you leave)', handler: () => void create('persistent') },
        { text: 'Temporary (ends after inactivity)', handler: () => void create('temporary') },
        { text: 'Cancel', role: 'cancel' },
      ],
    });
    await sheet.present();
  }

  return { chooseType };
}
