<template>
  <ion-page>
    <ion-header>
      <ion-toolbar>
        <ion-buttons slot="start">
          <ion-back-button default-href="/computers" />
        </ion-buttons>
        <ion-title>{{ computer?.name ?? 'Computer' }}</ion-title>
        <ion-buttons slot="end">
          <ion-button aria-label="New terminal" @click="chooseType">
            <ion-icon slot="icon-only" :icon="addOutline" />
          </ion-button>
        </ion-buttons>
      </ion-toolbar>
    </ion-header>

    <ion-content>
      <ion-refresher slot="fixed" @ion-refresh="onRefresh($event)">
        <ion-refresher-content />
      </ion-refresher>

      <ion-note v-if="error" color="danger" class="error" role="alert">
        {{ error }}
        <ion-button size="small" fill="clear" @click="refresh">Retry</ion-button>
      </ion-note>

      <div v-else-if="loaded && sessions.length === 0" class="empty">
        <ion-icon :icon="terminalOutline" />
        <h2>No terminals</h2>
        <p>Open a terminal on this computer.</p>
        <ion-button @click="chooseType">New terminal</ion-button>
      </div>

      <ion-list v-else>
        <ion-item-sliding v-for="s in sessions" :key="s.id">
          <ion-item button detail @click="open(s.id)">
            <ion-icon slot="start" :icon="terminalOutline" />
            <ion-label>
              <h2>{{ s.name }}</h2>
              <p>{{ s.type === 'persistent' ? 'Persistent' : 'Temporary' }}</p>
            </ion-label>
            <ion-badge slot="end" :color="s.status === 'running' ? 'success' : 'medium'">
              {{ s.status }}
            </ion-badge>
          </ion-item>
          <ion-item-options side="end">
            <ion-item-option color="danger" @click="confirmEnd(s)">End</ion-item-option>
          </ion-item-options>
        </ion-item-sliding>
      </ion-list>
    </ion-content>
  </ion-page>
</template>

<script setup lang="ts">
import {
  alertController,
  IonBackButton,
  IonBadge,
  IonButton,
  IonButtons,
  IonContent,
  IonHeader,
  IonIcon,
  IonItem,
  IonItemOption,
  IonItemOptions,
  IonItemSliding,
  IonLabel,
  IonList,
  IonNote,
  IonPage,
  IonRefresher,
  IonRefresherContent,
  IonTitle,
  IonToolbar,
  onIonViewWillEnter,
} from '@ionic/vue';
import { addOutline, terminalOutline } from 'ionicons/icons';
import { computed, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import { useNewTerminal } from '@/composables/useNewTerminal';
import { describeError } from '@/services/messages';
import { useComputersStore } from '@/stores/computers';
import { useSessionsStore } from '@/stores/sessions';
import { useWorkspaceStore } from '@/stores/workspace';
import type { Session } from '@/types/api';

const route = useRoute();
const router = useRouter();
const computers = useComputersStore();
const sessionsStore = useSessionsStore();
const workspace = useWorkspaceStore();

const computerId = computed(() => String(route.params.computerId));
const computer = computed(() => computers.computers.find((c) => c.id === computerId.value));
const sessions = computed(() => sessionsStore.byComputer[computerId.value] ?? []);
const loaded = ref(false);
const error = ref('');

async function refresh(): Promise<void> {
  error.value = '';
  try {
    await sessionsStore.refresh(computerId.value);
  } catch (err) {
    error.value = describeError(err);
  } finally {
    loaded.value = true;
  }
}

onIonViewWillEnter(async () => {
  if (!computers.loaded) await computers.load();
  if (!computer.value) {
    await router.replace('/computers');
    return;
  }
  await refresh();
});

async function onRefresh(ev: CustomEvent): Promise<void> {
  await refresh();
  (ev.target as HTMLIonRefresherElement).complete();
}

function open(sessionId: string): void {
  workspace.open(computerId.value, sessionId);
  void router.push(`/computers/${computerId.value}/sessions/${sessionId}`);
}

const { chooseType } = useNewTerminal(() => computerId.value, {
  onCreated: (session) => open(session.id),
  onError: (message) => {
    error.value = message;
  },
});

async function confirmEnd(s: Session): Promise<void> {
  const alert = await alertController.create({
    header: `End "${s.name}"?`,
    message: 'The shell and everything running in it will be terminated.',
    buttons: [
      { text: 'Cancel', role: 'cancel' },
      {
        text: 'End',
        role: 'destructive',
        handler: () => {
          void sessionsStore
            .close(computerId.value, s.id)
            .then(() => workspace.close(computerId.value, s.id))
            .catch((err) => {
              error.value = describeError(err);
            });
        },
      },
    ],
  });
  await alert.present();
}
</script>

<style scoped>
.error {
  display: block;
  padding: 16px;
}
.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 64px 24px;
  text-align: center;
}
.empty ion-icon {
  font-size: 56px;
  color: var(--ion-color-medium);
}
</style>
