import type { CapacitorConfig } from '@capacitor/cli';

const config: CapacitorConfig = {
  appId: 'com.aleixp.remoteshell',
  appName: 'Remote Shell',
  webDir: 'dist',
  plugins: {
    // Native HTTP: the webview origin (capacitor://localhost) would otherwise
    // need CORS on the agent, which deliberately has none.
    CapacitorHttp: { enabled: true },
  },
};

export default config;
