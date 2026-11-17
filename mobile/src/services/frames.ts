// Binary terminal frames: [type: 1 byte][sequence: 8 bytes big-endian][payload].

export const FRAME_INPUT = 0x01;
export const FRAME_OUTPUT = 0x02;
export const FRAME_HEADER = 9;

/** Builds a terminal input frame. Input frames carry sequence 0. */
export function encodeInput(data: Uint8Array): ArrayBuffer {
  const buf = new ArrayBuffer(FRAME_HEADER + data.length);
  new DataView(buf).setUint8(0, FRAME_INPUT);
  new Uint8Array(buf, FRAME_HEADER).set(data);
  return buf;
}

export interface OutputFrame {
  seq: number;
  data: Uint8Array;
}

/** Parses a terminal output frame. Payload bytes are kept as bytes. */
export function decodeOutput(buf: ArrayBuffer): OutputFrame {
  if (buf.byteLength < FRAME_HEADER) {
    throw new Error('terminal frame too short');
  }
  const view = new DataView(buf);
  if (view.getUint8(0) !== FRAME_OUTPUT) {
    throw new Error('unexpected terminal frame type');
  }
  const seq = view.getBigUint64(1, false);
  if (seq > BigInt(Number.MAX_SAFE_INTEGER)) {
    throw new Error('terminal sequence out of range');
  }
  return { seq: Number(seq), data: new Uint8Array(buf, FRAME_HEADER) };
}
