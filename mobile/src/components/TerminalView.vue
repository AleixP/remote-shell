<template>
  <div class="terminal-root">
    <div v-if="state !== 'ready'" class="terminal-banner" role="status">{{ stateLabel }}</div>
    <div ref="host" class="terminal-host" />
    <div class="terminal-keys" role="toolbar" aria-label="Terminal keys">
      <!-- pointerdown + preventDefault keeps the focus (and the keyboard) on the terminal -->
      <button type="button" @pointerdown.prevent="send(KEY_ESC)">Esc</button>
      <button type="button" @pointerdown.prevent="send(KEY_TAB)">Tab</button>
      <button type="button" :class="{ active: ctrl }" :aria-pressed="ctrl" @pointerdown.prevent="ctrl = !ctrl">
        Ctrl
      </button>
      <button type="button" aria-label="Up" @pointerdown.prevent="arrow('up')">↑</button>
      <button type="button" aria-label="Down" @pointerdown.prevent="arrow('down')">↓</button>
      <button type="button" aria-label="Left" @pointerdown.prevent="arrow('left')">←</button>
      <button type="button" aria-label="Right" @pointerdown.prevent="arrow('right')">→</button>
      <button v-for="ch in SYMBOLS" :key="ch" type="button" @pointerdown.prevent="send(ch)">
        {{ ch }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { FitAddon } from '@xterm/addon-fit';
import { Terminal } from '@xterm/xterm';
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';

import { Capacitor } from '@capacitor/core';

import { TerminalConnection, type ConnectionState } from '@/services/terminalConnection';
import { applyCtrl, arrowSequence, KEY_ESC, KEY_TAB, type Arrow } from '@/services/terminalKeys';
import { useComputersStore } from '@/stores/computers';

import '@xterm/xterm/css/xterm.css';

const props = withDefaults(
  defineProps<{
    computerId: string;
    sessionId: string;
    /** Whether this terminal is the visible tab (hidden ones keep running). */
    active?: boolean;
  }>(),
  { active: true },
);
const emit = defineEmits<{
  state: [state: ConnectionState];
  /** The shell process ended. */
  exit: [code: number];
  /** The session no longer exists or this phone lost access. */
  failed: [message: string];
}>();

const SYMBOLS = ['|', '/', '-', '~'];

const computers = useComputersStore();
const host = ref<HTMLDivElement>();
const state = ref<ConnectionState>('connecting');
const ctrl = ref(false);

let term: Terminal | null = null;
let connection: TerminalConnection | null = null;
let observer: ResizeObserver | null = null;
let fitAddon: FitAddon | null = null;
let removeResumeListener: (() => void) | null = null;

// A hidden (inactive) tab has no size, so it can only be fitted once shown.
function fitIfVisible(): void {
  if (host.value && host.value.offsetParent !== null) fitAddon?.fit();
}

watch(
  () => props.active,
  async (active) => {
    if (!active) return;
    await nextTick();
    fitIfVisible();
    term?.focus();
  },
);

// iOS adds a bar with arrows and "Done" above the keyboard: hide it, the
// terminal has its own key row.
async function setIosAccessoryBar(visible: boolean): Promise<void> {
  if (Capacitor.getPlatform() !== 'ios') return;
  try {
    const { Keyboard } = await import('@capacitor/keyboard');
    await Keyboard.setAccessoryBarVisible({ isVisible: visible });
  } catch {
    // Not available: the default bar stays.
  }
}

const stateLabel = computed(() =>
  state.value === 'reconnecting'
    ? 'Connection lost. Reconnecting…'
    : state.value === 'closed'
      ? 'Disconnected'
      : 'Connecting…',
);

function send(data: string): void {
  connection?.sendInput(data);
  term?.focus();
}

function arrow(dir: Arrow): void {
  send(arrowSequence(dir, term?.modes.applicationCursorKeysMode ?? false));
}

// Ctrl applies to the next key typed on the soft keyboard, then releases.
function fromKeyboard(data: string): void {
  if (ctrl.value) {
    ctrl.value = false;
    connection?.sendInput(applyCtrl(data));
  } else {
    connection?.sendInput(data);
  }
}

onMounted(async () => {
  if (!host.value) return;
  const t = new Terminal({
    cursorBlink: true,
    fontSize: 14,
    fontFamily: 'Menlo, Monaco, "Courier New", monospace',
    scrollback: 5000,
    theme: { background: '#000000', foreground: '#e5e5e5' },
  });
  const fit = new FitAddon();
  t.loadAddon(fit);
  t.open(host.value);
  fitIfVisible();
  term = t;
  fitAddon = fit;
  void setIosAccessoryBar(false);

  observer = new ResizeObserver(() => fitIfVisible());
  observer.observe(host.value);

  const api = await computers.apiFor(props.computerId);
  if (term !== t) return; // unmounted while loading the credentials

  const conn = new TerminalConnection({
    api,
    sessionId: props.sessionId,
    callbacks: {
      onOutput: (data) => t.write(data),
      onState: (s) => {
        state.value = s;
        emit('state', s);
      },
      onExit: (code) => {
        t.write(`\r\n\x1b[90m[process exited with code ${code}]\x1b[0m\r\n`);
        emit('exit', code);
      },
      onOutputLost: () => t.write('\r\n\x1b[33m[some output was lost while disconnected]\x1b[0m\r\n'),
      onError: (err) => {
        if (state.value === 'closed') emit('failed', err.message);
      },
    },
  });
  connection = conn;

  t.onData(fromKeyboard);
  t.onResize(({ cols, rows }) => conn.resize(cols, rows));
  conn.resize(t.cols, t.rows);
  conn.start();
  if (props.active) t.focus();

  // iOS may have killed the socket while the app was in the background.
  const { App } = await import('@capacitor/app');
  const handle = await App.addListener('resume', () => conn.reconnectNow());
  if (term === t) {
    removeResumeListener = () => void handle.remove();
  } else {
    void handle.remove();
  }
});

onBeforeUnmount(() => {
  void setIosAccessoryBar(true);
  removeResumeListener?.();
  observer?.disconnect();
  // Leaving the screen disconnects; persistent sessions keep running.
  connection?.close();
  term?.dispose();
  term = null;
  connection = null;
});
</script>

<style scoped>
.terminal-root {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: #000;
}
.terminal-host {
  flex: 1;
  min-height: 0;
  padding: 4px;
}
.terminal-banner {
  padding: 4px 12px;
  font-size: 13px;
  text-align: center;
  color: #000;
  background: #f5c542;
}
.terminal-keys {
  display: flex;
  gap: 4px;
  padding: 6px 4px calc(6px + env(safe-area-inset-bottom));
  overflow-x: auto;
  background: #1c1c1e;
}
.terminal-keys button {
  flex: 0 0 auto;
  min-width: 44px;
  min-height: 40px;
  padding: 0 12px;
  font: inherit;
  color: #fff;
  background: #3a3a3c;
  border: 0;
  border-radius: 6px;
  touch-action: manipulation;
  -webkit-user-select: none;
  user-select: none;
}
.terminal-keys button.active {
  color: #000;
  background: #f5c542;
}
</style>
