import type { Terminal } from '@xterm/xterm';
import { type Ref, ref, shallowRef } from 'vue';

import {
  type Cell,
  type Range,
  extendRange,
  handleColumn,
  orderRange,
  selectArgs,
  wordAt,
} from '@/services/terminalText';

/** How close to the top or bottom edge a finger must be to scroll the terminal. */
const EDGE_PX = 28;
const SCROLL_INTERVAL_MS = 70;
/** A touch that moves less than this is a tap. */
const TAP_SLOP_PX = 10;

export type HandleRole = 'start' | 'end';

export interface Point {
  x: number;
  y: number;
}

/** Where to draw the handles and the menu, in pixels relative to the terminal's container. */
export interface SelectionLayout {
  /** The top-left corner of the first selected cell (null when it is scrolled out of view). */
  start: Point | null;
  /** The bottom-right corner of the last selected cell (null when it is scrolled out of view). */
  end: Point | null;
  /** Where the menu points at. */
  menu: Point;
  /** The menu goes under the selection instead of over it (there is no room above). */
  menuBelow: boolean;
  cellH: number;
}

export interface TerminalSelection {
  /** True while the next touch on the terminal selects text instead of scrolling. */
  selectMode: Ref<boolean>;
  /** The selection that stays after the finger is lifted, or null. */
  range: Ref<Range | null>;
  /** Where to draw the handles and the menu for `range`. */
  layout: Ref<SelectionLayout | null>;
  /** The handle being dragged, if any. */
  draggingHandle: Ref<HandleRole | null>;
  /** A press-and-hold or a select-mode touch is still down. */
  gesturing: Ref<boolean>;
  /** A press-and-hold started here: select the word under the finger and follow the drag. */
  begin(e: PointerEvent): void;
  toggleSelectMode(): void;
  stopSelectMode(): void;
  /** Selects everything the terminal holds, scrollback included. */
  selectAll(): void;
  /** Drops the selection and its handles. */
  clear(): void;
  handleStart(role: HandleRole, e: TouchEvent): void;
  handleMove(e: TouchEvent): void;
  handleEnd(): void;
  /** Keeps the handles in place while the terminal scrolls, resizes and redraws. */
  bindTerminal(term: Terminal): void;
  /** Call once the terminal container is mounted, and `detach` before it is torn down. */
  attach(): void;
  detach(): void;
}

const raf: (fn: () => void) => void =
  typeof requestAnimationFrame === 'function'
    ? (fn) => void requestAnimationFrame(fn)
    : (fn) => void setTimeout(fn, 16);

/**
 * Selecting text with a finger, the way a phone does it:
 *
 * - press and hold on a word selects it (dragging before letting go extends it),
 *   or turn on "select mode" and touch or drag anywhere;
 * - when the finger lifts, the selection stays, with a handle at each end that
 *   can be dragged to adjust it, and the menu is drawn over it;
 * - a tap anywhere else, or typing, drops it.
 *
 * Touch is followed with touch events, not pointer events: iOS cancels a pointer
 * sequence as soon as it decides to scroll, but keeps delivering touch events,
 * and it only lets a handler stop that scroll if the handler was already
 * attached when the touch started. That is why the listeners are attached once,
 * up front. While selecting, the touch is also hidden from the elements above,
 * so Ionic's swipe-back gesture (which starts near the left edge) does not move
 * the page along with the finger.
 */
