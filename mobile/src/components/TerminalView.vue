<template>
  <div class="terminal-root">
    <div v-if="state !== 'ready'" class="terminal-banner" role="status">{{ stateLabel }}</div>
    <div v-if="selection.selectMode.value" class="terminal-banner select-banner" role="status">
      Drag over the text to select it
      <button type="button" @click="selection.stopSelectMode()">Cancel</button>
    </div>
    <div ref="host" v-long-press="onHold" class="terminal-host" />
    <!-- The selection's handles and menu float over the terminal. Touches on them are
         hidden from the page, or Ionic's swipe-back would take a drag near the edge. -->
    <template v-if="layout && selection.range.value">
      <div
        v-if="layout.start"
        class="sel-handle"
        :style="{ left: `${layout.start.x - 22}px`, top: `${layout.start.y - 38}px` }"
        aria-label="Start of the selection"
        role="slider"
        @touchstart.prevent.stop="selection.handleStart('start', $event)"
        @touchmove.prevent.stop="selection.handleMove($event)"
        @touchend.stop="selection.handleEnd()"
        @touchcancel.stop="selection.handleEnd()"
      >
        <span class="sel-stem" :style="{ top: '32px', height: `${layout.cellH + 6}px` }" />
        <span class="sel-knob" style="top: 20px" />
      </div>
      <div
        v-if="layout.end"
        class="sel-handle"
        :style="{ left: `${layout.end.x - 22}px`, top: `${layout.end.y - 6}px` }"
        aria-label="End of the selection"
        role="slider"
        @touchstart.prevent.stop="selection.handleStart('end', $event)"
        @touchmove.prevent.stop="selection.handleMove($event)"
        @touchend.stop="selection.handleEnd()"
        @touchcancel.stop="selection.handleEnd()"
      >
        <span class="sel-stem" :style="{ top: `${6 - layout.cellH}px`, height: `${layout.cellH}px` }" />
        <span class="sel-knob" style="top: 12px" />
      </div>
      <div
        v-if="menuVisible"
        class="sel-menu"
        :class="{ below: layout.menuBelow }"
        :style="menuStyle"
        role="menu"
        @pointerdown.prevent
        @touchstart.stop
        @touchmove.stop
      >
        <button type="button" role="menuitem" @click="copySelection">Copy</button>
        <button type="button" role="menuitem" @click="selection.selectAll()">Select all</button>
        <button type="button" role="menuitem" @click="pasteAndClear">Paste</button>
        <button type="button" role="menuitem" @click="showMore">More</button>
      </div>
    </template>
    <div class="terminal-keys" role="toolbar" aria-label="Terminal keys">
      <!-- pointerdown + preventDefault keeps the focus (and the keyboard) on the terminal -->
      <button type="button" @pointerdown.prevent="send(KEY_ESC)">Esc</button>
      <button type="button" @pointerdown.prevent="send(KEY_TAB)">Tab</button>
      <button type="button" :class="{ active: ctrl }" :aria-pressed="ctrl" @pointerdown.prevent="ctrl = !ctrl">
        Ctrl
      </button>
      <button
        type="button"
        aria-label="Select text"
        :class="{ active: selection.selectMode.value }"
        :aria-pressed="selection.selectMode.value"
        @pointerdown.prevent
        @click="selection.toggleSelectMode()"
      >
        Select
      </button>
      <button type="button" aria-label="Copy" @pointerdown.prevent @click="copySelectionOrScreen">Copy</button>
      <button type="button" aria-label="Paste" @pointerdown.prevent @click="pasteFromClipboard()">Paste</button>
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
import { actionSheetController, alertController, toastController } from '@ionic/vue';
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';

import { Capacitor } from '@capacitor/core';

import { useTerminalSelection } from '@/composables/useTerminalSelection';
import { longPress } from '@/directives/longPress';
import { readClipboard, writeClipboard } from '@/services/clipboard';
import { checkPaste, MAX_PASTE_BYTES } from '@/services/paste';
import { TerminalConnection, type ConnectionState } from '@/services/terminalConnection';
import { readLineAt, readRows, trimSelection } from '@/services/terminalText';
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
const vLongPress = longPress;

