import { describe, expect, test } from 'vitest';

import { AgentError, NETWORK_ERROR } from '@/services/errors';
import { describeError } from '@/services/messages';
import { PairingError } from '@/services/pairing';

describe('describeError', () => {
  test('explains the common failures', () => {
    expect(describeError(new AgentError(0, NETWORK_ERROR, 'x'))).toMatch(/Tailscale/);
    expect(describeError(new AgentError(401, 'PAIRING_EXPIRED', 'x'))).toMatch(/remote-shell pair/);
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
