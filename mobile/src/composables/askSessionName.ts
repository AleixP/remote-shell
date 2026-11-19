import { alertController } from '@ionic/vue';

import { MAX_SESSION_NAME, useSessionsStore } from '@/stores/sessions';

/** Asks for a new name for a session and saves it on this phone. */
export async function askSessionName(computerId: string, sessionId: string, original: string): Promise<void> {
  const sessions = useSessionsStore();
  const alert = await alertController.create({
    header: 'Rename terminal',
    message: original ? `Only changes the name on this phone. Leave it empty to use "${original}".` : undefined,
    inputs: [
      {
        name: 'name',
        type: 'text',
        value: sessions.label(computerId, sessionId),
        placeholder: original,
        attributes: { maxlength: MAX_SESSION_NAME },
      },
    ],
    buttons: [
      { text: 'Cancel', role: 'cancel' },
      {
        text: 'Save',
        handler: (values: { name?: string }) => {
          void sessions.rename(computerId, sessionId, values.name ?? '');
        },
      },
    ],
  });
  await alert.present();
}
