import type { Terminal } from '@xterm/xterm';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

import { useTerminalSelection } from '@/composables/useTerminalSelection';

const COLS = 40;
const ROWS = 10;
const CELL_W = 10;
const CELL_H = 20;
// The terminal's screen is not at the corner of its container, so positions
// have to be worked out relative to the container.
const SCREEN_LEFT = 4;
const SCREEN_TOP = 20;
const TEXT = [
  '$ echo hello-world',
  'hello-world',
  '$ ',
  'line 4 here',
  'line 5 here',
  'line 6 here',
  'line 7 here',
  'line 8 here',
];

/** The slice of xterm the composable uses, recording what it is asked to do. */
function fakeTerminal(scrollback = 0) {
  const state = { viewportY: scrollback > 0 ? 5 : 0, selected: '' };
  const calls = { select: [] as [number, number, number][], cleared: 0, scrolled: [] as number[], selectedAll: 0 };
  const listeners: Record<string, (() => void)[]> = {};
  const on = (name: string) => (fn: () => void) => {
    (listeners[name] ??= []).push(fn);
    return { dispose: () => (listeners[name] = listeners[name].filter((f) => f !== fn)) };
  };
  const term = {
    cols: COLS,
    rows: ROWS,
    buffer: {
      active: {
        get viewportY() {
          return state.viewportY;
        },
        baseY: scrollback,
        length: TEXT.length,
        getLine: (y: number) =>
          TEXT[y] === undefined ? undefined : { isWrapped: false, translateToString: () => TEXT[y].padEnd(COLS) },
      },
    },
    select: (col: number, row: number, length: number) => {
      calls.select.push([col, row, length]);
      // A selection is read back as the text under it (rows are joined by line breaks).
      let text = '';
      for (let i = 0; i < length; i++) {
        const c = col + i;
        const r = row + Math.floor(c / COLS);
        text += (TEXT[r] ?? '').padEnd(COLS)[c % COLS] ?? ' ';
        if ((c + 1) % COLS === 0 && i < length - 1) text += '\n';
      }
      state.selected = text.replace(/ +(\n|$)/g, '$1');
    },
    selectAll: () => {
      calls.selectedAll++;
      state.selected = TEXT.join('\n');
    },
    clearSelection: () => {
      calls.cleared++;
      state.selected = '';
    },
    getSelection: () => state.selected,
    scrollLines: (n: number) => {
      calls.scrolled.push(n);
      state.viewportY = Math.max(0, Math.min(scrollback, state.viewportY + n));
    },
    onScroll: on('scroll'),
    onRender: on('render'),
    onResize: on('resize'),
  };
  return { term: term as unknown as Terminal, calls, state, listeners };
}

function rect(left: number, top: number, width: number, height: number): DOMRect {
  return { left, top, right: left + width, bottom: top + height, width, height, x: left, y: top } as DOMRect;
}

function setup(scrollback = 0) {
  const root = document.createElement('div');
  root.className = 'terminal-root';
  root.getBoundingClientRect = () => rect(0, 0, 400, 300);
  const host = document.createElement('div');
  const screen = document.createElement('div');
  screen.className = 'xterm-screen';
  screen.getBoundingClientRect = () => rect(SCREEN_LEFT, SCREEN_TOP, COLS * CELL_W, ROWS * CELL_H);
  host.appendChild(screen);
  root.appendChild(host);
  document.body.appendChild(root);
  const fake = fakeTerminal(scrollback);
  const selection = useTerminalSelection(
    () => fake.term,
    () => host,
  );
  selection.attach();
  selection.bindTerminal(fake.term);
  return { root, host, screen, selection, ...fake };
}

type Touchy = Event & { touches: { clientX: number; clientY: number }[] };

/** Where the middle of a cell is on the screen. */
const px = (col: number, row: number) => ({
  clientX: SCREEN_LEFT + col * CELL_W + CELL_W / 2,
  clientY: SCREEN_TOP + row * CELL_H + CELL_H / 2,
});

/** A touch event the way a browser delivers it: cancelable, with the touch list. */
function touch(el: Element, type: string, col?: number, row?: number): Event {
  const e = new Event(type, { bubbles: true, cancelable: true }) as Touchy;
  e.touches = col === undefined ? [] : [px(col, row!)];
  el.dispatchEvent(e);
  return e;
}

