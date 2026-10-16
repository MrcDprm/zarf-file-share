// Zarf envelopes in the browser: the same bytes as the Go package (docs/PROTOCOL.md, section 2),
// built on the Web Crypto API. The shared test vectors keep the two implementations identical.

import { concat, equal, own, readU32, u32, utf8 } from "./bytes.ts";

export const Mode = { Link: 1, LinkPassword: 2, Request: 3, Live: 4 } as const;
export type Mode = (typeof Mode)[keyof typeof Mode];

export type ErrorCode = "header" | "wrong_key" | "auth" | "truncated" | "trailing";

/** Every way an envelope can be refused, with the same names the Go code and the vectors use. */
export class ZarfError extends Error {
  readonly code: ErrorCode;
  constructor(code: ErrorCode, message?: string) {
    super(message ?? code);
    this.name = "ZarfError";
    this.code = code;
  }
}

const MAGIC = utf8("ZARF");
const VERSION = 1;
export const DEFAULT_CHUNK_LOG2 = 16; // 64 KiB
const MIN_CHUNK_LOG2 = 12;
const MAX_CHUNK_LOG2 = 20;
const TAG = 16;
const MAX_META_CIPHER = 64 * 1024;
const META_BLOCK = 1024;
// Limits on what a header may ask for, so a hostile file cannot exhaust memory or time.
export const MAX_ARGON_MEMORY_KIB = 1024 * 1024;
const MAX_ARGON_TIME = 10;
const MAX_ARGON_THREADS = 4;

export interface ArgonParams {
  memoryKiB: number;
  time: number;
  threads: number;
  salt: Uint8Array; // 16 bytes
}

/** The first OWASP setting: 46 MB, one pass, one thread. */
export const DEFAULT_ARGON = { memoryKiB: 47104, time: 1, threads: 1 } as const;

export interface Header {
  mode: Mode;
  chunkLog2: number;
  salt: Uint8Array; // 32 bytes
  argon?: ArgonParams; // mode 2
  ephPub?: Uint8Array; // mode 3
  metaLen: number;
  commitment: Uint8Array;
}

export interface FileInfo {
  name: string;
  size: number;
  type?: string;
}

export interface Metadata {
  files: FileInfo[];
  zip?: boolean;
}

// --- header ---

const BASE = 4 + 1 + 1 + 1 + 32;

function paramsLength(mode: number): number {
  return mode === Mode.LinkPassword ? 4 + 4 + 1 + 16 : mode === Mode.Request ? 32 : 0;
}

function marshal(h: Header, withCommitment: boolean): Uint8Array<ArrayBuffer> {
  const parts: Uint8Array[] = [MAGIC, Uint8Array.of(VERSION, h.mode, h.chunkLog2), h.salt];
  if (h.mode === Mode.LinkPassword) {
    const a = h.argon!;
    parts.push(u32(a.memoryKiB), u32(a.time), Uint8Array.of(a.threads), a.salt);
  } else if (h.mode === Mode.Request) {
    parts.push(h.ephPub!);
  }
  parts.push(u32(h.metaLen));
  if (withCommitment) parts.push(h.commitment);
  return concat(...parts);
}

function check(h: Header): void {
  if (h.mode === Mode.LinkPassword) {
    const a = h.argon!;
    if (a.memoryKiB < 8 || a.memoryKiB > MAX_ARGON_MEMORY_KIB || a.time < 1 || a.time > MAX_ARGON_TIME || a.threads < 1 || a.threads > MAX_ARGON_THREADS) {
      throw new ZarfError("header", "password hash settings out of range");
    }
  } else if (!(h.mode === Mode.Link || h.mode === Mode.Request || h.mode === Mode.Live)) {
    throw new ZarfError("header", "unknown mode");
  }
  if (h.chunkLog2 < MIN_CHUNK_LOG2 || h.chunkLog2 > MAX_CHUNK_LOG2) throw new ZarfError("header", "chunk size out of range");
  if (h.metaLen < TAG || h.metaLen > MAX_META_CIPHER) throw new ZarfError("header", "metadata size out of range");
}

