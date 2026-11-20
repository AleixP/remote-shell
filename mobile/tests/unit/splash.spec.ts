import { beforeEach, describe, expect, test, vi } from 'vitest';

import { hideSplash } from '@/services/splash';

const hide = vi.fn();
let native = false;

vi.mock('@capacitor/core', () => ({ Capacitor: { isNativePlatform: () => native } }));
vi.mock('@capacitor/splash-screen', () => ({ SplashScreen: { hide: (...a: unknown[]) => hide(...a) } }));

beforeEach(() => {
  hide.mockReset();
  hide.mockResolvedValue(undefined);
  native = false;
});

describe('hideSplash', () => {
  test('on the phone it hides the launch screen with a short fade', async () => {
    native = true;
    await hideSplash();
    expect(hide).toHaveBeenCalledWith({ fadeOutDuration: 250 });
  });

  test('in a browser there is no launch screen, so it does nothing', async () => {
    await hideSplash();
    expect(hide).not.toHaveBeenCalled();
  });

  test('a failure to hide it never breaks the app', async () => {
    native = true;
    hide.mockRejectedValue(new Error('no plugin'));
    await expect(hideSplash()).resolves.toBeUndefined();
  });
});
