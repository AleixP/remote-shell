import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

import { AgentError, NETWORK_ERROR } from '@/services/errors';
import { FRAME_HEADER, FRAME_INPUT, FRAME_OUTPUT } from '@/services/frames';
import { type ConnectionState, TerminalConnection, type WebSocketLike } from '@/services/terminalConnection';

class FakeSocket implements WebSocketLike {
  binaryType = 'blob';
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: ((ev: { code: number }) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  sent: (string | ArrayBuffer)[] = [];
  closed = false;

  constructor(readonly url: string) {}
  send(data: string | ArrayBuffer): void {
    this.sent.push(data);
  }
  close(): void {
    this.closed = true;
  }

  // Server side helpers.
  json(msg: object): void {
    this.onmessage?.({ data: JSON.stringify(msg) });
  }
  output(seq: number, text: string): void {
    const payload = new TextEncoder().encode(text);
    const buf = new ArrayBuffer(FRAME_HEADER + payload.length);
    const view = new DataView(buf);
    view.setUint8(0, FRAME_OUTPUT);
    view.setBigUint64(1, BigInt(seq), false);
    new Uint8Array(buf, FRAME_HEADER).set(payload);
    this.onmessage?.({ data: buf });
  }
  drop(code = 1006): void {
    this.onclose?.({ code });
  }
  controlSent(): Record<string, unknown>[] {
    return this.sent.filter((m): m is string => typeof m === 'string').map((m) => JSON.parse(m));
  }
  inputSent(): string[] {
    return this.sent
      .filter((m): m is ArrayBuffer => typeof m !== 'string')
      .map((b) => {
        expect(new Uint8Array(b)[0]).toBe(FRAME_INPUT);
        return new TextDecoder().decode(new Uint8Array(b, FRAME_HEADER));
      });
  }
}

function setup(opts: { ticketError?: () => unknown } = {}) {
  const sockets: FakeSocket[] = [];
  const outputs: string[] = [];
  const states: ConnectionState[] = [];
  const events = { lost: 0, exits: [] as number[], errors: [] as Error[] };
  let tickets = 0;
  const api = {
    connectionTicket: vi.fn(async () => {
      const err = opts.ticketError?.();
      if (err) throw err;
      return { ticket: `tk${++tickets}`, expires_at: '' };
    }),
    terminalUrl: (id: string, ticket: string) => `wss://mac/api/v1/sessions/${id}/terminal?ticket=${ticket}`,
  };
  const conn = new TerminalConnection({
    api,
    sessionId: 'ses_1',
    baseDelayMs: 100,
    maxDelayMs: 400,
    createSocket: (url) => {
      const s = new FakeSocket(url);
      sockets.push(s);
      return s;
    },
    callbacks: {
      onOutput: (d) => outputs.push(new TextDecoder().decode(d)),
      onState: (s) => states.push(s),
      onExit: (c) => events.exits.push(c),
      onOutputLost: () => events.lost++,
      onError: (e) => events.errors.push(e),
    },
  });
  return { conn, api, sockets, outputs, states, events };
}

const flush = () => vi.advanceTimersByTimeAsync(0);

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe('TerminalConnection', () => {
  test('connects with a ticket, resumes from sequence 0 and becomes ready', async () => {
    const { conn, sockets, states } = setup();
    conn.start();
    await flush();
    expect(sockets).toHaveLength(1);
    expect(sockets[0].url).toBe('wss://mac/api/v1/sessions/ses_1/terminal?ticket=tk1');
    expect(sockets[0].binaryType).toBe('arraybuffer');

    sockets[0].json({ type: 'session.ready', session_id: 'ses_1' });
    expect(sockets[0].controlSent()).toEqual([{ type: 'session.resume', last_output_sequence: 0 }]);
    expect(states).toEqual(['ready']);
    expect(conn.connectionState).toBe('ready');
  });

  test('delivers output in order and ignores frames it already showed', async () => {
    const { conn, sockets, outputs } = setup();
    conn.start();
    await flush();
    sockets[0].json({ type: 'session.ready' });
    sockets[0].output(1, 'a');
    sockets[0].output(2, 'b');
    sockets[0].output(2, 'b'); // duplicate
    sockets[0].output(1, 'a'); // old
    sockets[0].output(3, 'c');
    expect(outputs).toEqual(['a', 'b', 'c']);
    expect(conn.lastSequence).toBe(3);
  });

  test('reconnects with a new ticket and resumes after the last sequence', async () => {
    const { conn, sockets, outputs, states, api } = setup();
    conn.start();
    await flush();
    sockets[0].json({ type: 'session.ready' });
    sockets[0].output(1, 'a');
    sockets[0].output(2, 'b');

    sockets[0].drop();
    expect(conn.connectionState).toBe('reconnecting');
    await vi.advanceTimersByTimeAsync(99);
    expect(sockets).toHaveLength(1); // still backing off
    await vi.advanceTimersByTimeAsync(1);
    expect(sockets).toHaveLength(2);
    expect(sockets[1].url).toContain('ticket=tk2'); // a fresh single-use ticket
    expect(api.connectionTicket).toHaveBeenCalledTimes(2);

    sockets[1].json({ type: 'session.ready' });
    expect(sockets[1].controlSent()[0]).toEqual({ type: 'session.resume', last_output_sequence: 2 });
    sockets[1].output(2, 'b'); // replay overlap is ignored
    sockets[1].output(3, 'c');
    expect(outputs).toEqual(['a', 'b', 'c']);
    expect(states).toEqual(['ready', 'reconnecting', 'ready']);
  });

  test('backs off exponentially up to a maximum', async () => {
    const { conn, sockets } = setup();
    conn.start();
    await flush();
    const delays: number[] = [];
    for (let i = 0; i < 5; i++) {
      const before = sockets.length;
      sockets[sockets.length - 1].drop();
      let waited = 0;
      while (sockets.length === before) {
        await vi.advanceTimersByTimeAsync(10);
        waited += 10;
      }
      delays.push(waited);
    }
    expect(delays).toEqual([100, 200, 400, 400, 400]);
  });

  test('reports lost output after a resume', async () => {
    const { conn, sockets, events } = setup();
    conn.start();
    await flush();
    sockets[0].json({ type: 'session.ready' });
    sockets[0].json({ type: 'session.resume.result', buffer_truncated: false });
    expect(events.lost).toBe(0);
    sockets[0].json({ type: 'session.resume.result', buffer_truncated: true });
    expect(events.lost).toBe(1);
  });

  test('a finished process ends the connection for good', async () => {
    const { conn, sockets, events, states } = setup();
    conn.start();
    await flush();
    sockets[0].json({ type: 'session.ready' });
    sockets[0].json({ type: 'session.exited', exit_code: 3 });
    sockets[0].drop(1000);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(events.exits).toEqual([3]);
    expect(sockets).toHaveLength(1);
    expect(states.at(-1)).toBe('closed');
  });

  test('close() disconnects and never reconnects', async () => {
    const { conn, sockets, states } = setup();
    conn.start();
    await flush();
    sockets[0].json({ type: 'session.ready' });
    conn.close();
    expect(sockets[0].closed).toBe(true);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(sockets).toHaveLength(1);
    expect(states.at(-1)).toBe('closed');
  });

  test('close() while backing off cancels the reconnect', async () => {
    const { conn, sockets } = setup();
    conn.start();
    await flush();
    sockets[0].json({ type: 'session.ready' });
    sockets[0].drop();
    conn.close();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(sockets).toHaveLength(1);
  });

  test('gives up when the session is gone or the device was revoked', async () => {
    for (const err of [new AgentError(404, 'SESSION_NOT_FOUND', 'x'), new AgentError(401, 'DEVICE_REVOKED', 'x')]) {
      const { conn, sockets, events, states } = setup({ ticketError: () => err });
      conn.start();
      await vi.advanceTimersByTimeAsync(60_000);
      expect(sockets).toHaveLength(0);
      expect(events.errors).toEqual([err]);
      expect(states.at(-1)).toBe('closed');
    }
  });

  test('retries after network errors and respects Retry-After', async () => {
    let failures = 2;
    const { conn, sockets } = setup({
      ticketError: () => (failures-- > 0 ? new AgentError(0, NETWORK_ERROR, 'offline') : null),
    });
    conn.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(sockets).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(100 + 200);
    expect(sockets).toHaveLength(1);

    let limited = true;
    const second = setup({
      ticketError: () => {
        if (!limited) return null;
        limited = false;
        return new AgentError(429, 'RATE_LIMITED', 'x', 5);
      },
    });
    second.conn.start();
    await vi.advanceTimersByTimeAsync(4_999);
    expect(second.sockets).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1);
    expect(second.sockets).toHaveLength(1);
  });

  test('queues input until ready, then flushes it in order', async () => {
    const { conn, sockets } = setup();
    conn.start();
    conn.sendInput('ls');
    conn.sendInput('\r');
    await flush();
    expect(sockets[0].inputSent()).toEqual([]);
    sockets[0].json({ type: 'session.ready' });
    expect(sockets[0].inputSent()).toEqual(['ls', '\r']);
    conn.sendInput('x');
    expect(sockets[0].inputSent()).toEqual(['ls', '\r', 'x']);
  });

  test('splits large input into bounded frames', async () => {
    const { conn, sockets } = setup();
    conn.start();
    await flush();
    sockets[0].json({ type: 'session.ready' });
    conn.sendInput('x'.repeat(40_000));
    const parts = sockets[0].inputSent();
    expect(parts.map((p) => p.length)).toEqual([16384, 16384, 7232]);
  });

  test('sends the terminal size when ready and again after a reconnect', async () => {
    const { conn, sockets } = setup();
    conn.resize(80, 24); // before connecting
    conn.start();
    await flush();
    sockets[0].json({ type: 'session.ready' });
    expect(sockets[0].controlSent()).toContainEqual({ type: 'terminal.resize', cols: 80, rows: 24 });
    conn.resize(100, 30);
    expect(sockets[0].controlSent().at(-1)).toEqual({ type: 'terminal.resize', cols: 100, rows: 30 });

    sockets[0].drop();
    await vi.advanceTimersByTimeAsync(100);
    sockets[1].json({ type: 'session.ready' });
    expect(sockets[1].controlSent()).toContainEqual({ type: 'terminal.resize', cols: 100, rows: 30 });
  });

  test('surfaces protocol errors without dropping the connection', async () => {
    const { conn, sockets, events, states } = setup();
    conn.start();
    await flush();
    sockets[0].json({ type: 'session.ready' });
    sockets[0].json({ type: 'error', code: 'INVALID_REQUEST', message: 'bad size' });
    expect(events.errors[0]).toMatchObject({ code: 'INVALID_REQUEST', message: 'bad size' });
    expect(states.at(-1)).toBe('ready');
  });

  test('ignores events from a replaced socket', async () => {
    const { conn, sockets, outputs } = setup();
    conn.start();
    await flush();
    sockets[0].json({ type: 'session.ready' });
    sockets[0].drop();
    await vi.advanceTimersByTimeAsync(100);
    sockets[0].output(9, 'stale');
    expect(outputs).toEqual([]);
    expect(conn.lastSequence).toBe(0);
  });
});
