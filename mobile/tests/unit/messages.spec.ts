import { describe, expect, test } from 'vitest';

import { AgentError, NETWORK_ERROR } from '@/services/errors';
import { describeError, describeSessionEnd, hasEnded } from '@/services/messages';
import { PairingError } from '@/services/pairing';
import type { Session } from '@/types/api';

describe('describeError', () => {
  test('explains the common failures', () => {
    expect(describeError(new AgentError(0, NETWORK_ERROR, 'x'))).toMatch(/Tailscale/);
    expect(describeError(new AgentError(401, 'PAIRING_EXPIRED', 'x'))).toMatch(/remote-shell pair/);
    expect(describeError(new AgentError(0, 'TIMEOUT', 'Saving timed out.'))).toMatch(/timed out.*try again/);
    expect(describeError(new AgentError(401, 'DEVICE_REVOKED', 'x'))).toMatch(/pair again/);
    expect(describeError(new AgentError(429, 'RATE_LIMITED', 'x', 30))).toMatch(/30 seconds/);
    expect(describeError(new AgentError(429, 'RATE_LIMITED', 'x'))).toMatch(/few minutes/);
  });

  test('passes through other agent and pairing messages', () => {
    expect(describeError(new AgentError(500, 'INTERNAL_ERROR', 'boom'))).toBe('boom');
    expect(describeError(new PairingError('not a pairing code'))).toBe('not a pairing code');
  });

  test('never leaks arbitrary error text', () => {
    expect(describeError(new Error('secret internals'))).toBe('Something went wrong.');
    expect(describeError('string')).toBe('Something went wrong.');
  });
});

const session = (over: Partial<Session>): Session => ({
  id: 'ses_1',
  name: 'sh',
  type: 'persistent',
  status: 'running',
  created_at: '',
  ...over,
});

describe('ended sessions', () => {
  test('only exited and expired sessions count as ended', () => {
    expect(hasEnded(session({ status: 'running' }))).toBe(false);
    expect(hasEnded(session({ status: 'disconnected' }))).toBe(false);
    expect(hasEnded(session({ status: 'exited' }))).toBe(true);
    expect(hasEnded(session({ status: 'expired' }))).toBe(true);
  });

  test('explains how each kind of session ended', () => {
    expect(describeSessionEnd(session({ status: 'exited', end_reason: 'process_exited', exit_code: 3 }))).toBe(
      'Exited (code 3)',
    );
    expect(describeSessionEnd(session({ status: 'exited', end_reason: 'process_exited' }))).toBe('Exited');
    expect(describeSessionEnd(session({ status: 'expired', end_reason: 'idle_timeout' }))).toBe(
      'Closed after being idle',
    );
    expect(describeSessionEnd(session({ status: 'exited', end_reason: 'agent_restarted' }))).toBe(
      'Ended when the agent restarted',
    );
    expect(describeSessionEnd(session({ status: 'expired' }))).toBe('Closed after being idle');
  });

  test('a closed session has its own message', () => {
    expect(describeError(new AgentError(410, 'SESSION_CLOSED', 'x'))).toBe('That terminal has ended.');
  });
});
