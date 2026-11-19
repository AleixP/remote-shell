import { Capacitor } from '@capacitor/core';

/** Reads the system clipboard as text. Empty when it holds nothing readable. */
export async function readClipboard(): Promise<string> {
  if (Capacitor.isNativePlatform()) {
    const { Clipboard } = await import('@capacitor/clipboard');
    const { type, value } = await Clipboard.read();
    return type === 'text/plain' ? (value ?? '') : '';
  }
  return (await navigator.clipboard?.readText?.()) ?? '';
}

export async function writeClipboard(text: string): Promise<void> {
  if (Capacitor.isNativePlatform()) {
    const { Clipboard } = await import('@capacitor/clipboard');
    await Clipboard.write({ string: text });
    return;
  }
  await navigator.clipboard.writeText(text);
}
