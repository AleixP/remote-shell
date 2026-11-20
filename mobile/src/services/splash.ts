import { Capacitor } from '@capacitor/core';

/**
 * Hides the launch screen. It is kept up (`launchAutoHide: false` in the
 * Capacitor config) until the first screen has been drawn: without that, the
 * system removes it as soon as the app starts and the empty web view shows,
 * which looks like a black screen while the interface loads.
 */
export async function hideSplash(): Promise<void> {
  if (!Capacitor.isNativePlatform()) return;
  try {
    const { SplashScreen } = await import('@capacitor/splash-screen');
    await SplashScreen.hide({ fadeOutDuration: 250 });
  } catch {
    // Nothing to hide, or the plugin is missing: the app works either way.
  }
}
