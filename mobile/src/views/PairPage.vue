<template>
  <ion-page>
    <ion-header>
      <ion-toolbar>
        <ion-buttons slot="start">
          <ion-back-button default-href="/computers" />
        </ion-buttons>
        <ion-title>Pair a computer</ion-title>
      </ion-toolbar>
    </ion-header>

    <ion-content class="ion-padding">
      <p>
        On the computer, run <code>remote-shell pair</code> and scan the QR code it shows. Both devices must be
        connected to Tailscale.
      </p>

      <ion-button expand="block" :disabled="busy" @click="scan">
        <ion-icon slot="start" :icon="qrCodeOutline" />
        Scan QR code
      </ion-button>

      <ion-note v-if="error" color="danger" class="error" role="alert">{{ error }}</ion-note>
      <ion-spinner v-if="busy" name="dots" />

      <ion-item class="paste" lines="none">
        <ion-textarea
          v-model="pasted"
          label="Or paste the pairing link"
          label-placement="stacked"
          placeholder="remoteshell://pair?…"
          :auto-grow="true"
          :disabled="busy"
        />
      </ion-item>
      <ion-button expand="block" fill="outline" :disabled="busy || !pasted.trim()" @click="pair(pasted)">
        Pair
      </ion-button>
    </ion-content>
  </ion-page>
</template>

<script setup lang="ts">
import {
  IonBackButton,
  IonButton,
  IonButtons,
  IonContent,
  IonHeader,
  IonIcon,
  IonItem,
  IonNote,
  IonPage,
  IonSpinner,
  IonTextarea,
  IonTitle,
  IonToolbar,
} from '@ionic/vue';
import { qrCodeOutline } from 'ionicons/icons';
import { ref } from 'vue';
import { useRouter } from 'vue-router';

import { describeError } from '@/services/messages';
import { parsePairingUrl } from '@/services/pairing';
import { useComputersStore } from '@/stores/computers';

const router = useRouter();
const computers = useComputersStore();

const pasted = ref('');
const busy = ref(false);
const error = ref('');

async function scan(): Promise<void> {
  error.value = '';
  let text: string;
  try {
    const { CapacitorBarcodeScanner, CapacitorBarcodeScannerTypeHint } = await import('@capacitor/barcode-scanner');
    const result = await CapacitorBarcodeScanner.scanBarcode({
      hint: CapacitorBarcodeScannerTypeHint.QR_CODE,
      scanInstructions: 'Point the camera at the QR code on your computer',
    });
    text = result.ScanResult;
  } catch {
    // Cancelled, or the camera permission was denied.
    error.value = 'Could not scan. Check the camera permission, or paste the pairing link below.';
    return;
  }
  if (text) await pair(text);
}

async function pair(text: string): Promise<void> {
  busy.value = true;
  error.value = '';
  try {
    const computer = await computers.pair(parsePairingUrl(text));
    pasted.value = '';
    await router.replace(`/computers/${computer.id}`);
  } catch (err) {
    error.value = describeError(err);
  } finally {
    busy.value = false;
  }
}
</script>

<style scoped>
.error {
  display: block;
  margin: 12px 0;
}
.paste {
  margin-top: 24px;
}
</style>
