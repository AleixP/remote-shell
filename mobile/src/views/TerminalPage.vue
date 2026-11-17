<template>
  <ion-page>
    <ion-header>
      <ion-toolbar>
        <ion-buttons slot="start">
          <ion-back-button default-href="/home" />
        </ion-buttons>
        <ion-title>
          {{ title }}
          <ion-note v-if="state === 'reconnecting'" color="warning">· reconnecting</ion-note>
        </ion-title>
        <ion-buttons slot="end">
          <ion-button aria-label="End session" @click="confirmEnd">
            <ion-icon slot="icon-only" :icon="trashOutline" />
          </ion-button>
        </ion-buttons>
      </ion-toolbar>
    </ion-header>
    <ion-content :scroll-y="false">
      <TerminalView
        :computer-id="computerId"
        :session-id="sessionId"
        @state="state = $event"
        @exit="onExit"
        @failed="onFailed"
      />
    </ion-content>
  </ion-page>
</template>

<script setup lang="ts">
import {
  alertController,
  IonBackButton,
  IonButton,
  IonButtons,
  IonContent,
  IonHeader,
  IonIcon,
  IonNote,
  IonPage,
  IonTitle,
  IonToolbar,
  toastController,
} from '@ionic/vue';
import { trashOutline } from 'ionicons/icons';
import { computed, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import TerminalView from '@/components/TerminalView.vue';
import type { ConnectionState } from '@/services/terminalConnection';
import { useSessionsStore } from '@/stores/sessions';

const route = useRoute();
const router = useRouter();
const sessions = useSessionsStore();

const computerId = computed(() => String(route.params.computerId));
const sessionId = computed(() => String(route.params.sessionId));
const state = ref<ConnectionState>('connecting');

const title = computed(
  () => sessions.byComputer[computerId.value]?.find((s) => s.id === sessionId.value)?.name ?? 'Terminal',
);

async function toast(message: string): Promise<void> {
  await (await toastController.create({ message, duration: 3000, position: 'top' })).present();
}

async function confirmEnd(): Promise<void> {
  const alert = await alertController.create({
    header: 'End session?',
    message: 'The shell and everything running in it will be terminated.',
    buttons: [
      { text: 'Cancel', role: 'cancel' },
      { text: 'End session', role: 'destructive', handler: () => void endSession() },
    ],
  });
  await alert.present();
}

async function endSession(): Promise<void> {
  try {
    await sessions.close(computerId.value, sessionId.value);
    await router.back();
  } catch {
    await toast('Could not end the session.');
  }
}

async function onExit(code: number): Promise<void> {
  await toast(`The process exited (code ${code}).`);
}

async function onFailed(message: string): Promise<void> {
  await toast(message || 'The session is no longer available.');
  await router.back();
}
</script>
