// Live mode (PROTOCOL.md section 5.2): two people meet on a numbered channel, prove to each
// other that they hold the same short code (CPace and key confirmation), and the file goes
// across as an envelope whose key comes from that exchange. The relay sees only ciphertext.

import { hmac } from "@noble/hashes/hmac.js";
import { sha256 } from "@noble/hashes/sha2.js";
import { concat, equal, fromB64u, toB64u, utf8 } from "./bytes.ts";
import { CPaceError, isk, lvCat, share } from "./cpace.ts";
import { EnvelopeReader, EnvelopeWriter, Mode, ZarfError, hkdf, type FileInfo } from "./envelope.ts";
import type { Code } from "./words.ts";

/** What went wrong, in terms the page can explain. */
export type LiveProblem =
  | "unknown_channel" // no such code, or it was already used
  | "taken" // someone else is already on this channel
  | "peer_left" // the other side closed the page or lost the connection
  | "limit" // too long or too large for the relay
  | "expired" // nobody joined in time
  | "wrong_code" // the codes did not match: the channel is closed for good
  | "declined" // the receiver said no
  | "damaged" // the envelope did not verify
  | "network";

export class LiveError extends Error {
  readonly problem: LiveProblem;
  constructor(problem: LiveProblem) {
    super(problem);
    this.name = "LiveError";
    this.problem = problem;
  }
}

const closeProblems: Record<number, LiveProblem> = { 4404: "unknown_channel", 4409: "taken", 4400: "unknown_channel", 4410: "peer_left", 4413: "limit", 4408: "expired" };

/** The part of a browser WebSocket this module uses, so tests can pass a stand-in. */
export interface SocketLike {
  binaryType: string;
  readonly bufferedAmount: number;
  send(data: string | Uint8Array<ArrayBuffer>): void;
  close(code?: number): void;
  onmessage: ((e: { data: unknown }) => void) | null;
  onclose: ((e: { code: number }) => void) | null;
}

type Message = { text: Record<string, unknown> } | { bytes: Uint8Array } | { closed: number };

/** Turns socket events into a queue that the protocol reads one message at a time. */
export class Pipe {
  private readonly socket: SocketLike;
  private readonly queue: Message[] = [];
  private waiting: ((m: Message) => void) | null = null;
  private closed = false;

  constructor(socket: SocketLike) {
    this.socket = socket;
    socket.binaryType = "arraybuffer";
    socket.onmessage = (e) => {
      let m: Message;
      if (typeof e.data === "string") {
        try {
          m = { text: JSON.parse(e.data) as Record<string, unknown> };
        } catch {
          return;
        }
      } else m = { bytes: new Uint8Array(e.data as ArrayBuffer) };
      this.deliver(m);
    };
    socket.onclose = (e) => {
      this.closed = true;
      this.deliver({ closed: e.code });
    };
  }

  private deliver(m: Message): void {
    if (this.waiting) {
      const w = this.waiting;
      this.waiting = null;
      w(m);
    } else this.queue.push(m);
  }

  next(): Promise<Message> {
    const m = this.queue.shift();
    if (m) return Promise.resolve(m);
    if (this.closed) return Promise.resolve({ closed: 1006 });
    return new Promise((resolve) => (this.waiting = resolve));
  }

  /** The next message, which must be a JSON message of one of the given types. */
  async expect(...types: string[]): Promise<Record<string, unknown>> {
    const m = await this.next();
    if ("closed" in m) throw new LiveError(closeProblems[m.closed] ?? "network");
    if (!("text" in m) || !types.includes(m.text.t as string)) throw new LiveError("damaged");
    return m.text;
  }

  async expectBytes(): Promise<Uint8Array> {
    const m = await this.next();
    if ("closed" in m) throw new LiveError(closeProblems[m.closed] ?? "network");
    if (!("bytes" in m)) throw new LiveError("damaged");
    return m.bytes;
  }

  send(m: Record<string, unknown> | Uint8Array<ArrayBuffer>): void {
    this.socket.send(m instanceof Uint8Array ? m : JSON.stringify(m));
  }

  /** Waits while the browser still holds a lot of unsent data: the relay sets the pace. */
  async drain(limit = 4 * 1024 * 1024): Promise<void> {
    while (this.socket.bufferedAmount > limit) {
      if (this.closed) throw new LiveError("peer_left");
      await new Promise((r) => setTimeout(r, 20));
    }
  }

  close(): void {
    this.socket.close(1000);
  }
}

type Role = "send" | "receive";

