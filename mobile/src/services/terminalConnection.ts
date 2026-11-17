import type { AgentApi } from './api';
import { AgentError } from './errors';
import { decodeOutput, encodeInput } from './frames';

export type ConnectionState = 'connecting' | 'ready' | 'reconnecting' | 'closed';

/** The subset of the WebSocket API used here (so tests can fake it). */
export interface WebSocketLike {
  binaryType: string;
  onopen: ((ev: unknown) => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: { code: number }) => void) | null;
  onerror: ((ev: unknown) => void) | null;
  send(data: string | ArrayBuffer): void;
  close(code?: number, reason?: string): void;
}

export interface TerminalCallbacks {
  onOutput(data: Uint8Array): void;
  onState?(state: ConnectionState): void;
  /** The shell process ended. The connection is closed for good. */
  onExit?(exitCode: number): void;
  /** Output was lost while disconnected (it fell out of the agent's buffer). */
  onOutputLost?(): void;
  /** A protocol error, or a fatal connection error (followed by state "closed"). */
  onError?(err: Error): void;
}

export interface TerminalConnectionOptions {
  api: Pick<AgentApi, 'connectionTicket' | 'terminalUrl'>;
  sessionId: string;
  callbacks: TerminalCallbacks;
  createSocket?: (url: string) => WebSocketLike;
  baseDelayMs?: number;
  maxDelayMs?: number;
}

const MAX_QUEUED_INPUT = 64 * 1024;
const MAX_INPUT_CHUNK = 16 * 1024;
const encoder = new TextEncoder();

/**
 * One terminal session connection. It reconnects on its own with a fresh
 * single-use ticket and resumes after the last output sequence it processed,
 * so it never creates a second session and never shows output twice.
 */
export class TerminalConnection {
  private readonly api: TerminalConnectionOptions['api'];
  private readonly sessionId: string;
  private readonly cb: TerminalCallbacks;
  private readonly createSocket: (url: string) => WebSocketLike;
  private readonly baseDelay: number;
  private readonly maxDelay: number;

  private socket: WebSocketLike | null = null;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private state: ConnectionState = 'connecting';
  private stopped = false;
  private everReady = false;
  private attempt = 0;
  private lastSeq = 0;
  private size: { cols: number; rows: number } | null = null;
  private queued: Uint8Array[] = [];
  private queuedBytes = 0;

  constructor(opts: TerminalConnectionOptions) {
    this.api = opts.api;
    this.sessionId = opts.sessionId;
    this.cb = opts.callbacks;
    this.createSocket = opts.createSocket ?? ((url) => new WebSocket(url) as unknown as WebSocketLike);
    this.baseDelay = opts.baseDelayMs ?? 500;
    this.maxDelay = opts.maxDelayMs ?? 15_000;
  }

  /** Sequence number of the last output frame passed to onOutput. */
  get lastSequence(): number {
    return this.lastSeq;
  }

  get connectionState(): ConnectionState {
    return this.state;
  }

  start(): void {
    void this.connect();
  }

  /** Sends keyboard input. Input typed before the connection is ready is queued. */
  sendInput(data: string | Uint8Array): void {
    const bytes = typeof data === 'string' ? encoder.encode(data) : data;
    if (this.state === 'ready' && this.socket) {
      this.sendBytes(bytes);
      return;
    }
    if (!this.stopped && this.queuedBytes + bytes.length <= MAX_QUEUED_INPUT) {
      this.queued.push(bytes);
      this.queuedBytes += bytes.length;
    }
  }

  resize(cols: number, rows: number): void {
    this.size = { cols, rows };
    if (this.state === 'ready') {
      this.sendControl({ type: 'terminal.resize', cols, rows });
    }
  }

  /**
   * Drops the current socket and reconnects right away. Used when the app
   * returns to the foreground: the OS may have killed the socket silently.
   */
  reconnectNow(): void {
    if (this.stopped) return;
    this.clearTimer();
    const socket = this.socket;
    this.socket = null;
    if (socket) {
      socket.onclose = null;
      socket.close(1000);
    }
    this.attempt = 0;
    void this.connect();
  }

  /** Disconnects without ending the session (persistent sessions keep running). */
  close(): void {
    this.stopped = true;
    this.clearTimer();
    const socket = this.socket;
    this.socket = null;
    if (socket) {
      socket.onclose = null;
      socket.close(1000);
    }
    this.setState('closed');
  }

