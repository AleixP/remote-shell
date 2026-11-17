// Keys a phone keyboard lacks, as the byte sequences a terminal expects.

export const KEY_ESC = '\x1b';
export const KEY_TAB = '\t';

export type Arrow = 'up' | 'down' | 'right' | 'left';
const ARROW_FINAL: Record<Arrow, string> = { up: 'A', down: 'B', right: 'C', left: 'D' };

/** Arrow key sequence; applications such as vim switch to "application cursor mode". */
export function arrowSequence(dir: Arrow, applicationMode: boolean): string {
  return (applicationMode ? '\x1bO' : '\x1b[') + ARROW_FINAL[dir];
}

/**
 * Applies a held Ctrl to typed text: Ctrl+C is 0x03, Ctrl+[ is Esc, and so on.
 * Text that has no control equivalent is passed through unchanged.
 */
export function applyCtrl(data: string): string {
  if (data.length !== 1) return data;
  if (data === ' ') return '\x00';
  if (data === '?') return '\x7f';
  const code = data.toUpperCase().charCodeAt(0);
  if (code >= 0x40 && code <= 0x5f) return String.fromCharCode(code & 0x1f);
  return data;
}
