import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

import { withTimeout } from '@/services/timeout';

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe('withTimeout', () => {
  test('passes through a result that arrives in time', async () => {
    await expect(withTimeout(Promise.resolve(42), 1000, 'x')).resolves.toBe(42);
  });

  test('passes through the original error', async () => {
    await expect(withTimeout(Promise.reject(new Error('boom')), 1000, 'x')).rejects.toThrow('boom');
  });

  test('rejects with a TIMEOUT error when the promise hangs', async () => {
    const hung = new Promise<void>(() => {});
    const result = withTimeout(hung, 1000, 'Saving credentials');
    const assertion = expect(result).rejects.toMatchObject({
      code: 'TIMEOUT',
      message: 'Saving credentials timed out.',
    });
    await vi.advanceTimersByTimeAsync(1000);
    await assertion;
  });
});
