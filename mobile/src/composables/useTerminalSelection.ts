import type { Terminal } from '@xterm/xterm';

import { type Cell, extendSelection, wordAt } from '@/services/terminalText';

/** How close to the top or bottom edge a finger must be to scroll the terminal. */
const EDGE_PX = 28;
const SCROLL_INTERVAL_MS = 70;

export interface TerminalSelection {
  /** Starts selecting at the position of a press-and-hold. */
  begin(e: PointerEvent): void;
  /** Call once the terminal is mounted and before it is torn down. */
  attach(): void;
  detach(): void;
}

/**
 * Selecting text with a finger: a press-and-hold starts on the word under it,
 * dragging extends the selection (scrolling when the finger nears an edge) and
 * letting go finishes it.
 */
export function useTerminalSelection(
  getTerm: () => Terminal | null,
  getHost: () => HTMLElement | undefined,
  onFinish: (selection: { text: string; row: number | null }) => void,
): TerminalSelection {
  let active = false;
  let wordStart: Cell | null = null;
  let wordEnd: Cell | null = null;
  let last = { x: 0, y: 0 };
  let timer: ReturnType<typeof setInterval> | undefined;

  /** The buffer cell under a screen position, clamped to the visible screen. */
  function cellAt(x: number, y: number, clamp: boolean): Cell | null {
    const term = getTerm();
    const screen = getHost()?.querySelector<HTMLElement>('.xterm-screen');
    if (!term || !screen) return null;
    const r = screen.getBoundingClientRect();
    if (!clamp && (x < r.left || x > r.right || y < r.top || y > r.bottom)) return null;
    const col = Math.max(0, Math.min(term.cols - 1, Math.floor((x - r.left) / (r.width / term.cols))));
    const row = Math.max(0, Math.min(term.rows - 1, Math.floor((y - r.top) / (r.height / term.rows))));
    return { col, row: term.buffer.active.viewportY + row };
  }

  function extend(): void {
    const term = getTerm();
    const cell = cellAt(last.x, last.y, true);
    if (!term || !cell || !wordStart || !wordEnd) return;
    const s = extendSelection(wordStart, wordEnd, cell, term.cols);
    term.select(s.col, s.row, s.length);
  }

  function onMove(e: PointerEvent): void {
    if (!active) return;
    last = { x: e.clientX, y: e.clientY };
    extend();
  }

  // Dragging near the top or bottom edge scrolls, so a selection can be long.
  function autoScroll(): void {
    const term = getTerm();
    const screen = getHost()?.querySelector<HTMLElement>('.xterm-screen');
    if (!active || !term || !screen) return;
    const r = screen.getBoundingClientRect();
    if (last.y < r.top + EDGE_PX) term.scrollLines(-1);
    else if (last.y > r.bottom - EDGE_PX) term.scrollLines(1);
    else return;
    extend();
  }

  // While selecting, the browser must not take the drag for a page scroll.
  function onTouchMove(e: TouchEvent): void {
    if (active && e.cancelable) e.preventDefault();
  }

  function finish(): void {
    if (!active) return;
    active = false;
    clearInterval(timer);
    window.removeEventListener('pointermove', onMove);
    window.removeEventListener('pointerup', finish);
    window.removeEventListener('pointercancel', finish);
    const term = getTerm();
    const row = wordStart ? wordStart.row : null;
    onFinish({ text: term?.getSelection() ?? '', row });
  }

  /** Starts selecting at the press position. */
  function begin(e: PointerEvent): void {
    const term = getTerm();
    if (!term) return;
    last = { x: e.clientX, y: e.clientY };
    const cell = cellAt(e.clientX, e.clientY, false);
    wordStart = wordEnd = cell;
    if (cell) {
      const text = term.buffer.active.getLine(cell.row)?.translateToString(false) ?? '';
      const w = wordAt(text, cell.col);
      if (w) {
        wordStart = { col: w.start, row: cell.row };
        wordEnd = { col: w.start + w.length - 1, row: cell.row };
        term.select(w.start, cell.row, w.length);
      }
    }
    active = true;
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', finish);
    window.addEventListener('pointercancel', finish);
    timer = setInterval(autoScroll, SCROLL_INTERVAL_MS);
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

  function attach(): void {
    getHost()?.addEventListener('touchmove', onTouchMove, { passive: false });
  }

  function detach(): void {
    getHost()?.removeEventListener('touchmove', onTouchMove);
    finish();
  }

  return { begin, attach, detach };
}
