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

/**
 * `text` without the line breaks at its end. A loop, not a regular expression
 * like /(\r\n|\r|\n)+$/: that one takes time proportional to the square of a long
 * run of line breaks that is not at the end, and pasted text is not trusted.
 */
export function trimTrailingNewlines(text: string): string {
  let end = text.length;
  while (end > 0 && (text[end - 1] === '\n' || text[end - 1] === '\r')) end--;
  return text.slice(0, end);
}

/** Decides what to do with clipboard text before it reaches the terminal. */
export function checkPaste(raw: string, bracketedPasteMode: boolean): PasteCheck {
  const text = sanitizePaste(raw);
  const tooLarge = new TextEncoder().encode(text).length > MAX_PASTE_BYTES;
  const body = trimTrailingNewlines(text);
  const lines = body === '' ? 0 : body.split(/\r\n|\r|\n/).length;
  const multiline = /[\r\n]/.test(body);
  return { text, empty: text === '', tooLarge, lines, needsConfirm: multiline && !bracketedPasteMode };
}
