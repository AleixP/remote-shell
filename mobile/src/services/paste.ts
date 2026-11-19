/** Pasted text is bounded like every other input: nothing larger than this is sent. */
export const MAX_PASTE_BYTES = 1024 * 1024;

/**
 * Removes control characters from text about to be pasted, keeping tab and
 * line breaks. Without this, pasted text could carry an Esc sequence such as
 * the end-of-paste marker and have the rest run as typed commands.
 */
export function sanitizePaste(text: string): string {
  let out = '';
  for (const ch of text) {
    const code = ch.codePointAt(0) ?? 0;
    const control = code < 0x20 || code === 0x7f || (code >= 0x80 && code <= 0x9f);
    if (!control || ch === '\t' || ch === '\n' || ch === '\r') out += ch;
  }
  return out;
}

export interface PasteCheck {
  /** The text to send. */
  text: string;
  empty: boolean;
  tooLarge: boolean;
  /** Number of lines, counting the last one even without a final break. */
  lines: number;
  /**
   * The shell would run each line as soon as it arrives: it has no bracketed
   * paste mode, so a multi-line paste needs the user's confirmation.
   */
  needsConfirm: boolean;
}

/** Decides what to do with clipboard text before it reaches the terminal. */
export function checkPaste(raw: string, bracketedPasteMode: boolean): PasteCheck {
  const text = sanitizePaste(raw);
  const tooLarge = new TextEncoder().encode(text).length > MAX_PASTE_BYTES;
  const body = text.replace(/(\r\n|\r|\n)+$/, '');
  const lines = body === '' ? 0 : body.split(/\r\n|\r|\n/).length;
  const multiline = /[\r\n]/.test(body);
  return { text, empty: text === '', tooLarge, lines, needsConfirm: multiline && !bracketedPasteMode };
}
