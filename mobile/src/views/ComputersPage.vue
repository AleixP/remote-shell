<template>
  <ion-page>
    <ion-header>
      <ion-toolbar>
        <ion-title>Computers</ion-title>
        <ion-buttons slot="end">
          <ion-button aria-label="Pair a computer" router-link="/pair">
            <ion-icon slot="icon-only" :icon="addOutline" />
          </ion-button>
        </ion-buttons>
      </ion-toolbar>
    </ion-header>

    <ion-content>
      <ion-refresher slot="fixed" @ion-refresh="refresh($event)">
        <ion-refresher-content />
      </ion-refresher>

      <div v-if="store.loaded && store.computers.length === 0" class="empty">
        <ion-icon :icon="desktopOutline" />
        <h2>No computers yet</h2>
        <p>Run <code>remote-shell pair</code> on a computer and scan the QR code it shows.</p>
        <ion-button router-link="/pair">Pair a computer</ion-button>
      </div>

      <ion-list v-else>
        <ion-item-sliding v-for="c in store.computers" :key="c.id">
          <ion-item v-long-press="() => rename(c)" button detail @click="router.push(`/computers/${c.id}`)">
            <ion-label>
              <h2>{{ computerLabel(c) }}</h2>
              <p v-if="c.alias" class="reported">{{ c.name }}</p>
              <p>{{ c.baseUrl.replace('https://', '') }}</p>
            </ion-label>
            <ion-badge slot="end" :color="statusColor(store.status[c.id])">
              {{ statusText(store.status[c.id]) }}
            </ion-badge>
          </ion-item>
          <ion-item-options side="end">
            <ion-item-option color="danger" @click="confirmRemove(c)">Remove</ion-item-option>
          </ion-item-options>
        </ion-item-sliding>
      </ion-list>
    </ion-content>
  </ion-page>
</template>

<script setup lang="ts">
import {
  alertController,
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
  IonPage,
  IonRefresher,
  IonRefresherContent,
  IonTitle,
  IonToolbar,
  onIonViewWillEnter,
  toastController,
} from '@ionic/vue';
import { addOutline, desktopOutline } from 'ionicons/icons';
import { useRouter } from 'vue-router';

import { longPress } from '@/directives/longPress';
import {
  type Computer,
  type ComputerStatus,
  computerLabel,
  MAX_NAME_LENGTH,
  useComputersStore,
} from '@/stores/computers';

const router = useRouter();
const vLongPress = longPress;
const store = useComputersStore();

const STATUS_TEXT: Record<ComputerStatus, string> = {
  unknown: '',
  checking: '…',
  online: 'Online',
  offline: 'Offline',
  revoked: 'Access revoked',
};
const STATUS_COLOR: Record<ComputerStatus, string> = {
  unknown: 'light',
  checking: 'light',
  online: 'success',
  offline: 'medium',
  revoked: 'danger',
};

const statusText = (s: ComputerStatus | undefined) => STATUS_TEXT[s ?? 'unknown'];
const statusColor = (s: ComputerStatus | undefined) => STATUS_COLOR[s ?? 'unknown'];

async function checkAll(): Promise<void> {
  await Promise.all(store.computers.map((c) => store.checkStatus(c.id)));
}

onIonViewWillEnter(async () => {
  if (!store.loaded) await store.load();
  await checkAll();
});

async function refresh(ev: CustomEvent): Promise<void> {
  await checkAll();
  (ev.target as HTMLIonRefresherElement).complete();
}

async function rename(c: Computer): Promise<void> {
  const alert = await alertController.create({
    header: 'Rename computer',
    message: `Only changes the name on this phone. Leave it empty to use "${c.name}".`,
    inputs: [
      {
        name: 'alias',
        type: 'text',
        value: c.alias ?? '',
        placeholder: c.name,
        attributes: { maxlength: MAX_NAME_LENGTH },
      },
    ],
    buttons: [
      { text: 'Cancel', role: 'cancel' },
      {
        text: 'Save',
        handler: (values: { alias?: string }) => {
          void store.rename(c.id, values.alias ?? '');
        },
      },
    ],
  });
  await alert.present();
}

async function confirmRemove(c: Computer): Promise<void> {
  const alert = await alertController.create({
    header: `Remove ${computerLabel(c)}?`,
    message: 'Removing it only forgets it on this phone. Also revoke access so this phone can no longer connect.',
    inputs: [{ type: 'checkbox', label: 'Also revoke access on the computer', value: 'revoke', checked: true }],
    buttons: [
      { text: 'Cancel', role: 'cancel' },
      {
        text: 'Remove',
        role: 'destructive',
        handler: (values: string[]) => {
          void remove(c, values.includes('revoke'));
        },
      },
    ],
  });
  await alert.present();
}

async function remove(c: Computer, revoke: boolean): Promise<void> {
  const revoked = await store.remove(c.id, { revoke });
  if (revoke && !revoked) {
    await (
      await toastController.create({
        message: `Removed, but ${computerLabel(c)} could not be reached to revoke access. Revoke it there with "remote-shell devices revoke".`,
        duration: 6000,
        position: 'top',
      })
    ).present();
  }
}
</script>

<style scoped>
.reported {
  font-size: 0.8rem;
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