/** A touch at an exact screen position (for the handles, which sit between cells). */
function touchAt(el: Element, type: string, x: number, y: number): Touchy {
  const e = new Event(type, { bubbles: true, cancelable: true }) as Touchy;
  e.touches = type === 'touchend' ? [] : [{ clientX: x, clientY: y }];
  el.dispatchEvent(e);
  return e;
}

let ctx: ReturnType<typeof setup>;
beforeEach(() => {
  vi.useFakeTimers();
  ctx = setup();
});
afterEach(() => {
  ctx.selection.detach();
  ctx.root.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

/** Selects "echo hello-world" (row 0, cols 2 to 17) with select mode. */
function selectEchoLine(): void {
  ctx.selection.toggleSelectMode();
  touch(ctx.screen, 'touchstart', 2, 0);
  touch(ctx.screen, 'touchmove', 17, 0);
  touch(ctx.screen, 'touchend');
}

describe('select mode', () => {
  test('a drag selects from where it starts to where it ends, and the selection stays', () => {
    ctx.selection.toggleSelectMode();
    expect(ctx.selection.selectMode.value).toBe(true);
    touch(ctx.screen, 'touchstart', 2, 0);
    touch(ctx.screen, 'touchmove', 9, 0);
    touch(ctx.screen, 'touchmove', 17, 0);
    expect(ctx.selection.range.value).toBeNull(); // still dragging
    expect(ctx.selection.gesturing.value).toBe(true);
    touch(ctx.screen, 'touchend');
    expect(ctx.calls.select.at(-1)).toEqual([2, 0, 16]);
    expect(ctx.selection.range.value).toEqual({ start: { col: 2, row: 0 }, end: { col: 17, row: 0 } });
    expect(ctx.selection.gesturing.value).toBe(false);
    expect(ctx.selection.selectMode.value).toBe(false);
    expect(ctx.term.getSelection()).toBe('echo hello-world');
  });

  test('just touching a word selects it', () => {
    ctx.selection.toggleSelectMode();
    touch(ctx.screen, 'touchstart', 3, 0);
    touch(ctx.screen, 'touchend');
    expect(ctx.selection.range.value).toEqual({ start: { col: 2, row: 0 }, end: { col: 5, row: 0 } });
  });

  test('touching blank space selects nothing', () => {
    ctx.selection.toggleSelectMode();
    touch(ctx.screen, 'touchstart', 30, 0);
    touch(ctx.screen, 'touchend');
    expect(ctx.selection.range.value).toBeNull();
    expect(ctx.selection.layout.value).toBeNull();
  });

  test('the drag is not left to the browser, and neither is the tap that starts it', () => {
    ctx.selection.toggleSelectMode();
    expect(touch(ctx.screen, 'touchstart', 2, 0).defaultPrevented).toBe(true);
    expect(touch(ctx.screen, 'touchmove', 5, 0).defaultPrevented).toBe(true);
  });

  test('a drag can go backwards and across rows', () => {
    ctx.selection.toggleSelectMode();
    touch(ctx.screen, 'touchstart', 5, 1);
    touch(ctx.screen, 'touchmove', 2, 0);
    touch(ctx.screen, 'touchend');
    expect(ctx.selection.range.value).toEqual({ start: { col: 2, row: 0 }, end: { col: 10, row: 1 } });
    expect(ctx.term.getSelection()).toBe('echo hello-world\nhello-world');
  });

  test('it can be left without selecting anything', () => {
    ctx.selection.toggleSelectMode();
    ctx.selection.toggleSelectMode();
    expect(ctx.selection.selectMode.value).toBe(false);
    touch(ctx.screen, 'touchstart', 2, 0);
    touch(ctx.screen, 'touchend');
    expect(ctx.selection.range.value).toBeNull();
  });

  test('stopping drops a selection in progress', () => {
    ctx.selection.toggleSelectMode();
    touch(ctx.screen, 'touchstart', 2, 0);
    ctx.selection.stopSelectMode();
    touch(ctx.screen, 'touchend');
    expect(ctx.selection.range.value).toBeNull();
  });
});

describe('outside select mode', () => {
  test('touches are left to the browser, so the terminal still scrolls', () => {
    expect(touch(ctx.screen, 'touchstart', 2, 0).defaultPrevented).toBe(false);
    expect(touch(ctx.screen, 'touchmove', 2, 5).defaultPrevented).toBe(false);
    touch(ctx.screen, 'touchend');
    expect(ctx.calls.select).toHaveLength(0);
    expect(ctx.selection.range.value).toBeNull();
  });

  test('a tap on the terminal drops the selection', () => {
    selectEchoLine();
    expect(ctx.selection.range.value).not.toBeNull();
    touch(ctx.screen, 'touchstart', 20, 6);
    touch(ctx.screen, 'touchend');
    expect(ctx.selection.range.value).toBeNull();
    expect(ctx.selection.layout.value).toBeNull();
    expect(ctx.term.getSelection()).toBe('');
  });

  test('scrolling the terminal does not drop it', () => {
    selectEchoLine();
    touch(ctx.screen, 'touchstart', 20, 6);
    touch(ctx.screen, 'touchmove', 20, 2); // a drag, not a tap
    touch(ctx.screen, 'touchend');
    expect(ctx.selection.range.value).not.toBeNull();
  });

  test('a small tremor in a tap still counts as a tap', () => {
    selectEchoLine();
    touchAt(ctx.screen, 'touchstart', 100, 100);
    touchAt(ctx.screen, 'touchmove', 103, 104);
    touchAt(ctx.screen, 'touchend', 0, 0);
    expect(ctx.selection.range.value).toBeNull();
  });
});

describe('press and hold', () => {
  const hold = (col: number, row: number) =>
    ctx.selection.begin({ ...px(col, row), pointerType: 'touch' } as PointerEvent);

  test('selects the word under the finger, and dragging extends it', () => {
    hold(5, 0); // "echo"
    expect(ctx.calls.select.at(-1)).toEqual([2, 0, 4]);
    expect(ctx.selection.gesturing.value).toBe(true);
    touch(ctx.screen, 'touchmove', 17, 0);
    touch(ctx.screen, 'touchend');
    expect(ctx.selection.range.value).toEqual({ start: { col: 2, row: 0 }, end: { col: 17, row: 0 } });
  });

  test('letting go without dragging keeps the word, with its handles', () => {
    hold(4, 1);
    touch(ctx.screen, 'touchend');
    expect(ctx.selection.range.value).toEqual({ start: { col: 0, row: 1 }, end: { col: 10, row: 1 } });
    expect(ctx.selection.layout.value?.start).not.toBeNull();
    expect(ctx.selection.layout.value?.end).not.toBeNull();
  });

  test('a pointercancel in the middle (iOS does this) does not end the drag', () => {
    hold(5, 0);
    touch(ctx.screen, 'touchmove', 9, 0);
    ctx.screen.dispatchEvent(new Event('pointercancel', { bubbles: true }));
    window.dispatchEvent(new Event('pointercancel'));
    expect(ctx.selection.range.value).toBeNull();
    touch(ctx.screen, 'touchmove', 17, 0);
    touch(ctx.screen, 'touchend');
    expect(ctx.term.getSelection()).toBe('echo hello-world');
  });

  test('a system touchcancel ends it', () => {
    hold(5, 0);
    touch(ctx.screen, 'touchcancel');
    expect(ctx.selection.gesturing.value).toBe(false);
    expect(ctx.selection.range.value).not.toBeNull();
  });

  test('holding again while selecting does not start over', () => {
    hold(5, 0);
    touch(ctx.screen, 'touchmove', 12, 0);
    const before = ctx.calls.select.length;
    hold(12, 0);
    expect(ctx.calls.select).toHaveLength(before);
  });

  test('holding in select mode is part of the drag, not a new selection', () => {
    ctx.selection.toggleSelectMode();
    hold(5, 0);
    expect(ctx.calls.select).toHaveLength(0);
  });

  test('holding on a new word replaces the selection that was up', () => {
    selectEchoLine();
    touch(ctx.screen, 'touchstart', 4, 1);
    hold(4, 1);
    touch(ctx.screen, 'touchend');
    expect(ctx.selection.range.value).toEqual({ start: { col: 0, row: 1 }, end: { col: 10, row: 1 } });
  });
});

describe('the handles', () => {
  // "echo hello-world": the start handle is on the left edge of col 2, row 0;
  // the end handle on the right edge of col 17, row 0.
  const startEdge = { x: SCREEN_LEFT + 2 * CELL_W, y: SCREEN_TOP };
  const endEdge = { x: SCREEN_LEFT + 18 * CELL_W, y: SCREEN_TOP + CELL_H };

  beforeEach(() => selectEchoLine());

  function grab(role: 'start' | 'end', dx = 0, dy = role === 'start' ? -6 : 6): void {
    const edge = role === 'start' ? startEdge : endEdge;
    touchAt(ctx.root, 'touchstart', edge.x + dx, edge.y + dy);
    ctx.selection.handleStart(
      role,
      Object.assign(new Event('touchstart'), {
        touches: [{ clientX: edge.x + dx, clientY: edge.y + dy }],
      }) as TouchEvent,
    );
  }
  const drag = (x: number, y: number) =>
    ctx.selection.handleMove(
      Object.assign(new Event('touchmove'), { touches: [{ clientX: x, clientY: y }] }) as TouchEvent,
    );

  test('the end handle moves the end of the selection', () => {
    grab('end');
    expect(ctx.selection.draggingHandle.value).toBe('end');
    drag(SCREEN_LEFT + 10 * CELL_W, SCREEN_TOP + CELL_H + 6); // the boundary before col 10: the end is col 9
    expect(ctx.selection.range.value).toEqual({ start: { col: 2, row: 0 }, end: { col: 9, row: 0 } });
    expect(ctx.calls.select.at(-1)).toEqual([2, 0, 8]);
    ctx.selection.handleEnd();
    expect(ctx.selection.draggingHandle.value).toBeNull();
    expect(ctx.term.getSelection()).toBe('echo hel');
  });

  test('the start handle moves the start of the selection', () => {
    grab('start');
    drag(SCREEN_LEFT + 7 * CELL_W, SCREEN_TOP - 6); // the boundary before col 7, where "hello-world" starts
    expect(ctx.selection.range.value).toEqual({ start: { col: 7, row: 0 }, end: { col: 17, row: 0 } });
    ctx.selection.handleEnd();
    expect(ctx.term.getSelection()).toBe('hello-world');
  });

  test('grabbing a handle off-centre does not make it jump', () => {
    grab('end', 3, 9); // the finger is a bit to the right and lower than the handle's edge
    drag(endEdge.x + 3, endEdge.y + 9); // and has not moved
    expect(ctx.selection.range.value).toEqual({ start: { col: 2, row: 0 }, end: { col: 17, row: 0 } });
  });

  test('the finger can be well below the text: the handle follows the row above it', () => {
    grab('end');
    drag(endEdge.x, SCREEN_TOP + CELL_H * 2 + 6); // the knob is under row 1, so the end is on row 1
    expect(ctx.selection.range.value?.end).toEqual({ col: 17, row: 1 });
    ctx.selection.handleEnd();
  });

  test('dragging one handle past the other swaps them', () => {
    grab('start');
    drag(SCREEN_LEFT + 24 * CELL_W, SCREEN_TOP - 6); // past the end handle (col 17)
    expect(ctx.selection.range.value).toEqual({ start: { col: 17, row: 0 }, end: { col: 24, row: 0 } });
    expect(ctx.selection.layout.value?.start).not.toBeNull();
    ctx.selection.handleEnd();
    expect(ctx.selection.range.value).toEqual({ start: { col: 17, row: 0 }, end: { col: 24, row: 0 } });
  });

  test('while a handle is dragged the selection reports it, so the menu can hide', () => {
    expect(ctx.selection.draggingHandle.value).toBeNull();
    grab('end');
    expect(ctx.selection.draggingHandle.value).toBe('end');
    ctx.selection.handleEnd();
    expect(ctx.selection.draggingHandle.value).toBeNull();
  });

  test('a handle drag that never started does nothing', () => {
    drag(100, 100);
    ctx.selection.handleEnd();
    expect(ctx.selection.range.value).toEqual({ start: { col: 2, row: 0 }, end: { col: 17, row: 0 } });
  });
});

describe('where the handles and the menu are drawn', () => {
  test('relative to the container, at the corners of the first and last cell', () => {
    selectEchoLine(); // row 0, cols 2 to 17
    const l = ctx.selection.layout.value!;
    expect(l.start).toEqual({ x: SCREEN_LEFT + 2 * CELL_W, y: SCREEN_TOP });
    expect(l.end).toEqual({ x: SCREEN_LEFT + 18 * CELL_W, y: SCREEN_TOP + CELL_H });
    expect(l.cellH).toBe(CELL_H);
  });

  test('the menu goes under a selection near the top, where there is no room above', () => {
    selectEchoLine();
    const l = ctx.selection.layout.value!;
    expect(l.menuBelow).toBe(true);
    expect(l.menu.y).toBe(SCREEN_TOP + CELL_H); // the bottom of the selection
    expect(l.menu.x).toBe((l.start!.x + l.end!.x) / 2);
  });

  test('and over a selection lower down, centred on it', () => {
    ctx.selection.toggleSelectMode();
    touch(ctx.screen, 'touchstart', 1, 5); // the word "line" on row 5
    touch(ctx.screen, 'touchend');
    const l = ctx.selection.layout.value!;
    expect(l.menuBelow).toBe(false);
    expect(l.menu.y).toBe(SCREEN_TOP + 5 * CELL_H); // the top of the selection
    expect(l.menu.x).toBe((l.start!.x + l.end!.x) / 2);
  });

  test('a selection that spans rows centres the menu on the screen', () => {
    ctx.selection.toggleSelectMode();
    touch(ctx.screen, 'touchstart', 5, 3);
    touch(ctx.screen, 'touchmove', 3, 5);
    touch(ctx.screen, 'touchend');
    expect(ctx.selection.layout.value!.menu.x).toBe(200); // half of the 400 px container
  });

  test('an end scrolled out of view has no handle, and the other one stays', () => {
    ctx.root.remove();
    ctx.selection.detach();
    ctx = setup(20); // history, with the viewport 5 lines down
    ctx.selection.toggleSelectMode();
    touch(ctx.screen, 'touchstart', 1, 0);
    touch(ctx.screen, 'touchend');
    expect(ctx.selection.layout.value).not.toBeNull();
    // Scroll the terminal so that the selected row (buffer row 5) is above the screen.
    ctx.state.viewportY = 8;
    vi.stubGlobal('requestAnimationFrame', (fn: () => void) => fn());
    ctx.listeners.scroll.forEach((fn) => fn());
    expect(ctx.selection.layout.value?.start).toBeNull();
    expect(ctx.selection.layout.value?.end).toBeNull();
  });

  test('the handles follow the text when the terminal scrolls', () => {
    ctx.root.remove();
    ctx.selection.detach();
    ctx = setup(20);
    ctx.selection.toggleSelectMode();
    touch(ctx.screen, 'touchstart', 1, 0); // viewport starts at row 5, so this is buffer row 5
    touch(ctx.screen, 'touchend');
    const before = ctx.selection.layout.value!.start!.y;
    ctx.state.viewportY = 4; // scrolled up one line: the text moves down one row
    vi.stubGlobal('requestAnimationFrame', (fn: () => void) => fn());
    ctx.listeners.scroll.forEach((fn) => fn());
    expect(ctx.selection.layout.value!.start!.y).toBe(before + CELL_H);
  });
});

describe('select all, clear and typing', () => {
  test('select all covers the whole buffer', () => {
    ctx.selection.selectAll();
    expect(ctx.calls.selectedAll).toBe(1);
    expect(ctx.selection.range.value).toEqual({
      start: { col: 0, row: 0 },
      end: { col: COLS - 1, row: TEXT.length - 1 },
    });
  });

  test("clear drops the selection, its handles and the terminal's own selection", () => {
    selectEchoLine();
    ctx.selection.clear();
    expect(ctx.selection.range.value).toBeNull();
    expect(ctx.selection.layout.value).toBeNull();
    expect(ctx.term.getSelection()).toBe('');
  });
});

describe('the page gestures', () => {
  /** What an ancestor (like Ionic's swipe-back, which listens on the router outlet) hears. */
  function heard(): string[] {
    const events: string[] = [];
    const outlet = document.createElement('div');
    outlet.appendChild(ctx.root);
    document.body.appendChild(outlet);
    for (const t of ['touchstart', 'touchmove', 'touchend']) outlet.addEventListener(t, () => events.push(t));
    return events;
  }

  test('while selecting, an ancestor does not hear the touch start or the drag', () => {
    const events = heard();
    ctx.selection.toggleSelectMode();
    touch(ctx.screen, 'touchstart', 1, 0); // near the left edge, where a swipe back would start
    touch(ctx.screen, 'touchmove', 9, 0);
    touch(ctx.screen, 'touchmove', 17, 0);
    expect(events).toEqual([]);
  });

  test('after a long press, the drag is hidden from ancestors too', () => {
    const events = heard();
    touch(ctx.screen, 'touchstart', 1, 0); // outside select mode: heard, nothing to stop yet
    ctx.selection.begin({ ...px(1, 0), pointerType: 'touch' } as PointerEvent);
    touch(ctx.screen, 'touchmove', 9, 0);
    touch(ctx.screen, 'touchmove', 17, 0);
    expect(events).toEqual(['touchstart']);
  });

  test('outside selecting, ancestors hear everything, so the page still behaves normally', () => {
    const events = heard();
    touch(ctx.screen, 'touchstart', 1, 0);
    touch(ctx.screen, 'touchmove', 9, 0);
    touch(ctx.screen, 'touchend');
    expect(events).toEqual(['touchstart', 'touchmove', 'touchend']);
  });
});

describe('near the edges', () => {
  beforeEach(() => {
    ctx.selection.detach();
    ctx.root.remove();
    ctx = setup(20); // 20 lines of history, viewport in the middle of it
  });

  function touchAtY(type: string, y: number): void {
    const e = new Event(type, { bubbles: true, cancelable: true }) as Touchy;
    e.touches = [{ clientX: 50, clientY: y }];
    ctx.screen.dispatchEvent(e);
  }

  test('dragging at the top or bottom scrolls the terminal and keeps extending', () => {
    ctx.selection.toggleSelectMode();
    touch(ctx.screen, 'touchstart', 5, 5);
    touchAtY('touchmove', SCREEN_TOP + ROWS * CELL_H - 2); // the bottom edge
    vi.advanceTimersByTime(300);
    expect(ctx.calls.scrolled.length).toBeGreaterThan(0);
    expect(ctx.calls.scrolled.every((n) => n === 1)).toBe(true);
    touchAtY('touchmove', SCREEN_TOP + 2); // the top edge
    vi.advanceTimersByTime(300);
    expect(ctx.calls.scrolled).toContain(-1);
    touch(ctx.screen, 'touchend');
  });

  test('with no history there is nothing to scroll, so it never scrolls', () => {
    ctx.selection.detach();
    ctx.root.remove();
    ctx = setup(0);
    ctx.selection.toggleSelectMode();
    touch(ctx.screen, 'touchstart', 5, 0);
    touchAtY('touchmove', SCREEN_TOP + 2); // the top edge
    vi.advanceTimersByTime(500);
    expect(ctx.calls.scrolled).toHaveLength(0);
    touch(ctx.screen, 'touchend');
  });

  test('no scrolling while the finger is in the middle', () => {
    ctx.selection.toggleSelectMode();
    touch(ctx.screen, 'touchstart', 5, 5);
    vi.advanceTimersByTime(500);
    expect(ctx.calls.scrolled).toHaveLength(0);
    touch(ctx.screen, 'touchend');
  });
});

describe('mouse (desktop)', () => {
  /** A pointer event with the position and type the browser would give it. */
  const pointer = (type: string, col: number, row: number): Event =>
    Object.assign(new Event(type, { bubbles: true, cancelable: true }), { ...px(col, row), pointerType: 'mouse' });

  test('select mode follows a mouse drag with pointer events', () => {
    ctx.selection.toggleSelectMode();
    ctx.host.dispatchEvent(pointer('pointerdown', 2, 0));
    window.dispatchEvent(pointer('pointermove', 17, 0));
    window.dispatchEvent(pointer('pointerup', 17, 0));
    expect(ctx.selection.range.value).toEqual({ start: { col: 2, row: 0 }, end: { col: 17, row: 0 } });
  });
});
