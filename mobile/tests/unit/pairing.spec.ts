import { describe, expect, test } from 'vitest';

import { PairingError, parsePairingUrl } from '@/services/pairing';

const CODE = 'MFRGGZDFMZTWQ2LKNNWG23TPOM';
const valid = `remoteshell://pair?v=1&host=mac.tail1234.ts.net%3A8443&code=${CODE}&name=My%20Mac`;

describe('parsePairingUrl', () => {
  test('parses a valid pairing URL', () => {
    expect(parsePairingUrl(valid)).toEqual({
      baseUrl: 'https://mac.tail1234.ts.net:8443',
      host: 'mac.tail1234.ts.net:8443',
      code: CODE,
      name: 'My Mac',
    });
  });

  test('tolerates surrounding whitespace and a missing name', () => {
    const info = parsePairingUrl(`  remoteshell://pair?v=1&host=mac.ts.net&code=${CODE}\n`);
    expect(info.name).toBe('mac.ts.net');
    expect(info.baseUrl).toBe('https://mac.ts.net');
  });

  test.each([
    ['not a url', 'hello'],
    ['other scheme', `https://pair?v=1&host=mac.ts.net&code=${CODE}`],
    ['other action', `remoteshell://other?v=1&host=mac.ts.net&code=${CODE}`],
    ['unknown version', `remoteshell://pair?v=2&host=mac.ts.net&code=${CODE}`],
    ['missing host', `remoteshell://pair?v=1&code=${CODE}`],
    ['userinfo in host', `remoteshell://pair?v=1&host=evil.com%40mac.ts.net&code=${CODE}`],
    ['path in host', `remoteshell://pair?v=1&host=mac.ts.net%2Fx&code=${CODE}`],
    ['space in host', `remoteshell://pair?v=1&host=mac%20x.ts.net&code=${CODE}`],
    ['port zero', `remoteshell://pair?v=1&host=mac.ts.net%3A0&code=${CODE}`],
    ['port too large', `remoteshell://pair?v=1&host=mac.ts.net%3A99999&code=${CODE}`],
    ['short code', 'remoteshell://pair?v=1&host=mac.ts.net&code=ABC'],
    ['lowercase code', `remoteshell://pair?v=1&host=mac.ts.net&code=${CODE.toLowerCase()}`],
  ])('rejects %s', (_label, input) => {
    expect(() => parsePairingUrl(input)).toThrow(PairingError);
  });
});
