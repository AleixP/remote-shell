import type { Directive } from 'vue';

const HOLD_MS = 500;
/** A finger that moves farther than this is scrolling, not holding. */
const MOVE_TOLERANCE_PX = 10;

interface State {
  cleanup: () => void;
  setHandler: (h: () => void) => void;
}
const states = new WeakMap<HTMLElement, State>();

/**
 * `v-long-press="handler"`: runs the handler when the element is pressed and
 * held. The click that ends the press is swallowed, so holding a row does not
 * also open it. Scrolling or releasing early does nothing.
 */
export const longPress: Directive<HTMLElement, () => void> = {
  mounted(el, binding) {
    let handler = binding.value;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let startX = 0;
    let startY = 0;
    let fired = false;

    const cancel = () => {
      if (timer !== undefined) clearTimeout(timer);
      timer = undefined;
    };
    const onDown = (e: Event) => {
      const p = e as PointerEvent;
      fired = false;
      startX = p.clientX;
      startY = p.clientY;
      cancel();
      timer = setTimeout(() => {
        timer = undefined;
        fired = true;
        handler?.();
      }, HOLD_MS);
    };
    const onMove = (e: Event) => {
      const p = e as PointerEvent;
      if (timer !== undefined && Math.hypot(p.clientX - startX, p.clientY - startY) > MOVE_TOLERANCE_PX) cancel();
    };
    // Runs before the element's own click handlers.
    const onClick = (e: Event) => {
      if (!fired) return;
      fired = false;
      e.stopImmediatePropagation();
      e.preventDefault();
    };
    // The system menu a long press opens on touch screens and with a right click.
    const onContextMenu = (e: Event) => e.preventDefault();

    el.addEventListener('pointerdown', onDown);
    el.addEventListener('pointermove', onMove);
    el.addEventListener('pointerup', cancel);
    el.addEventListener('pointercancel', cancel);
    el.addEventListener('pointerleave', cancel);
    el.addEventListener('click', onClick, true);
    el.addEventListener('contextmenu', onContextMenu);
    el.style.setProperty('-webkit-touch-callout', 'none');
    el.style.setProperty('-webkit-user-select', 'none');
    el.style.setProperty('user-select', 'none');

    states.set(el, {
      setHandler: (h) => {
        handler = h;
      },
      cleanup: () => {
        cancel();
        el.removeEventListener('pointerdown', onDown);
        el.removeEventListener('pointermove', onMove);
        el.removeEventListener('pointerup', cancel);
        el.removeEventListener('pointercancel', cancel);
        el.removeEventListener('pointerleave', cancel);
        el.removeEventListener('click', onClick, true);
        el.removeEventListener('contextmenu', onContextMenu);
      },
    });
  },
  // The handler can change between renders.
  updated(el, binding) {
    states.get(el)?.setHandler(binding.value);
  },
  unmounted(el) {
    states.get(el)?.cleanup();
    states.delete(el);
  },
};
