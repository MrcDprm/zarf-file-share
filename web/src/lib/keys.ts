// The input key of each mode (PROTOCOL.md sections 3-4), matching the Go package.

import { argon2idAsync } from "@noble/hashes/argon2.js";
import { concat, fromHex, own, utf8 } from "./bytes.ts";
import { hkdf, type ArgonParams } from "./envelope.ts";

/** A link without a password: the 32 random bytes after "#". */
export const linkKey = (fileKey: Uint8Array): Uint8Array => fileKey.slice();

/**
 * A link with a password needs both: the link key and the password's Argon2id output.
 * The async version gives the page time to repaint, so a phone does not freeze while it runs.
 */
export async function passwordKey(fileKey: Uint8Array, password: string, a: ArgonParams, onProgress?: (p: number) => void): Promise<Uint8Array> {
  const pw = await argon2idAsync(utf8(password), a.salt, { t: a.time, m: a.memoryKiB, p: a.threads, dkLen: 32, asyncTick: 25, onProgress });
  return concat(fileKey, pw);
}

/** What a downloader shows the server to spend a download; unrelated to the encryption keys. */
export const downloadAuth = (fileKey: Uint8Array): Promise<Uint8Array> => hkdf(fileKey, new Uint8Array(), "zarf v1 download auth");

// --- X25519 for request mode ---

const subtle = globalThis.crypto.subtle;
// PKCS#8 wrapping of a raw X25519 private key (RFC 8410): Web Crypto does not import raw private keys.
const PKCS8_X25519 = fromHex("302e020100300506032b656e04220420");

export interface X25519Pair {
  privateKey: CryptoKey;
  publicKey: Uint8Array;
}

export async function generateX25519(): Promise<X25519Pair> {
  const pair = (await subtle.generateKey({ name: "X25519" }, false, ["deriveBits"])) as CryptoKeyPair;
  return { privateKey: pair.privateKey, publicKey: new Uint8Array(await subtle.exportKey("raw", pair.publicKey)) };
}

/** Imports a raw 32-byte private key (test vectors, a key kept by the user). */
export async function importX25519(raw: Uint8Array): Promise<X25519Pair> {
  const privateKey = await subtle.importKey("pkcs8", concat(PKCS8_X25519, raw), { name: "X25519" }, true, ["deriveBits"]);
  const jwk = await subtle.exportKey("jwk", privateKey);
  const publicKey = Uint8Array.from(atob(jwk.x!.replace(/-/g, "+").replace(/_/g, "/")), (c) => c.charCodeAt(0));
  return { privateKey, publicKey };
}

/**
 * The input key of request mode: the X25519 shared secret bound to both public keys, as in HPKE.
 * The sender uses its one-time key and the requester's public key; the requester the reverse.
 */
export async function requestKey(privateKey: CryptoKey, peerPublic: Uint8Array, ephPub: Uint8Array, reqPub: Uint8Array): Promise<Uint8Array> {
  const peer = await subtle.importKey("raw", own(peerPublic), { name: "X25519" }, false, []);
  let shared: Uint8Array;
  try {
    shared = new Uint8Array(await subtle.deriveBits({ name: "X25519", public: peer }, privateKey, 256));
  } catch {
    throw new Error("invalid public key");
  }
  if (shared.every((b) => b === 0)) throw new Error("invalid public key"); // a malicious low-order key
  return concat(shared, ephPub, reqPub);
}