/** Parses the header at the start of b; null when more bytes are needed. Returns the header and its length. */
export function parseHeader(b: Uint8Array): { header: Header; length: number } | null {
  if (b.length < BASE) return null;
  if (!equal(b.subarray(0, 4), MAGIC)) throw new ZarfError("header", "not a Zarf envelope");
  if (b[4] !== VERSION) throw new ZarfError("header", "unsupported version");
  const mode = b[5]!;
  const length = BASE + paramsLength(mode) + 4 + 32;
  if (b.length < length) return null;
  const h: Header = { mode: mode as Mode, chunkLog2: b[6]!, salt: b.slice(7, BASE), metaLen: 0, commitment: new Uint8Array() };
  let at = BASE;
  if (mode === Mode.LinkPassword) {
    h.argon = { memoryKiB: readU32(b, at), time: readU32(b, at + 4), threads: b[at + 8]!, salt: b.slice(at + 9, at + 25) };
    at += 25;
  } else if (mode === Mode.Request) {
    h.ephPub = b.slice(at, at + 32);
    at += 32;
  }
  h.metaLen = readU32(b, at);
  h.commitment = b.slice(at + 4, at + 36);
  check(h);
  return { header: h, length };
}

// --- keys (PROTOCOL.md 2.2 and 2.3) ---

const subtle = globalThis.crypto.subtle;

interface Keys {
  content: CryptoKey;
  meta: CryptoKey;
  commit: Uint8Array<ArrayBuffer>;
}

export async function hkdf(ikm: Uint8Array, salt: Uint8Array, info: string): Promise<Uint8Array<ArrayBuffer>> {
  const base = await subtle.importKey("raw", own(ikm), "HKDF", false, ["deriveBits"]);
  const bits = await subtle.deriveBits({ name: "HKDF", hash: "SHA-256", salt: own(salt), info: utf8(info) }, base, 256);
  return new Uint8Array(bits);
}

async function aesKey(raw: Uint8Array<ArrayBuffer>): Promise<CryptoKey> {
  return subtle.importKey("raw", raw, "AES-GCM", false, ["encrypt", "decrypt"]);
}

async function deriveKeys(salt: Uint8Array, ikm: Uint8Array): Promise<Keys> {
  const [content, meta, commit] = await Promise.all([
    hkdf(ikm, salt, "zarf v1 content"),
    hkdf(ikm, salt, "zarf v1 metadata"),
    hkdf(ikm, salt, "zarf v1 commitment"),
  ]);
  return { content: await aesKey(content), meta: await aesKey(meta), commit };
}

export async function sha256(b: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  return new Uint8Array(await subtle.digest("SHA-256", own(b)));
}

