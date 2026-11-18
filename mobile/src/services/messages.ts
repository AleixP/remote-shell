import { AgentError, NETWORK_ERROR } from './errors';
import { PairingError } from './pairing';
import { TIMEOUT } from './timeout';

import type { Session } from '@/types/api';

/** A user-facing explanation of an error. */
export function describeError(err: unknown): string {
  if (err instanceof PairingError) return err.message;
  if (err instanceof AgentError) {
    switch (err.code) {
      case NETWORK_ERROR:
        return 'Cannot reach the computer. Check that Tailscale is connected on this phone and that the agent is running.';
      case TIMEOUT:
        return `${err.message} Check the connection and try again.`;
      case 'PAIRING_EXPIRED':
        return 'The pairing code is invalid, expired or already used. Run "remote-shell pair" on the computer and scan the new QR code.';
      case 'DEVICE_REVOKED':
        return 'This phone no longer has access to the computer. Remove it and pair again.';
      case 'UNAUTHORIZED':
        return 'This phone is not authorized on the computer. Remove it and pair again.';
      case 'RATE_LIMITED':
        return err.retryAfter
          ? `Too many attempts. Try again in ${err.retryAfter} seconds.`
          : 'Too many attempts. Try again in a few minutes.';
      case 'SESSION_LIMIT_REACHED':
        return 'The computer reached its maximum number of terminals. End one and try again.';
      case 'SHELL_NOT_AVAILABLE':
        return 'That shell is not available on the computer.';
      case 'SESSION_CLOSED':
        return 'That terminal has ended.';
      case 'SESSION_NOT_FOUND':
        return 'That terminal no longer exists.';
      default:
        return err.message || 'Something went wrong.';
    }
  }
  return 'Something went wrong.';
}

/** True for a session whose shell is gone (it can no longer be opened). */
export function hasEnded(s: Session): boolean {
  return s.status === 'exited' || s.status === 'expired';
}

/** A short explanation of how an ended session ended. */
export function describeSessionEnd(s: Session): string {
  switch (s.end_reason) {
    case 'process_exited':
      return s.exit_code === undefined ? 'Exited' : `Exited (code ${s.exit_code})`;
    case 'idle_timeout':
      return 'Closed after being idle';
    case 'agent_restarted':
      return 'Ended when the agent restarted';
    default:
      return s.status === 'expired' ? 'Closed after being idle' : 'Ended';
  }
}
