import { describe, expect, test } from 'vitest';

import { decodeOutput, encodeInput, FRAME_HEADER, FRAME_INPUT, FRAME_OUTPUT } from '@/services/frames';

function outputFrame(seq: bigint, payload: number[]): ArrayBuffer {
  const buf = new ArrayBuffer(FRAME_HEADER + payload.length);
  const view = new DataView(buf);
  view.setUint8(0, FRAME_OUTPUT);
  view.setBigUint64(1, seq, false);
  new Uint8Array(buf, FRAME_HEADER).set(payload);
  return buf;
}

describe('frames', () => {
  test('encodes input with type 0x01 and sequence 0', () => {
    const buf = encodeInput(new Uint8Array([0x61, 0x0d]));
    const bytes = new Uint8Array(buf);
    expect(bytes[0]).toBe(FRAME_INPUT);
    expect([...bytes.slice(1, 9)]).toEqual([0, 0, 0, 0, 0, 0, 0, 0]);
    expect([...bytes.slice(9)]).toEqual([0x61, 0x0d]);
  });

  test('decodes output with a big-endian sequence', () => {
    const f = decodeOutput(outputFrame(0x0102n, [1, 2, 3]));
    expect(f.seq).toBe(258);
    expect([...f.data]).toEqual([1, 2, 3]);
  });

  test('keeps non-UTF-8 bytes intact', () => {
    const f = decodeOutput(outputFrame(1n, [0xff, 0xe2, 0x82]));
    expect([...f.data]).toEqual([0xff, 0xe2, 0x82]);
  });

  test('rejects malformed frames', () => {
    expect(() => decodeOutput(new ArrayBuffer(3))).toThrow();
    const wrongType = outputFrame(1n, []);
    new DataView(wrongType).setUint8(0, FRAME_INPUT);
    expect(() => decodeOutput(wrongType)).toThrow();
    expect(() => decodeOutput(outputFrame(2n ** 60n, []))).toThrow();
  });
});
