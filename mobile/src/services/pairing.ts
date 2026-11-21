export class PairingError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'PairingError';
  }
}

export interface PairingInfo {
  /** Base URL of the agent, always https. */
  baseUrl: string;
  host: string;
  code: string;
  name: string;
}

// hostname[:port] with DNS-name characters only; no userinfo, path or query.
const HOST_RE = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*(:\d{1,5})?$/i;
// A real link is well under 200 characters; this only has to stop absurd input
// from reaching the parser. A DNS name is at most 253 characters.
const MAX_LINK_LENGTH = 2048;
const MAX_HOST_LENGTH = 260;
// 128-bit codes encoded as unpadded base32.
const CODE_RE = /^[A-Z2-7]{26}$/;

/**
 * Parses the pairing QR payload (remoteshell://pair?v=1&host=...&code=...&name=...).
 * The QR only ever carries a short-lived pairing code, never a token.
 */
export function parsePairingUrl(text: string): PairingInfo {
  let url: URL;
  if (text.length > MAX_LINK_LENGTH) throw new PairingError('This is not a Remote Shell pairing code.');
  try {
    url = new URL(text.trim());
  } catch {
    throw new PairingError('This is not a Remote Shell pairing code.');
  }
  if (url.protocol !== 'remoteshell:' || url.hostname !== 'pair') {
    throw new PairingError('This is not a Remote Shell pairing code.');
  }
  const q = url.searchParams;
  if (q.get('v') !== '1') {
    throw new PairingError('Unsupported pairing code version. Update the app or the agent.');
  }
  const host = q.get('host') ?? '';
  const code = q.get('code') ?? '';
  if (host.length > MAX_HOST_LENGTH || !HOST_RE.test(host)) {
    throw new PairingError('The pairing code contains an invalid host.');
  }
  const port = host.split(':')[1];
  if (port !== undefined && (Number(port) < 1 || Number(port) > 65535)) {
    throw new PairingError('The pairing code contains an invalid port.');
  }
  if (!CODE_RE.test(code)) {
    throw new PairingError('The pairing code is malformed.');
  }
  const name = (q.get('name') ?? '').slice(0, 64) || host;
  return { baseUrl: `https://${host}`, host, code, name };
}