  private setState(state: ConnectionState): void {
    if (this.state === state) return;
    this.state = state;
    this.cb.onState?.(state);
  }

  private clearTimer(): void {
    if (this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
    }
  }

  private async connect(): Promise<void> {
    if (this.stopped) return;
    this.setState(this.everReady ? 'reconnecting' : 'connecting');

    let url: string;
    try {
      const { ticket } = await this.api.connectionTicket(this.sessionId);
      url = this.api.terminalUrl(this.sessionId, ticket);
    } catch (err) {
      this.handleTicketError(err);
      return;
    }
    if (this.stopped) return;

    const ws = this.createSocket(url);
    ws.binaryType = 'arraybuffer';
    this.socket = ws;
    ws.onmessage = (ev) => this.handleMessage(ws, ev.data);
    ws.onclose = () => {
      if (this.socket !== ws) return;
      this.socket = null;
      if (!this.stopped) this.scheduleReconnect();
    };
    ws.onerror = () => {
      // A close event always follows; reconnection is handled there.
    };
  }

  private handleTicketError(err: unknown): void {
    if (err instanceof AgentError) {
      const gone = err.status === 404 || err.code === 'DEVICE_REVOKED' || err.code === 'UNAUTHORIZED';
      if (gone) {
        this.fail(err);
        return;
      }
      if (err.code === 'RATE_LIMITED' && err.retryAfter) {
        this.scheduleReconnect(err.retryAfter * 1000);
        return;
      }
    }
    this.scheduleReconnect();
  }

  private fail(err: Error): void {
    this.stopped = true;
    this.clearTimer();
    this.cb.onError?.(err);
    this.setState('closed');
  }

  private scheduleReconnect(delayMs?: number): void {
    if (this.stopped) return;
    this.attempt++;
    const delay = delayMs ?? Math.min(this.maxDelay, this.baseDelay * 2 ** (this.attempt - 1));
    this.setState('reconnecting');
    this.clearTimer();
    this.timer = setTimeout(() => {
      this.timer = null;
      void this.connect();
    }, delay);
  }

  private handleMessage(ws: WebSocketLike, data: unknown): void {
    if (this.socket !== ws) return;
    if (data instanceof ArrayBuffer) {
      this.handleOutput(data);
      return;
    }
    if (typeof data !== 'string') return;

    let msg: { type?: string; [k: string]: unknown };
    try {
      msg = JSON.parse(data);
    } catch {
      return;
    }
    switch (msg.type) {
      case 'session.ready':
        this.onReady();
        break;
      case 'session.resume.result':
        if (msg.buffer_truncated === true) this.cb.onOutputLost?.();
        break;
      case 'session.exited':
        this.onExited(Number(msg.exit_code ?? 0));
        break;
      case 'error':
        this.cb.onError?.(new AgentError(400, String(msg.code ?? 'INTERNAL_ERROR'), String(msg.message ?? '')));
        break;
    }
  }

  private handleOutput(buf: ArrayBuffer): void {
    let frame;
    try {
      frame = decodeOutput(buf);
    } catch {
      return;
    }
    if (frame.seq <= this.lastSeq) return; // already shown
    this.lastSeq = frame.seq;
    this.cb.onOutput(frame.data);
  }

  private onReady(): void {
    this.attempt = 0;
    this.everReady = true;
    this.sendControl({ type: 'session.resume', last_output_sequence: this.lastSeq });
    this.setState('ready');
    if (this.size) {
      this.sendControl({ type: 'terminal.resize', ...this.size });
    }
    const queued = this.queued;
    this.queued = [];
    this.queuedBytes = 0;
    for (const bytes of queued) this.sendBytes(bytes);
  }

  private onExited(code: number): void {
    this.stopped = true;
    this.clearTimer();
    this.cb.onExit?.(code);
    this.setState('closed');
  }

  private sendControl(msg: object): void {
    this.socket?.send(JSON.stringify(msg));
  }

  private sendBytes(bytes: Uint8Array): void {
    for (let i = 0; i < bytes.length; i += MAX_INPUT_CHUNK) {
      this.socket?.send(encodeInput(bytes.subarray(i, i + MAX_INPUT_CHUNK)));
    }
  }
}