const u16 = (n: number) => Uint8Array.of(n >> 8, n & 0xff);

/**
 * Joins the channel and runs CPace and key confirmation. Returns the input key of the
 * envelope; a wrong code ends in LiveError("wrong_code") on both sides.
 */
export async function handshake(pipe: Pipe, role: Role, code: Code, token?: string): Promise<Uint8Array> {
  pipe.send(role === "send" ? { t: "join", role, token } : { t: "join", role });
  const start = await pipe.expect("start");
  const sid = fromB64u(String(start.sid));
  if (!sid || sid.length !== 16) throw new LiveError("damaged");

  const prs = concat(utf8("zarf v1 live"), u16(code.positions[0]), u16(code.positions[1]));
  const ci = lvCat(utf8("zarf v1 live"), utf8(String(code.channel)));
  const me = share(prs, ci, sid);
  pipe.send({ t: "pake", y: toB64u(me.Y) });
  const peerY = fromB64u(String((await pipe.expect("pake")).y));
  if (!peerY || peerY.length !== 32) throw new LiveError("wrong_code");
  const [ya, yb] = role === "send" ? [me.Y, peerY] : [peerY, me.Y];

  let key: Uint8Array;
  try {
    key = isk(me, peerY, sid, ya, utf8("send"), yb, utf8("receive"));
  } catch (err) {
    if (err instanceof CPaceError) throw new LiveError("wrong_code");
    throw err;
  }
  const confirmKey = await hkdf(key, sid, "zarf v1 confirm");
  const mac = (who: Role) => hmac(sha256, confirmKey, concat(utf8(who), ya, yb));
  pipe.send({ t: "confirm", mac: toB64u(mac(role)) });
  const theirs = fromB64u(String((await pipe.expect("confirm")).mac));
  if (!theirs || !equal(theirs, mac(role === "send" ? "receive" : "send"))) throw new LiveError("wrong_code");
  return hkdf(key, sid, "zarf v1 live content");
}

export interface Progress {
  (done: number, total: number): void;
}

/** Offers the file, waits for the receiver's answer, then sends it chunk by chunk. */
export async function sendFile(pipe: Pipe, ikm: Uint8Array, file: File, onAnswerWait: () => void, onProgress: Progress): Promise<void> {
  const writer = await EnvelopeWriter.create({ mode: Mode.Live }, ikm, { files: [{ name: file.name, size: file.size, type: file.type || undefined }] });
  pipe.send(writer.head);
  onAnswerWait();
  const answer = await pipe.expect("accept", "decline");
  if (answer.t === "decline") throw new LiveError("declined");
  for (let at = 0; ; at += writer.chunkSize) {
    const end = Math.min(at + writer.chunkSize, file.size);
    const plain = new Uint8Array(await file.slice(at, end).arrayBuffer());
    const last = end >= file.size;
    pipe.send(await writer.seal(plain, last));
    await pipe.drain();
    onProgress(end, file.size);
    if (last) break;
  }
  pipe.send({ t: "done" });
  await pipe.expect("received");
}

/**
 * Receives an offer, asks the person through decide(), and returns the verified plaintext,
 * or null when they declined. Nothing is returned before the last chunk verified.
 */
export async function receiveFile(pipe: Pipe, ikm: Uint8Array, decide: (f: FileInfo) => Promise<boolean>, onProgress: Progress): Promise<{ file: FileInfo; parts: Uint8Array[] } | null> {
  const reader = new EnvelopeReader(async (h) => {
    if (h.mode !== Mode.Live) throw new ZarfError("header", "not a live envelope");
    return ikm;
  });
  const parts: Uint8Array[] = [];
  try {
    parts.push(...(await reader.push(await pipe.expectBytes())));
    const file = reader.metadata?.files[0];
    if (!file) throw new LiveError("damaged");
    if (!(await decide(file))) {
      pipe.send({ t: "decline" });
      return null;
    }
    pipe.send({ t: "accept" });
    let done = 0;
    for (;;) {
      const m = await pipe.next();
      if ("closed" in m) throw new LiveError(closeProblems[m.closed] ?? "network");
      if ("text" in m) {
        if (m.text.t !== "done") throw new LiveError("damaged");
        break;
      }
      for (const p of await reader.push(m.bytes)) {
        parts.push(p);
        done += p.length;
      }
      onProgress(done, file.size);
    }
    parts.push(...(await reader.finish()));
    pipe.send({ t: "received" });
    return { file, parts };
  } catch (err) {
    if (err instanceof ZarfError) throw new LiveError("damaged");
    throw err;
  }
}