export function useTerminalSelection(
  getTerm: () => Terminal | null,
  getHost: () => HTMLElement | undefined,
): TerminalSelection {
  const selectMode = ref(false);
  const range = shallowRef<Range | null>(null);
  const layout = shallowRef<SelectionLayout | null>(null);
  const draggingHandle = ref<HandleRole | null>(null);
  const gesturing = ref(false);

  // The word a drag started on, and the range it has grown to.
  let active = false;
  let wordStart: Cell | null = null;
  let wordEnd: Cell | null = null;
  let current: Range | null = null;
  // A handle being dragged: the end that stays put, the one that moves, and the
  // distance between the finger and the handle's edge when it was grabbed.
  let handle: { role: HandleRole; anchor: Cell; moving: Cell; dx: number; dy: number } | null = null;
  let last: Point = { x: 0, y: 0 };
  let timer: ReturnType<typeof setInterval> | undefined;
  let tap: Point | null = null;
  let move: (() => void) | null = null;
  let layoutQueued = false;
  const disposables: { dispose(): void }[] = [];

  // ---- geometry ----

  function geometry() {
    const term = getTerm();
    const host = getHost();
    const screen = host?.querySelector<HTMLElement>('.xterm-screen');
    const root = host?.closest<HTMLElement>('.terminal-root') ?? host?.parentElement;
    if (!term || !screen || !root) return null;
    const sr = screen.getBoundingClientRect();
    const rr = root.getBoundingClientRect();
    return { term, sr, rr, cellW: sr.width / term.cols, cellH: sr.height / term.rows };
  }

  /** The buffer cell under a screen position, clamped to the visible screen unless `clamp` is false. */
  function cellAt(x: number, y: number, clamp: boolean): Cell | null {
    const g = geometry();
    if (!g) return null;
    const { term, sr } = g;
    if (!clamp && (x < sr.left || x > sr.right || y < sr.top || y > sr.bottom)) return null;
    const col = Math.max(0, Math.min(term.cols - 1, Math.floor((x - sr.left) / g.cellW)));
    const row = Math.max(0, Math.min(term.rows - 1, Math.floor((y - sr.top) / g.cellH)));
    return { col, row: term.buffer.active.viewportY + row };
  }

  /** The two ends to draw handles at: the ordered range, or the live ends while one is dragged. */
  function ends(): { start: Cell; end: Cell } | null {
    if (handle) {
      return {
        start: handle.role === 'start' ? handle.moving : handle.anchor,
        end: handle.role === 'end' ? handle.moving : handle.anchor,
      };
    }
    return range.value;
  }

  function updateLayout(): void {
    const g = geometry();
    const e = ends();
    if (!g || !e) {
      layout.value = null;
      return;
    }
    const viewportY = g.term.buffer.active.viewportY;
    const originX = g.sr.left - g.rr.left;
    const originY = g.sr.top - g.rr.top;
    const place = (c: Cell, edge: HandleRole): Point | null => {
      const vis = c.row - viewportY;
      if (vis < 0 || vis >= g.term.rows) return null;
      return {
        x: originX + (edge === 'end' ? c.col + 1 : c.col) * g.cellW,
        y: originY + (edge === 'end' ? vis + 1 : vis) * g.cellH,
      };
    };
    const start = place(e.start, 'start');
    const end = place(e.end, 'end');
    // The menu points at the top of the selection, or the bottom when there is no room above.
    const top = start ? start.y : originY;
    const menuBelow = top < 56;
    const bottom = end ? end.y : originY + g.sr.height;
    const sameRow = start && end && Math.abs(end.y - start.y - g.cellH) < 1;
    const x = sameRow ? (start.x + end.x) / 2 : g.rr.width / 2;
    layout.value = { start, end, menu: { x, y: menuBelow ? bottom : top }, menuBelow, cellH: g.cellH };
  }

  /** Redraw the handles at most once per frame: xterm renders often. */
  function queueLayout(): void {
    if (layoutQueued || !range.value) return;
    layoutQueued = true;
    raf(() => {
      layoutQueued = false;
      updateLayout();
    });
  }

  function apply(r: Range | null): void {
    const term = getTerm();
    if (!term || !r) return;
    const s = selectArgs(r, term.cols);
    term.select(s.col, s.row, s.length);
  }

  // ---- scrolling at the edges ----

  // Dragging near the top or bottom edge scrolls, so a selection can be long.
  function autoScroll(): void {
    const term = getTerm();
    const screen = getHost()?.querySelector<HTMLElement>('.xterm-screen');
    if (!(active || handle) || !term || !screen) return;
    const r = screen.getBoundingClientRect();
    const buffer = term.buffer.active;
    // Only scroll when there is history that way: scrolling with nowhere to go
    // still shifts the terminal sideways under the finger.
    if (last.y < r.top + EDGE_PX && buffer.viewportY > 0) term.scrollLines(-1);
    else if (last.y > r.bottom - EDGE_PX && buffer.viewportY < buffer.baseY) term.scrollLines(1);
    else return;
    move?.();
  }

  function startTimer(): void {
    clearInterval(timer);
    timer = setInterval(autoScroll, SCROLL_INTERVAL_MS);
  }

  // ---- the selection a finger draws ----

  function extend(): void {
    const cell = cellAt(last.x, last.y, true);
    if (!cell || !wordStart || !wordEnd) return;
    current = extendRange(wordStart, wordEnd, cell);
    apply(current);
  }

  /** Starts following a drag at a screen position; `word` selects the word under it first. */
  function startDrag(x: number, y: number, word: boolean): void {
    const term = getTerm();
    if (!term) return;
    clear();
    last = { x, y };
    const cell = cellAt(x, y, !word);
    wordStart = wordEnd = cell;
    current = cell ? { start: cell, end: cell } : null;
    if (cell && word) {
      const text = term.buffer.active.getLine(cell.row)?.translateToString(false) ?? '';
      const w = wordAt(text, cell.col);
      if (w) {
        wordStart = { col: w.start, row: cell.row };
        wordEnd = { col: w.start + w.length - 1, row: cell.row };
        current = { start: wordStart, end: wordEnd };
      }
    }
    apply(current);
    active = true;
    gesturing.value = true;
    move = extend;
    startTimer();
  }

  function moveDrag(x: number, y: number): void {
    if (!active) return;
    last = { x, y };
    extend();
  }

  /** The finger lifted: the selection stays, with its handles. */
  function endDrag(): void {
    if (!active) return;
    active = false;
    gesturing.value = false;
    clearInterval(timer);
    selectMode.value = false;
    const term = getTerm();
    if (!current || !term?.getSelection().trim()) {
      clear();
      return;
    }
    range.value = current;
    updateLayout();
  }

  // ---- the handles ----

  function handleStart(role: HandleRole, e: TouchEvent): void {
    const g = geometry();
    const r = range.value;
    if (!g || !r || e.touches.length !== 1) return;
    const t = e.touches[0];
    const cell = role === 'start' ? r.start : r.end;
    const vis = cell.row - g.term.buffer.active.viewportY;
    const edgeX = g.sr.left + (role === 'end' ? cell.col + 1 : cell.col) * g.cellW;
    const edgeY = g.sr.top + (role === 'end' ? vis + 1 : vis) * g.cellH;
    // Remember where on the handle the finger landed, so it does not jump.
    handle = {
      role,
      anchor: role === 'start' ? r.end : r.start,
      moving: cell,
      dx: t.clientX - edgeX,
      dy: t.clientY - edgeY,
    };
    draggingHandle.value = role;
    last = { x: t.clientX, y: t.clientY };
    move = moveHandle;
    startTimer();
  }

  function moveHandle(): void {
    const g = geometry();
    if (!g || !handle) return;
    const edgeX = last.x - handle.dx;
    // The handle's edge is at the corner of a cell: aim at the middle of the row it belongs to.
    const edgeY = last.y - handle.dy + (handle.role === 'start' ? g.cellH / 2 : -g.cellH / 2);
    const row = Math.max(0, Math.min(g.term.rows - 1, Math.floor((edgeY - g.sr.top) / g.cellH)));
    const col = handleColumn(handle.role, edgeX, g.sr.left, g.cellW, g.term.cols);
    handle.moving = { col, row: g.term.buffer.active.viewportY + row };
    const r = orderRange(handle.anchor, handle.moving);
    range.value = r;
    apply(r);
    updateLayout();
  }

  function handleMove(e: TouchEvent): void {
    if (!handle || e.touches.length === 0) return;
    last = { x: e.touches[0].clientX, y: e.touches[0].clientY };
    moveHandle();
  }

  function handleEnd(): void {
    if (!handle) return;
    handle = null;
    draggingHandle.value = null;
    clearInterval(timer);
    move = null;
    updateLayout();
  }

  // ---- touch (the phone) ----

  function onTouchStart(e: TouchEvent): void {
    if (e.touches.length !== 1) return;
    if (!selectMode.value || active) {
      // Outside select mode a touch is left alone, but if a selection is up, a
      // tap (not a drag) on the terminal will drop it.
      tap = range.value && !active ? { x: e.touches[0].clientX, y: e.touches[0].clientY } : null;
      return;
    }
    // Keeps the tap from also focusing the terminal and raising the keyboard.
    if (e.cancelable) e.preventDefault();
    // Ionic's swipe-back gesture listens higher up and starts on a drag that
    // begins near the left edge: it would drag the whole page along with the
    // finger. While selecting, it must not hear about the touch.
    e.stopPropagation();
    startDrag(e.touches[0].clientX, e.touches[0].clientY, true);
  }

  function onTouchMove(e: TouchEvent): void {
    if (
      tap &&
      e.touches.length > 0 &&
      Math.hypot(e.touches[0].clientX - tap.x, e.touches[0].clientY - tap.y) > TAP_SLOP_PX
    ) {
      tap = null; // it is a drag (a scroll), not a tap
    }
    if (!active && !selectMode.value) return;
    // The browser must not take this drag for a scroll, nor Ionic for a swipe back.
    if (e.cancelable) e.preventDefault();
    e.stopPropagation();
    if (e.touches.length > 0) moveDrag(e.touches[0].clientX, e.touches[0].clientY);
  }

  function onTouchEnd(): void {
    if (active) {
      endDrag();
      return;
    }
    if (tap) {
      tap = null;
      clear();
    }
  }

  function onTouchCancel(): void {
    tap = null;
    endDrag();
  }

  // ---- mouse (desktop browsers, development) ----

  function onMouseMove(e: PointerEvent): void {
    if (e.pointerType === 'mouse') moveDrag(e.clientX, e.clientY);
  }

  function onMouseUp(e: PointerEvent): void {
    if (e.pointerType !== 'mouse') return;
    window.removeEventListener('pointermove', onMouseMove);
    window.removeEventListener('pointerup', onMouseUp);
    endDrag();
  }

  function trackMouse(): void {
    window.addEventListener('pointermove', onMouseMove);
    window.addEventListener('pointerup', onMouseUp);
  }

  function onPointerDown(e: PointerEvent): void {
    if (e.pointerType !== 'mouse' || !selectMode.value || active) return;
    e.preventDefault();
    startDrag(e.clientX, e.clientY, true);
    trackMouse();
  }

  // ---- public ----

  function begin(e: PointerEvent): void {
    // Holding still for a moment while already selecting must not start over.
    if (active || selectMode.value) return;
    tap = null;
    startDrag(e.clientX, e.clientY, true);
    if (e.pointerType === 'mouse') trackMouse();
    void tick();
  }

  // A light tap under the finger when the selection starts, like a native app.
  async function tick(): Promise<void> {
    try {
      const { Capacitor } = await import('@capacitor/core');
      if (!Capacitor.isNativePlatform()) return;
      const { Haptics, ImpactStyle } = await import('@capacitor/haptics');
      await Haptics.impact({ style: ImpactStyle.Light });
    } catch {
      /* no haptics: nothing to do */
    }
  }

  function clear(): void {
    active = false;
    handle = null;
    current = null;
    move = null;
    clearInterval(timer);
    gesturing.value = false;
    draggingHandle.value = null;
    range.value = null;
    layout.value = null;
    getTerm()?.clearSelection();
  }

  function toggleSelectMode(): void {
    if (selectMode.value) {
      stopSelectMode();
      return;
    }
    clear();
    selectMode.value = true;
  }

  function stopSelectMode(): void {
    selectMode.value = false;
    clear();
  }

  function selectAll(): void {
    const term = getTerm();
    if (!term) return;
    term.selectAll();
    range.value = { start: { col: 0, row: 0 }, end: { col: term.cols - 1, row: term.buffer.active.length - 1 } };
    updateLayout();
  }

  function bindTerminal(term: Terminal): void {
    for (const sub of [term.onScroll?.(queueLayout), term.onRender?.(queueLayout), term.onResize?.(queueLayout)]) {
      if (sub) disposables.push(sub);
    }
  }

  function attach(): void {
    const host = getHost();
    if (!host) return;
    host.addEventListener('touchstart', onTouchStart, { passive: false });
    host.addEventListener('touchmove', onTouchMove, { passive: false });
    host.addEventListener('touchend', onTouchEnd);
    host.addEventListener('touchcancel', onTouchCancel);
    host.addEventListener('pointerdown', onPointerDown);
  }

  function detach(): void {
    const host = getHost();
    host?.removeEventListener('touchstart', onTouchStart);
    host?.removeEventListener('touchmove', onTouchMove);
    host?.removeEventListener('touchend', onTouchEnd);
    host?.removeEventListener('touchcancel', onTouchCancel);
    host?.removeEventListener('pointerdown', onPointerDown);
    window.removeEventListener('pointermove', onMouseMove);
    window.removeEventListener('pointerup', onMouseUp);
    for (const d of disposables.splice(0)) d.dispose();
    active = false;
    handle = null;
    clearInterval(timer);
  }

  return {
    selectMode,
    range,
    layout,
    draggingHandle,
    gesturing,
    begin,
    toggleSelectMode,
    stopSelectMode,
    selectAll,
    clear,
    handleStart,
    handleMove,
    handleEnd,
    bindTerminal,
    attach,
    detach,
  };
}
