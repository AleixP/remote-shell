import { createPinia, setActivePinia } from 'pinia';
import { beforeEach, describe, expect, test } from 'vitest';

import { setServices } from '@/services/container';
import { MemoryStore } from '@/services/storage';
import { useComputersStore } from '@/stores/computers';
import { useSessionsStore } from '@/stores/sessions';
import { useWorkspaceStore } from '@/stores/workspace';

const BASE = 'https://mac.ts.net:8443';
const info = { baseUrl: BASE, host: 'mac.ts.net:8443', code: 'C'.repeat(26), name: 'My Mac' };

interface Call {
  method: string;
  url: string;
  auth: string | undefined;
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status });
}

/** A fake agent: a handler per "METHOD /path" plus a log of requests. */
function fakeAgent(handlers: Record<string, (init: RequestInit) => Response | Error>) {
  const calls: Call[] = [];
  const fetchFn = async (input: string, init: RequestInit = {}) => {
    const path = input.replace(BASE + '/api/v1', '');
    const headers = (init.headers ?? {}) as Record<string, string>;
    calls.push({ method: init.method ?? 'GET', url: path, auth: headers['Authorization'] });
    const h = handlers[`${init.method ?? 'GET'} ${path}`];
    if (!h) return json(404, { error: { code: 'NOT_FOUND', message: 'x' } });
    const res = h(init);
    if (res instanceof Error) throw res;
    return res;
  };
  return { fetchFn, calls };
}

let secure: MemoryStore;
let prefs: MemoryStore;

function useAgent(agent: ReturnType<typeof fakeAgent>) {
  setServices({ secure, prefs, fetch: agent.fetchFn, deviceName: async () => 'iPhone' });
}

/** Simulates restarting the app: new store instances over the same storage. */
function restart() {
  setActivePinia(createPinia());
  return useComputersStore();
}

const pairOk = () => json(201, { device_id: 'dev_1', access_token: 'secret-token', token_type: 'Bearer' });

beforeEach(() => {
  secure = new MemoryStore();
  prefs = new MemoryStore();
  setActivePinia(createPinia());
});