async function commitment(commitKey: Uint8Array<ArrayBuffer>, h: Header): Promise<Uint8Array<ArrayBuffer>> {
  const key = await subtle.importKey("raw", commitKey, { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const start = await sha256(marshal(h, false));
  return new Uint8Array(await subtle.sign("HMAC", key, concat(utf8("zarf v1 commit"), start)));
}

/** Checks an input key against the header's commitment without touching any ciphertext. */
export async function keyMatches(h: Header, ikm: Uint8Array): Promise<boolean> {
  const k = await deriveKeys(h.salt, ikm);
  return equal(await commitment(k.commit, h), h.commitment);
}

// --- chunks (STREAM, PROTOCOL.md 2.4) ---

function chunkNonce(i: number, last: boolean): Uint8Array<ArrayBuffer> {
  const n = new Uint8Array(12);
  for (let j = 10, x = i; j >= 3 && x > 0; j--, x = Math.floor(x / 256)) n[j] = x % 256;
  n[11] = last ? 1 : 0;
  return n;
}

async function seal(key: CryptoKey, nonce: Uint8Array<ArrayBuffer>, plain: Uint8Array, aad: Uint8Array<ArrayBuffer>): Promise<Uint8Array<ArrayBuffer>> {
  return new Uint8Array(await subtle.encrypt({ name: "AES-GCM", iv: nonce, additionalData: aad, tagLength: 128 }, key, own(plain)));
}

async function open(key: CryptoKey, nonce: Uint8Array<ArrayBuffer>, ct: Uint8Array, aad: Uint8Array<ArrayBuffer>): Promise<Uint8Array<ArrayBuffer> | null> {
  try {
    return new Uint8Array(await subtle.decrypt({ name: "AES-GCM", iv: nonce, additionalData: aad, tagLength: 128 }, key, own(ct)));
  } catch {
    return null;
  }
}

// --- metadata (PROTOCOL.md 2.5) ---

// U+2028 and U+2029, written as \u escapes the way Go's encoder does, so both sides make the same bytes.
const LINE_SEP = String.fromCharCode(0x2028);
const PARA_SEP = String.fromCharCode(0x2029);

/** The canonical JSON form: fixed field order, no spaces, "type" left out when empty. */
export function encodeMetadata(m: Metadata): string {
  const files = m.files.map((f) => (f.type ? { name: f.name, size: f.size, type: f.type } : { name: f.name, size: f.size }));
  return JSON.stringify({ v: 1, files, zip: !!m.zip })
    .split(LINE_SEP).join("\\" + "u2028")
    .split(PARA_SEP).join("\\" + "u2029");
}

function pad(b: Uint8Array): Uint8Array<ArrayBuffer> {
  const out = new Uint8Array(Math.ceil((b.length + 1) / META_BLOCK) * META_BLOCK);
  out.set(b);
  out[b.length] = 0x80;
  return out;
}

function decodeMetadata(padded: Uint8Array): Metadata {
  let end = padded.length - 1;
  while (end >= 0 && padded[end] === 0) end--;
  if (end < 0 || padded[end] !== 0x80) throw new ZarfError("auth", "bad metadata padding");
  let m: unknown;
  try {
    m = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(padded.subarray(0, end)));
  } catch {
    throw new ZarfError("header", "unreadable metadata");
  }
  const meta = m as { v?: unknown; files?: unknown; zip?: unknown };
  if (meta.v !== 1 || !Array.isArray(meta.files) || meta.files.length === 0) throw new ZarfError("header", "unreadable metadata");
  const files: FileInfo[] = meta.files.map((f: unknown) => {
    const x = f as { name?: unknown; size?: unknown; type?: unknown };
    if (typeof x.name !== "string" || typeof x.size !== "number" || !Number.isSafeInteger(x.size) || x.size < 0) {
      throw new ZarfError("header", "unreadable metadata");
    }
    return { name: x.name, size: x.size, type: typeof x.type === "string" ? x.type : undefined };
  });
  return { files, zip: meta.zip === true };
}

// --- writing ---

export interface WriterParams {
  mode: Mode;
  chunkLog2?: number;
  salt?: Uint8Array; // random when left out (tests fix it)
  argon?: ArgonParams;
  ephPub?: Uint8Array;
}

/**
 * Seals content chunk by chunk, so a large file never has to be in memory at once. Send `head`
 * first, then every chunk in order: full-size chunks with last = false, then exactly one last chunk.
 */
export class EnvelopeWriter {
  readonly head: Uint8Array<ArrayBuffer>;
  readonly chunkSize: number;
  private readonly key: CryptoKey;
  private readonly aad: Uint8Array<ArrayBuffer>;
  private n = 0;
  private done = false;

  private constructor(head: Uint8Array<ArrayBuffer>, chunkSize: number, key: CryptoKey, aad: Uint8Array<ArrayBuffer>) {
    this.head = head;
    this.chunkSize = chunkSize;
    this.key = key;
    this.aad = aad;
  }

  static async create(p: WriterParams, ikm: Uint8Array, meta: Metadata): Promise<EnvelopeWriter> {
    const plainMeta = pad(utf8(encodeMetadata(meta)));
    const h: Header = {
      mode: p.mode,
      chunkLog2: p.chunkLog2 ?? DEFAULT_CHUNK_LOG2,
      salt: p.salt ?? crypto.getRandomValues(new Uint8Array(32)),
      argon: p.argon,
      ephPub: p.ephPub,
      metaLen: plainMeta.length + TAG,
      commitment: new Uint8Array(),
    };
    check(h);
    const k = await deriveKeys(h.salt, ikm);
    h.commitment = await commitment(k.commit, h);
    const headerBytes = marshal(h, true);
    const aad = await sha256(headerBytes);
    const metaCt = await seal(k.meta, new Uint8Array(12), plainMeta, aad); // the metadata key is used once
    return new EnvelopeWriter(concat(headerBytes, metaCt), 1 << h.chunkLog2, k.content, aad);
  }

  async seal(chunk: Uint8Array, last: boolean): Promise<Uint8Array<ArrayBuffer>> {
    if (this.done) throw new Error("envelope: write after the last chunk");
    if (chunk.length > this.chunkSize || (!last && chunk.length !== this.chunkSize)) {
      throw new Error("envelope: only the last chunk may be shorter than the chunk size");
    }
    const ct = await seal(this.key, chunkNonce(this.n++, last), chunk, this.aad);
    this.done = last;
    return ct;
  }
}

/** The exact envelope size for a content size, known before encrypting (the server needs it up front). */
export function envelopeSize(headLength: number, contentSize: number, chunkLog2 = DEFAULT_CHUNK_LOG2): number {
  const chunks = Math.max(1, Math.ceil(contentSize / (1 << chunkLog2)));
  return headLength + contentSize + chunks * TAG;
}

/** Seals a whole small content in memory (tests, tiny files). */
export async function sealAll(p: WriterParams, ikm: Uint8Array, meta: Metadata, content: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  const w = await EnvelopeWriter.create(p, ikm, meta);
  const parts: Uint8Array[] = [w.head];
  for (let at = 0; ; at += w.chunkSize) {
    const end = Math.min(at + w.chunkSize, content.length);
    const last = end >= content.length;
    parts.push(await w.seal(content.subarray(at, end), last));
    if (last) break;
  }
  return concat(...parts);
}

// --- reading ---

export type KeyFunc = (h: Header) => Promise<Uint8Array>;

/**
 * Opens an envelope as its bytes arrive. push() returns plaintext only from chunks that
 * verified; finish() checks the end. A chunk is the last one only if nothing follows it.
 */
export class EnvelopeReader {
  header?: Header;
  metadata?: Metadata;
  private buf: Uint8Array = new Uint8Array();
  private state: "header" | "meta" | "body" | "done" = "header";
  private keys?: Keys;
  private aad?: Uint8Array<ArrayBuffer>;
  private n = 0;
  private headerLength = 0;
  private readonly key: KeyFunc;

  constructor(key: KeyFunc) {
    this.key = key;
  }

  private get full(): number {
    return (1 << this.header!.chunkLog2) + TAG;
  }

  async push(bytes: Uint8Array): Promise<Uint8Array[]> {
    if (this.state === "done") throw new ZarfError("trailing", "unexpected data after the end");
    this.buf = concat(this.buf, bytes);
    const out: Uint8Array[] = [];
    if (this.state === "header") {
      const parsed = parseHeader(this.buf);
      if (!parsed) return out;
      this.header = parsed.header;
      this.headerLength = parsed.length;
      const ikm = await this.key(parsed.header);
      const k = await deriveKeys(parsed.header.salt, ikm);
      if (!equal(await commitment(k.commit, parsed.header), parsed.header.commitment)) throw new ZarfError("wrong_key", "wrong key or password");
      this.keys = k;
      this.aad = await sha256(this.buf.subarray(0, parsed.length));
      this.buf = this.buf.slice(parsed.length);
      this.state = "meta";
    }
    if (this.state === "meta") {
      const len = this.header!.metaLen;
      if (this.buf.length < len) return out;
      const plain = await open(this.keys!.meta, new Uint8Array(12), this.buf.subarray(0, len), this.aad!);
      if (!plain) throw new ZarfError("auth", "metadata was changed");
      this.metadata = decodeMetadata(plain);
      this.buf = this.buf.slice(len);
      this.state = "body";
    }
    // A full chunk is opened as a middle one only once more data follows it.
    while (this.buf.length > this.full) {
      const ct = this.buf.subarray(0, this.full);
      const plain = await open(this.keys!.content, chunkNonce(this.n, false), ct, this.aad!);
      if (!plain) {
        // It opens as the last chunk: bytes were added after the real end.
        if (await open(this.keys!.content, chunkNonce(this.n, true), ct, this.aad!)) throw new ZarfError("trailing", "unexpected data after the end");
        throw new ZarfError("auth", "content was changed or damaged");
      }
      out.push(plain);
      this.n++;
      this.buf = this.buf.slice(this.full);
    }
    return out;
  }

  async finish(): Promise<Uint8Array[]> {
    if (this.state === "done") return [];
    if (this.state === "header") throw new ZarfError("header", "not a Zarf envelope, or cut short");
    if (this.state === "meta") throw new ZarfError("truncated", "content is cut short");
    const ct = this.buf;
    if (ct.length < TAG) throw new ZarfError("truncated", "content is cut short");
    const plain = await open(this.keys!.content, chunkNonce(this.n, true), ct, this.aad!);
    if (!plain) {
      if (await open(this.keys!.content, chunkNonce(this.n, false), ct, this.aad!)) throw new ZarfError("truncated", "content is cut short");
      throw new ZarfError("auth", "content was changed or damaged");
    }
    this.state = "done";
    this.buf = new Uint8Array();
    return [plain];
  }

  /** How many bytes of header and metadata came before the content (for progress bars). */
  get prefixLength(): number {
    return this.headerLength + (this.header?.metaLen ?? 0);
  }
}

/** Opens a whole envelope held in memory (tests, small files). */
export async function openAll(env: Uint8Array, key: KeyFunc): Promise<{ metadata: Metadata; content: Uint8Array }> {
  const r = new EnvelopeReader(key);
  const parts = [...(await r.push(env)), ...(await r.finish())];
  return { metadata: r.metadata!, content: concat(...parts) };
}