const computers = useComputersStore();
const host = ref<HTMLDivElement>();
const state = ref<ConnectionState>('connecting');
const ctrl = ref(false);

let term: Terminal | null = null;
let connection: TerminalConnection | null = null;
let observer: ResizeObserver | null = null;
let fitAddon: FitAddon | null = null;
let removeResumeListener: (() => void) | null = null;
let stopWatchingNetwork: (() => void) | null = null;

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
  if (selection.range.value) selection.clear();
  if (ctrl.value) {
    ctrl.value = false;
    connection?.sendInput(applyCtrl(data));
  } else {
    connection?.sendInput(data);
  }
}

async function toast(message: string): Promise<void> {
  await (await toastController.create({ message, duration: 1800, position: 'top' })).present();
}

async function copyText(text: string, what: string): Promise<void> {
  if (!text) return toast('Nothing to copy');
  try {
    await writeClipboard(text);
    await toast(`Copied ${what}`);
  } catch {
    await toast('Could not copy to the clipboard');
  }
}

function screenText(): string {
  const buffer = term?.buffer.active;
  return buffer && term ? readRows(buffer, buffer.viewportY, buffer.viewportY + term.rows - 1) : '';
}

// The Copy button: what is selected, or else what is on the screen.
function copySelectionOrScreen(): void {
  const selected = trimSelection(term?.getSelection() ?? '');
  void (selected ? copyText(selected, 'selection') : copyText(screenText(), 'screen'));
  selection.clear();
  term?.focus();
}

function confirmPaste(lines: number): Promise<boolean> {
  return new Promise((resolve) => {
    void alertController
      .create({
        header: `Paste ${lines} lines?`,
        message: 'This shell runs each line as soon as it arrives. Check the text before pasting.',
        buttons: [
          { text: 'Cancel', role: 'cancel', handler: () => resolve(false) },
          { text: 'Paste', handler: () => resolve(true) },
        ],
        backdropDismiss: false,
      })
      .then((alert) => alert.present());
  });
}

/** Pastes `text`, or the clipboard when none is given. */
async function pasteFromClipboard(text?: string): Promise<void> {
  const t = term;
  if (!t) return;
  // Input sent while disconnected is only queued up to a small limit, and a
  // paste would be cut or lost without notice.
  if (state.value !== 'ready') return toast('Not connected. Try again once it reconnects.');
  let raw = text;
  if (raw === undefined) {
    try {
      raw = await readClipboard();
    } catch {
      return toast('Could not read the clipboard');
    }
  }
  const check = checkPaste(raw, t.modes.bracketedPasteMode);
  if (check.empty) return toast('The clipboard is empty');
  if (check.tooLarge) return toast(`Too much text to paste (limit ${MAX_PASTE_BYTES / 1024 / 1024} MiB)`);
  if (check.needsConfirm && !(await confirmPaste(check.lines))) return;
  // paste() wraps the text as a block when the shell supports bracketed paste.
  t.paste(check.text);
  t.focus();
}

// Paste from a hardware keyboard or the system menu goes through the same checks.
function onPasteEvent(e: ClipboardEvent): void {
  e.preventDefault();
  e.stopImmediatePropagation();
  void pasteFromClipboard(e.clipboardData?.getData('text/plain') ?? '');
}

// Selecting with a finger: press and hold a word, or turn on Select. The selection
// stays, with a handle at each end and a menu over it.
const selection = useTerminalSelection(
  () => term,
  () => host.value,
);
const layout = selection.layout;

function onHold(e: PointerEvent): void {
  selection.begin(e);
}

const menuVisible = computed(
  () => !!selection.range.value && !!layout.value && !selection.gesturing.value && !selection.draggingHandle.value,
);

// Keep the menu inside the screen: it is about 190 px wide and centred on its point.
const menuStyle = computed(() => {
  const l = layout.value;
  return l ? { left: `clamp(100px, ${l.menu.x}px, calc(100% - 100px))`, top: `${l.menu.y}px` } : {};
});

