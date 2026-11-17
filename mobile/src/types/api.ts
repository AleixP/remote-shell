// Types of the agent's HTTP API (docs/WEBSOCKET_PROTOCOL.md).

export type SessionType = 'persistent' | 'temporary';
export type SessionStatus = 'running' | 'disconnected' | 'closing' | 'exited' | 'expired';

export interface Session {
  id: string;
  name: string;
  type: SessionType;
  status: SessionStatus;
  created_at: string;
  last_activity_at?: string;
}

export interface CreateSessionRequest {
  name?: string;
  type: SessionType;
  shell?: string;
  cols?: number;
  rows?: number;
}

export interface Device {
  id: string;
  name: string;
  created_at: string;
  last_used_at: string | null;
  current: boolean;
}

export interface PairResult {
  device_id: string;
  access_token: string;
  token_type: 'Bearer';
}

export interface ConnectionTicket {
  ticket: string;
  expires_at: string;
}

export interface Health {
  status: string;
  version: string;
}
