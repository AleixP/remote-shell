/** An error answered by the agent, or a network failure (status 0). */
export class AgentError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    /** Seconds to wait before retrying, for RATE_LIMITED. */
    readonly retryAfter?: number,
  ) {
    super(message);
    this.name = 'AgentError';
  }
}

export const NETWORK_ERROR = 'NETWORK_ERROR';
