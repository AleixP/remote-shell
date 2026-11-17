import { describe, expect, test, vi } from 'vitest';

import { AgentApi } from '@/services/api';
import { AgentError, NETWORK_ERROR } from '@/services/errors';

function jsonResponse(status: number, body: unknown, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), { status, headers });
}

function setup(response: Response | Error, token: string | null = 'tok') {
  const fetchFn = vi.fn(async () => {
    if (response instanceof Error) throw response;
    return response;
  });
  return { api: new AgentApi('https://mac.ts.net:8443', () => token ?? undefined, fetchFn), fetchFn };
}

describe('AgentApi', () => {
  test('sends the bearer token and parses the response', async () => {
    const { api, fetchFn } = setup(jsonResponse(200, { sessions: [{ id: 'ses_1' }] }));
    expect(await api.listSessions()).toEqual([{ id: 'ses_1' }]);
    const [url, init] = fetchFn.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('https://mac.ts.net:8443/api/v1/sessions');
    expect((init.headers as Record<string, string>)['Authorization']).toBe('Bearer tok');
  });

  test('pairing and health do not send credentials', async () => {
    const { api, fetchFn } = setup(
      jsonResponse(201, { device_id: 'dev_1', access_token: 't', token_type: 'Bearer' }),
      null,
    );
    await api.pair('CODE', 'iPhone');
    const [, init] = fetchFn.mock.calls[0] as unknown as [string, RequestInit];
    expect((init.headers as Record<string, string>)['Authorization']).toBeUndefined();
    expect(JSON.parse(init.body as string)).toEqual({ pairing_code: 'CODE', device_name: 'iPhone' });
  });

  test('refuses protected calls without a token, without hitting the network', async () => {
    const { api, fetchFn } = setup(jsonResponse(200, {}), null);
    await expect(api.listSessions()).rejects.toMatchObject({ code: 'UNAUTHORIZED' });
    expect(fetchFn).not.toHaveBeenCalled();
  });

  test('maps agent errors', async () => {
    const { api } = setup(
      jsonResponse(429, { error: { code: 'RATE_LIMITED', message: 'slow down' } }, { 'Retry-After': '30' }),
    );
    const err = await api.createSession({ type: 'persistent' }).catch((e) => e);
    expect(err).toBeInstanceOf(AgentError);
    expect(err).toMatchObject({ status: 429, code: 'RATE_LIMITED', message: 'slow down', retryAfter: 30 });
  });

  test('maps network failures and non-JSON errors', async () => {
    const net = await setup(new TypeError('failed'))
      .api.health()
      .catch((e) => e);
    expect(net).toMatchObject({ status: 0, code: NETWORK_ERROR });
    const html = await setup(new Response('<html>', { status: 502 }))
      .api.listSessions()
      .catch((e) => e);
    expect(html).toMatchObject({ status: 502, code: 'INTERNAL_ERROR' });
  });

  test('escapes ids in paths and builds the WebSocket URL', async () => {
    const { api, fetchFn } = setup(jsonResponse(200, { success: true }));
    await api.closeSession('ses_a/../b');
    expect((fetchFn.mock.calls[0] as unknown as [string])[0]).toContain('/sessions/ses_a%2F..%2Fb');
    expect(api.terminalUrl('ses_1', 'a+b/c')).toBe(
      'wss://mac.ts.net:8443/api/v1/sessions/ses_1/terminal?ticket=a%2Bb%2Fc',
    );
  });
});