describe('computers store', () => {
  test('pairing keeps the token in secure storage only', async () => {
    const agent = fakeAgent({ 'POST /pair': pairOk });
    useAgent(agent);
    const store = useComputersStore();
    const computer = await store.pair(info);

    expect(computer).toMatchObject({ id: 'dev_1', name: 'My Mac', baseUrl: BASE });
    expect(await secure.get('token_dev_1')).toBe('secret-token');
    expect(await prefs.get('computers')).not.toContain('secret-token');
    expect(JSON.stringify(store.computers)).not.toContain('secret-token');
  });

  test('if the credential cannot be saved, nothing is listed and the new device is revoked', async () => {
    const agent = fakeAgent({
      'POST /pair': pairOk,
      'DELETE /devices/dev_1': () => json(200, { success: true }),
    });
    useAgent(agent);
    secure.set = async () => {
      throw new Error('keychain unavailable');
    };
    const store = useComputersStore();
    await expect(store.pair(info)).rejects.toThrow('keychain unavailable');
    expect(store.computers).toEqual([]);
    expect(agent.calls.at(-1)).toMatchObject({ method: 'DELETE', url: '/devices/dev_1', auth: 'Bearer secret-token' });
  });

  test('sends the device name and pairing code, without credentials', async () => {
    let body: unknown;
    const agent = fakeAgent({
      'POST /pair': (init) => {
        body = JSON.parse(init.body as string);
        return pairOk();
      },
    });
    useAgent(agent);
    await useComputersStore().pair(info);
    expect(body).toEqual({ pairing_code: info.code, device_name: 'iPhone' });
    expect(agent.calls[0].auth).toBeUndefined();
  });

  test('a failed pairing stores nothing', async () => {
    useAgent(fakeAgent({ 'POST /pair': () => json(401, { error: { code: 'PAIRING_EXPIRED', message: 'x' } }) }));
    const store = useComputersStore();
    await expect(store.pair(info)).rejects.toMatchObject({ code: 'PAIRING_EXPIRED' });
    expect(store.computers).toEqual([]);
    expect(await prefs.get('computers')).toBeNull();
  });

  test('computers and tokens survive an app restart', async () => {
    const agent = fakeAgent({
      'POST /pair': pairOk,
      'GET /sessions': () => json(200, { sessions: [] }),
    });
    useAgent(agent);
    await useComputersStore().pair(info);

    const store = restart();
    expect(store.computers).toEqual([]); // not loaded yet
    await store.load();
    expect(store.computers).toHaveLength(1);
    await (await store.apiFor('dev_1')).listSessions();
    expect(agent.calls.at(-1)?.auth).toBe('Bearer secret-token');
  });

  test('pairing the same computer again replaces the old entry and token', async () => {
    let n = 0;
    useAgent(
      fakeAgent({
        'POST /pair': () => json(201, { device_id: `dev_${++n}`, access_token: `token-${n}`, token_type: 'Bearer' }),
      }),
    );
    const store = useComputersStore();
    await store.pair(info);
    await store.pair(info);
    expect(store.computers.map((c) => c.id)).toEqual(['dev_2']);
    expect(await secure.get('token_dev_1')).toBeNull();
    expect(await secure.get('token_dev_2')).toBe('token-2');
  });

  test('removing with revocation revokes this phone on the agent first', async () => {
    const agent = fakeAgent({
      'POST /pair': pairOk,
      'DELETE /devices/dev_1': () => json(200, { success: true }),
    });
    useAgent(agent);
    const store = useComputersStore();
    await store.pair(info);

    expect(await store.remove('dev_1', { revoke: true })).toBe(true);
    expect(agent.calls.at(-1)).toMatchObject({ method: 'DELETE', url: '/devices/dev_1', auth: 'Bearer secret-token' });
    expect(store.computers).toEqual([]);
    expect(await secure.get('token_dev_1')).toBeNull();
    expect(JSON.parse((await prefs.get('computers')) ?? '[]')).toEqual([]);
  });

  test('removing still works offline but reports that revocation failed', async () => {
    useAgent(fakeAgent({ 'POST /pair': pairOk, 'DELETE /devices/dev_1': () => new TypeError('offline') }));
    const store = useComputersStore();
    await store.pair(info);
    expect(await store.remove('dev_1', { revoke: true })).toBe(false);
    expect(store.computers).toEqual([]);
    expect(await secure.get('token_dev_1')).toBeNull();
  });

  test('removing without revocation does not contact the agent', async () => {
    const agent = fakeAgent({ 'POST /pair': pairOk });
    useAgent(agent);
    const store = useComputersStore();
    await store.pair(info);
    const before = agent.calls.length;
    expect(await store.remove('dev_1', { revoke: false })).toBe(true);
    expect(agent.calls.length).toBe(before);
  });

  test('reports online, offline and revoked computers', async () => {
    let mode: 'ok' | 'down' | 'revoked' = 'ok';
    useAgent(
      fakeAgent({
        'POST /pair': pairOk,
        'GET /sessions': () =>
          mode === 'ok'
            ? json(200, { sessions: [] })
            : mode === 'down'
              ? new TypeError('offline')
              : json(401, { error: { code: 'DEVICE_REVOKED', message: 'x' } }),
      }),
    );
    const store = useComputersStore();
    await store.pair(info);
    expect(await store.checkStatus('dev_1')).toBe('online');
    mode = 'down';
    expect(await store.checkStatus('dev_1')).toBe('offline');
    mode = 'revoked';
    expect(await store.checkStatus('dev_1')).toBe('revoked');
    expect(store.status['dev_1']).toBe('revoked');
  });

  test('a computer whose token is missing needs to be paired again', async () => {
    useAgent(fakeAgent({ 'POST /pair': pairOk }));
    const store = useComputersStore();
    await store.pair(info);
    await secure.remove('token_dev_1'); // e.g. credentials lost
    const fresh = restart();
    await fresh.load();
    expect(await fresh.checkStatus('dev_1')).toBe('revoked');
  });

  test('ignores corrupt or unsafe stored data', async () => {
    useAgent(fakeAgent({}));
    await prefs.set(
      'computers',
      JSON.stringify([
        { id: 'a', name: 'ok', baseUrl: BASE, pairedAt: '' },
        { id: 'b', name: 'plain http', baseUrl: 'http://mac.ts.net', pairedAt: '' },
        { nope: true },
      ]),
    );
    const store = useComputersStore();
    await store.load();
    expect(store.computers.map((c) => c.id)).toEqual(['a']);

    await prefs.set('computers', '{not json');
    await restart().load();
    expect(useComputersStore().computers).toEqual([]);
  });
});

describe('sessions store', () => {
  test('lists, creates and closes sessions of a computer', async () => {
    const session = { id: 'ses_1', name: 'sh', type: 'persistent', status: 'running', created_at: '' };
    const agent = fakeAgent({
      'POST /pair': pairOk,
      'GET /sessions': () => json(200, { sessions: [] }),
      'POST /sessions': () => json(201, { session }),
      'DELETE /sessions/ses_1': () => json(200, { success: true }),
    });
    useAgent(agent);
    await useComputersStore().pair(info);
    const sessions = useSessionsStore();

    expect(await sessions.refresh('dev_1')).toEqual([]);
    await sessions.create('dev_1', { type: 'persistent' });
    expect(sessions.byComputer['dev_1']).toEqual([session]);
    await sessions.close('dev_1', 'ses_1');
    expect(sessions.byComputer['dev_1']).toEqual([]);
  });

  test('refreshing closes the tabs of sessions that ended or disappeared', async () => {
    const running = { id: 'ses_run', name: 'a', type: 'persistent', status: 'running', created_at: '' };
    const ended = {
      id: 'ses_end',
      name: 'b',
      type: 'persistent',
      status: 'exited',
      created_at: '',
      end_reason: 'agent_restarted',
    };
    useAgent(
      fakeAgent({
        'POST /pair': pairOk,
        'GET /sessions': () => json(200, { sessions: [running, ended] }),
      }),
    );
    await useComputersStore().pair(info);
    const workspace = useWorkspaceStore();
    ['ses_run', 'ses_end', 'ses_gone'].forEach((id) => workspace.open('dev_1', id));

    await useSessionsStore().refresh('dev_1');
    expect(workspace.list('dev_1')).toEqual(['ses_run']);
  });
});
