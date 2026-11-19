<template>
  <ion-page>
    <ion-header>
      <ion-toolbar>
        <ion-buttons slot="start">
          <ion-back-button default-href="/computers" />
        </ion-buttons>
        <ion-title>{{ tabName(active) }}</ion-title>
        <ion-buttons slot="end">
          <ion-button aria-label="New terminal" @click="chooseType">
            <ion-icon slot="icon-only" :icon="addOutline" />
          </ion-button>
          <ion-button aria-label="Rename terminal" @click="renameActive">
            <ion-icon slot="icon-only" :icon="createOutline" />
          </ion-button>
          <ion-button aria-label="End session" @click="confirmEnd">
            <ion-icon slot="icon-only" :icon="trashOutline" />
          </ion-button>
        </ion-buttons>
      </ion-toolbar>
      <div class="tabbar" role="tablist" aria-label="Open terminals">
        <button
          v-for="id in tabIds"
          :key="id"
          type="button"
          role="tab"
          class="tab"
          :class="{ active: id === active }"
          :aria-selected="id === active"
          @click="active = id"
        >
          <span class="dot" :class="dotClass(id)" />
          <span class="name">{{ tabName(id) }}</span>
          <span class="close" role="button" :aria-label="`Close ${tabName(id)}`" @click.stop="closeTab(id)"> × </span>
        </button>
      </div>
    </ion-header>
    <ion-content :scroll-y="false">
      <!-- Every open terminal stays mounted and connected: switching tabs only
           hides one and shows another, so each keeps its own screen. -->
      <TerminalView
        v-for="id in tabIds"
        v-show="id === active"
        :key="id"
        :computer-id="computerId"
        :session-id="id"
        :active="id === active"
        @state="states[id] = $event"
        @exit="onExit(id, $event)"
        @failed="onFailed(id, $event)"
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
  IonPage,
  IonTitle,
  IonToolbar,
  toastController,
} from '@ionic/vue';
import { addOutline, createOutline, trashOutline } from 'ionicons/icons';
import { computed, reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import TerminalView from '@/components/TerminalView.vue';
import { askSessionName } from '@/composables/askSessionName';
import { useNewTerminal } from '@/composables/useNewTerminal';
import type { ConnectionState } from '@/services/terminalConnection';
import { useSessionsStore } from '@/stores/sessions';
import { useWorkspaceStore } from '@/stores/workspace';

const route = useRoute();
const router = useRouter();
const sessions = useSessionsStore();
const workspace = useWorkspaceStore();

const computerId = computed(() => String(route.params.computerId));
const active = ref(String(route.params.sessionId));
workspace.open(computerId.value, active.value);

const tabIds = computed(() => workspace.list(computerId.value));
const states = reactive<Record<string, ConnectionState>>({});
const exited = reactive<Record<string, boolean>>({});

function tabName(id: string): string {
  return sessions.label(computerId.value, id);
}

function dotClass(id: string): string {
  if (exited[id]) return 'off';
  return states[id] === 'ready' ? 'ok' : states[id] === 'reconnecting' ? 'warn' : 'wait';
}

async function toast(message: string): Promise<void> {
  await (await toastController.create({ message, duration: 3000, position: 'top' })).present();
}

function closeTab(id: string): void {
  const next = workspace.close(computerId.value, id);
  delete states[id];
  delete exited[id];
  if (id !== active.value) return;
  if (next) active.value = next;
  else void router.back();
}

const { chooseType } = useNewTerminal(() => computerId.value, {
  onCreated: (session) => {
    active.value = session.id;
  },
  onError: (message) => void toast(message),
});

async function renameActive(): Promise<void> {
  const original = sessions.byComputer[computerId.value]?.find((s) => s.id === active.value)?.name ?? '';
  await askSessionName(computerId.value, active.value, original);
}

async function confirmEnd(): Promise<void> {
  const id = active.value;
  const alert = await alertController.create({
    header: `End "${tabName(id)}"?`,
    message: 'The shell and everything running in it will be terminated.',
    buttons: [
      { text: 'Cancel', role: 'cancel' },
      { text: 'End session', role: 'destructive', handler: () => void endSession(id) },
    ],
  });
  await alert.present();
}

async function endSession(id: string): Promise<void> {
  try {
    await sessions.close(computerId.value, id);
    closeTab(id);
  } catch {
    await toast('Could not end the session.');
  }
}

async function onExit(id: string, code: number): Promise<void> {
  exited[id] = true;
  await toast(`"${tabName(id)}" exited (code ${code}).`);
}

async function onFailed(id: string, message: string): Promise<void> {
  await toast(message || 'The session is no longer available.');
  closeTab(id);
}
</script>

<style scoped>
.tabbar {
  display: flex;
  gap: 4px;
  padding: 4px;
  overflow-x: auto;
  background: #1c1c1e;
}
.tab {
  display: flex;
  flex: 0 0 auto;
  gap: 6px;
  align-items: center;
  max-width: 180px;
  min-height: 36px;
  padding: 0 4px 0 10px;
  font: inherit;
  font-size: 14px;
  color: #d1d1d6;
  background: #2c2c2e;
  border: 0;
  border-radius: 8px;
}
.tab.active {
  color: #fff;
  background: #48484a;
}
.name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.dot {
  flex: 0 0 auto;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #8e8e93;
}
.dot.ok {
  background: #30d158;
}
.dot.warn {
  background: #ffd60a;
}
.dot.off {
  background: #636366;
}
.close {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  width: 28px;
  height: 28px;
  font-size: 18px;
  line-height: 1;
}
</style>