function copySelection(): void {
  const text = trimSelection(term?.getSelection() ?? '');
  selection.clear();
  void copyText(text, 'text');
  term?.focus();
}

function pasteAndClear(): void {
  selection.clear();
  void pasteFromClipboard();
}

/** More ways to copy: the line the selection starts on, the screen, or everything. */
async function showMore(): Promise<void> {
  const t = term;
  const r = selection.range.value;
  if (!t || !r) return;
  const buffer = t.buffer.active;
  const row = r.start.row;
  const sheet = await actionSheetController.create({
    buttons: [
      { text: 'Copy line', handler: () => void copyText(readLineAt(buffer, row), 'line') },
      { text: 'Copy screen', handler: () => void copyText(screenText(), 'screen') },
      { text: 'Copy all output', handler: () => void copyText(readRows(buffer, 0, buffer.length - 1), 'output') },
      { text: 'Cancel', role: 'cancel' },
    ],
  });
  await sheet.present();
  void sheet.onDidDismiss().then(() => {
    selection.clear();
    t.focus();
  });
}

onMounted(async () => {
  if (!host.value) return;
  host.value.addEventListener('paste', onPasteEvent, true);
  selection.attach();
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
  selection.bindTerminal(t);
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
  stopWatchingNetwork = conn.watchNetwork();

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
  selection.detach();
  host.value?.removeEventListener('paste', onPasteEvent, true);
  void setIosAccessoryBar(true);
  stopWatchingNetwork?.();
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
  position: relative;
  display: flex;
  flex-direction: column;
  height: 100%;
  background: #000;
}
.terminal-host {
  flex: 1;
  min-height: 0;
}
/* The padding goes on xterm's own element, not on the container: the fit addon
   works out the rows from the container's height minus the padding of the
   terminal element only, so padding on the container made the terminal taller
   than the space it has and it ran over the key row. The extra room above the
   first row is for the knob of a selection handle: without it the knob would
   sit under the page header, out of reach of a finger. */
.terminal-host :deep(.xterm) {
  padding: 24px 4px 4px;
}
.terminal-banner {
  padding: 4px 12px;
  font-size: 13px;
  text-align: center;
  color: #000;
  background: #f5c542;
}
/* Floats over the terminal instead of taking space: if it did, the terminal would
   shrink when select mode starts, the shell would be resized and redraw, and the
   text under the finger would move. Touches pass through it, except on Cancel. */
.select-banner {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  z-index: 5;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  pointer-events: none;
}
.select-banner button {
  pointer-events: auto;
  min-height: 28px;
  padding: 0 12px;
  font: inherit;
  color: #f5c542;
  background: #000;
  border: 0;
  border-radius: 6px;
}
/* The selection's handles: a stem along the first or last row with a knob above or
   below it, like the system's, inside a 44 px square so a finger can grab them. */
.sel-handle {
  position: absolute;
  z-index: 6;
  width: 44px;
  height: 44px;
  touch-action: none;
  -webkit-user-select: none;
  user-select: none;
}
.sel-stem {
  position: absolute;
  left: 21px;
  width: 2px;
  background: #0a84ff;
  pointer-events: none;
}
.sel-knob {
  position: absolute;
  left: 16px;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: #0a84ff;
  pointer-events: none;
}
.sel-menu {
  position: absolute;
  z-index: 7;
  display: flex;
  overflow: hidden;
  border-radius: 8px;
  background: #2c2c2e;
  box-shadow: 0 4px 14px rgb(0 0 0 / 50%);
  transform: translate(-50%, calc(-100% - 10px));
}
.sel-menu.below {
  transform: translate(-50%, 18px);
}
.sel-menu button {
  min-height: 40px;
  padding: 0 12px;
  font: inherit;
  font-size: 14px;
  color: #fff;
  background: transparent;
  border: 0;
  border-right: 1px solid #48484a;
  white-space: nowrap;
}
.sel-menu button:last-child {
  border-right: 0;
}
.sel-menu button:active {
  background: #48484a;
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
