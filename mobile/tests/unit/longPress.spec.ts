import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { createApp, defineComponent, h, nextTick, ref, withDirectives } from 'vue';

import { longPress } from '@/directives/longPress';

function pointer(el: Element, type: string, x = 0, y = 0): void {
  const e = new Event(type, { bubbles: true, cancelable: true }) as Event & { clientX: number; clientY: number };
  e.clientX = x;
  e.clientY = y;
  el.dispatchEvent(e);
}

let root: HTMLElement;
let app: ReturnType<typeof createApp>;
const onHold = vi.fn();
const onClickRow = vi.fn();

function mount(handler = onHold) {
  const Row = defineComponent({
    setup: () => () => withDirectives(h('div', { id: 'row', onClick: onClickRow }, 'row'), [[longPress, handler]]),
  });
  root = document.createElement('div');
  document.body.appendChild(root);
  app = createApp(Row);
  app.mount(root);
  return root.querySelector('#row') as HTMLElement;
}

beforeEach(() => {
  vi.useFakeTimers();
  onHold.mockReset();
  onClickRow.mockReset();
});
afterEach(() => {
  app.unmount();
  root.remove();
  vi.useRealTimers();
});

describe('v-long-press', () => {
  test('runs the handler after holding for half a second', () => {
    const row = mount();
    pointer(row, 'pointerdown');
    vi.advanceTimersByTime(499);
    expect(onHold).not.toHaveBeenCalled();
    vi.advanceTimersByTime(2);
    expect(onHold).toHaveBeenCalledTimes(1);
  });

  test('a quick tap is not a long press and still clicks', () => {
    const row = mount();
    pointer(row, 'pointerdown');
    vi.advanceTimersByTime(150);
    pointer(row, 'pointerup');
    vi.advanceTimersByTime(1000);
    row.click();
    expect(onHold).not.toHaveBeenCalled();
    expect(onClickRow).toHaveBeenCalledTimes(1);
  });

  test('the click that ends a long press is swallowed, the next one is not', () => {
    const row = mount();
    pointer(row, 'pointerdown');
    vi.advanceTimersByTime(600);
    pointer(row, 'pointerup');
    row.click();
    expect(onClickRow).not.toHaveBeenCalled();
    row.click();
    expect(onClickRow).toHaveBeenCalledTimes(1);
  });

  test('moving the finger (scrolling) cancels it, a small tremor does not', () => {
    const row = mount();
    pointer(row, 'pointerdown', 100, 100);
    pointer(row, 'pointermove', 103, 104);
    vi.advanceTimersByTime(600);
    expect(onHold).toHaveBeenCalledTimes(1);

    onHold.mockReset();
    pointer(row, 'pointerdown', 100, 100);
    pointer(row, 'pointermove', 100, 140);
    vi.advanceTimersByTime(600);
    expect(onHold).not.toHaveBeenCalled();
  });

  test('leaving the element or cancelling the touch stops the timer', () => {
    const row = mount();
    pointer(row, 'pointerdown');
    pointer(row, 'pointerleave');
    vi.advanceTimersByTime(600);
    pointer(row, 'pointerdown');
    pointer(row, 'pointercancel');
    vi.advanceTimersByTime(600);
    expect(onHold).not.toHaveBeenCalled();
  });

  test('the system context menu is suppressed', () => {
    const row = mount();
    const e = new Event('contextmenu', { cancelable: true });
    row.dispatchEvent(e);
    expect(e.defaultPrevented).toBe(true);
  });

  test('uses the latest handler after a re-render', async () => {
    const first = vi.fn();
    const second = vi.fn();
    const which = ref(first);
    const Row = defineComponent({
      setup: () => () => withDirectives(h('div', { id: 'row' }, 'row'), [[longPress, which.value]]),
    });
    root = document.createElement('div');
    document.body.appendChild(root);
    app = createApp(Row);
    app.mount(root);
    which.value = second;
    await nextTick();
    const row = root.querySelector('#row') as HTMLElement;
    pointer(row, 'pointerdown');
    vi.advanceTimersByTime(600);
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });

  test('stops listening when the element is removed', () => {
    const row = mount();
    pointer(row, 'pointerdown');
    app.unmount();
    vi.advanceTimersByTime(600);
    expect(onHold).not.toHaveBeenCalled();
    app = createApp({ render: () => null }); // keeps afterEach happy
    app.mount(document.createElement('div'));
  });
});
