import { AgentError, NETWORK_ERROR } from './errors';

import type { ConnectionTicket, CreateSessionRequest, Device, Health, PairResult, Session } from '@/types/api';

type FetchFn = (input: string, init?: RequestInit) => Promise<Response>;

/** Client for the HTTP API of one agent. */
export class AgentApi {
  constructor(
    readonly baseUrl: string,
    private readonly getToken: () => string | undefined,
    private readonly fetchFn: FetchFn = (input, init) => fetch(input, init),
  ) {}

  private async request<T>(method: string, path: string, opts: { body?: unknown; auth?: boolean } = {}): Promise<T> {
    const { body, auth = true } = opts;
    const headers: Record<string, string> = {};
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
    }
    if (auth) {
      const token = this.getToken();
      if (!token) {
        throw new AgentError(401, 'UNAUTHORIZED', 'This device is not paired with the agent.');
      }
      headers['Authorization'] = `Bearer ${token}`;
    }

    let res: Response;
    try {
      res = await this.fetchFn(`${this.baseUrl}/api/v1${path}`, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch {
      throw new AgentError(0, NETWORK_ERROR, 'Cannot reach the computer.');
    }

    if (!res.ok) {
      let code = 'INTERNAL_ERROR';
      let message = res.statusText;
      try {
        const data = await res.json();
        code = data?.error?.code ?? code;
        message = data?.error?.message ?? message;
      } catch {
        // Non-JSON error body: keep the defaults.
      }
      const retry = Number(res.headers.get('Retry-After'));
      throw new AgentError(res.status, code, message, retry > 0 ? retry : undefined);
    }
    return (await res.json()) as T;
  }

  health(): Promise<Health> {
    return this.request('GET', '/health', { auth: false });
  }

  pair(pairingCode: string, deviceName: string): Promise<PairResult> {
    return this.request('POST', '/pair', {
      auth: false,
      body: { pairing_code: pairingCode, device_name: deviceName },
    });
  }

  async listSessions(): Promise<Session[]> {
    return (await this.request<{ sessions: Session[] }>('GET', '/sessions')).sessions;
  }

  async createSession(req: CreateSessionRequest): Promise<Session> {
    return (await this.request<{ session: Session }>('POST', '/sessions', { body: req })).session;
  }

  async getSession(id: string): Promise<Session> {
    return (await this.request<{ session: Session }>('GET', `/sessions/${encodeURIComponent(id)}`)).session;
  }

  async closeSession(id: string): Promise<void> {
    await this.request('DELETE', `/sessions/${encodeURIComponent(id)}`);
  }

  connectionTicket(sessionId: string): Promise<ConnectionTicket> {
    return this.request('POST', `/sessions/${encodeURIComponent(sessionId)}/connection-ticket`);
  }

  async listDevices(): Promise<Device[]> {
    return (await this.request<{ devices: Device[] }>('GET', '/devices')).devices;
  }

  async revokeDevice(id: string): Promise<void> {
    await this.request('DELETE', `/devices/${encodeURIComponent(id)}`);
  }

  /** WebSocket URL for a session. The ticket is single-use and short-lived. */
  terminalUrl(sessionId: string, ticket: string): string {
    const wsBase = this.baseUrl.replace(/^http/, 'ws');
    return `${wsBase}/api/v1/sessions/${encodeURIComponent(sessionId)}/terminal?ticket=${encodeURIComponent(ticket)}`;
  }
}
