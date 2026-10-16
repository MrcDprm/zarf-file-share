// CPace, the password-authenticated key exchange of live mode (draft-irtf-cfrg-cpace-20),
// with ristretto255 and SHA-512. Both sides turn the short code into a secret group
// generator; someone without the code can do no better than one guess per session.
// The test vectors of the draft's appendix B.3 check every step.

import { ristretto255, ristretto255_hasher } from "@noble/curves/ed25519.js";
import { sha512 } from "@noble/hashes/sha2.js";
import { concat, utf8 } from "./bytes.ts";

const DSI = utf8("CPaceRistretto255");
const SHA512_BLOCK = 128;
const Point = ristretto255.Point;
type Point = InstanceType<typeof Point>;

/** The length of the data, in LEB128, then the data. */
export function prependLen(data: Uint8Array): Uint8Array {
  const len: number[] = [];
  let n = data.length;
  do {
    len.push((n & 0x7f) | (n >= 0x80 ? 0x80 : 0));
    n >>>= 7;
  } while (n > 0);
  return concat(Uint8Array.from(len), data);
}

export const lvCat = (...parts: Uint8Array[]): Uint8Array => concat(...parts.map(prependLen));

export function generatorString(prs: Uint8Array, ci: Uint8Array, sid: Uint8Array): Uint8Array {
  const zpad = Math.max(0, SHA512_BLOCK - prependLen(prs).length - prependLen(DSI).length - 1);
  return lvCat(DSI, prs, new Uint8Array(zpad), ci, sid);
}

export const calculateGenerator = (prs: Uint8Array, ci: Uint8Array, sid: Uint8Array): Point =>
  ristretto255_hasher.deriveToCurve!(sha512(generatorString(prs, ci, sid))) as Point;

const fromLE = (b: Uint8Array): bigint => b.reduceRight((acc, byte) => (acc << 8n) | BigInt(byte), 0n);

/** A random scalar below 2^252, as the draft recommends: 32 random bytes, top bits cleared. */
export function sampleScalar(): bigint {
  for (;;) {
    const b = crypto.getRandomValues(new Uint8Array(32));
    b[31]! &= 0x0f;
    const y = fromLE(b);
    if (y !== 0n) return y;
  }
}

export const scalarFromLE = fromLE;

export class CPaceError extends Error {
  constructor() {
    super("invalid point");
    this.name = "CPaceError";
  }
}

/** y times the peer's point; an invalid encoding or the neutral element aborts the session. */
export function scalarMultVfy(y: bigint, encoded: Uint8Array): Uint8Array {
  let p: Point;
  try {
    p = Point.fromBytes(encoded) as Point;
  } catch {
    throw new CPaceError();
  }
  if (p.equals(Point.ZERO)) throw new CPaceError();
  const k = p.multiply(y);
  if (k.equals(Point.ZERO)) throw new CPaceError();
  return k.toBytes();
}

export interface Share {
  y: bigint;
  Y: Uint8Array; // what is sent to the peer
}

export function share(prs: Uint8Array, ci: Uint8Array, sid: Uint8Array, y = sampleScalar()): Share {
  return { y, Y: calculateGenerator(prs, ci, sid).multiply(y).toBytes() };
}

/**
 * The intermediate session key, with the initiator/responder transcript: A's message first,
 * whichever side is computing.
 */
export function isk(own: Share, peerY: Uint8Array, sid: Uint8Array, ya: Uint8Array, ada: Uint8Array, yb: Uint8Array, adb: Uint8Array): Uint8Array {
  const k = scalarMultVfy(own.y, peerY);
  const transcript = concat(lvCat(ya, ada), lvCat(yb, adb));
  return sha512(concat(lvCat(concat(DSI, utf8("_ISK")), sid, k), transcript));
}
