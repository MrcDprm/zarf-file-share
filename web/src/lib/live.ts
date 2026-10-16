// Live mode (PROTOCOL.md section 5.2): two people meet on a numbered channel, prove to each
// other that they hold the same short code (CPace and key confirmation), try to connect
// directly (WebRTC), and the file goes across as an envelope whose key comes from that
// exchange. Whichever path it takes, only ciphertext travels.

import { hmac } from "@noble/hashes/hmac.js";
import { sha256 } from "@noble/hashes/sha2.js";
import { concat, equal, fromB64u, own, toB64u, utf8 } from "./bytes.ts";
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

/** The part of a WebSocket or an RTCDataChannel this module uses, so tests can pass a stand-in. */
export interface SocketLike {
  binaryType: string;
  readonly bufferedAmount: number;
  send(data: string | Uint8Array<ArrayBuffer>): void;
  close(code?: number): void;
  onmessage: ((e: { data: unknown }) => void) | null;
  onclose: ((e: { code?: number }) => void) | null;
}

type Text = Record<string, unknown>;
type Message = { text: Text } | { bytes: Uint8Array } | { closed: number };

/** Turns socket events into a queue that the protocol reads one message at a time. */
export class Pipe {
  private readonly socket: SocketLike;
  private readonly queue: Message[] = [];
  private waiting: ((m: Message) => void) | null = null;
  private closed = false;
  private readonly handlers = new Map<string, (m: Text) => void>();
  private readonly highWater: number;

  /** highWater: how much unsent data the sender lets pile up before it waits. */
  constructor(socket: SocketLike, highWater = 4 * 1024 * 1024) {
    this.socket = socket;
    this.highWater = highWater;
    socket.binaryType = "arraybuffer";
    socket.onmessage = (e) => {
      let m: Message;
      if (typeof e.data === "string") {
        try {
          m = { text: JSON.parse(e.data) as Text };
        } catch {
          return;
        }
      } else m = { bytes: new Uint8Array(e.data as ArrayBuffer) };
      this.deliver(m);
    };
    socket.onclose = (e) => {
      this.closed = true;
      this.deliver({ closed: e.code ?? 4410 }); // a data channel closes without a code: the peer left
    };
  }

  private deliver(m: Message): void {
    if ("text" in m) {
      const h = this.handlers.get(String(m.text.t));
      if (h) return h(m.text);
    }
    if (this.waiting) {
      const w = this.waiting;
      this.waiting = null;
      w(m);
    } else this.queue.push(m);
  }

  /**
   * Hands every message of one type to a handler instead of the queue, including any that
   * already waited there: signalling messages arrive whenever the network finds a route.
   */
  handle(type: string, h: ((m: Text) => void) | null): void {
    if (!h) return void this.handlers.delete(type);
    this.handlers.set(type, h);
    for (let i = 0; i < this.queue.length; ) {
      const m = this.queue[i]!;
      if ("text" in m && m.text.t === type) {
        this.queue.splice(i, 1);
        h(m.text);
      } else i++;
    }
  }

  next(): Promise<Message> {
    const m = this.queue.shift();
    if (m) return Promise.resolve(m);
    if (this.closed) return Promise.resolve({ closed: 1006 });
    return new Promise((resolve) => (this.waiting = resolve));
  }

  /** The next message, which must be a JSON message of one of the given types. */
  async expect(...types: string[]): Promise<Text> {
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

  send(m: Text | Uint8Array<ArrayBuffer>): void {
    if (this.closed) throw new LiveError("peer_left");
    this.socket.send(m instanceof Uint8Array ? m : JSON.stringify(m));
  }

  /** Waits while a lot of data is still unsent: the slower side sets the pace. */
  async drain(): Promise<void> {
    while (this.socket.bufferedAmount > this.highWater) {
      if (this.closed) throw new LiveError("peer_left");
      await new Promise((r) => setTimeout(r, 15));
    }
  }

  close(): void {
    try {
      this.socket.close(1000);
    } catch {
      /* already closed */
    }
  }
}

type Role = "send" | "receive";

export interface Session {
  role: Role;
  ikm: Uint8Array; // the envelope's input key
  signalKey: Uint8Array; // encrypts the WebRTC setup messages
}

const u16 = (n: number) => Uint8Array.of(n >> 8, n & 0xff);

/**
 * Joins the channel and runs CPace and key confirmation. A wrong code ends in
 * LiveError("wrong_code") on both sides, before anything else is said.
 */
export async function handshake(pipe: Pipe, role: Role, code: Code, token?: string): Promise<Session> {
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
  return { role, ikm: await hkdf(key, sid, "zarf v1 live content"), signalKey: await hkdf(key, sid, "zarf v1 signal") };
}

// --- the path: a direct WebRTC connection when both can, the relay otherwise ---

const DIRECT_TIMEOUT = 10_000;

/** Makes a peer connection; null where the platform has none. */
export type PeerFactory = (() => RTCPeerConnection) | null;

/** Encrypts and authenticates the WebRTC setup messages, so the relay can neither read nor change them. */
class SignalBox {
  private readonly key: Promise<CryptoKey>;
  private readonly mine: number;
  private sent = 0;
  private received = 0;
  constructor(raw: Uint8Array, role: Role) {
    this.key = crypto.subtle.importKey("raw", own(raw), "AES-GCM", false, ["encrypt", "decrypt"]);
    this.mine = role === "send" ? 1 : 2;
  }
  private static nonce(direction: number, n: number): Uint8Array<ArrayBuffer> {
    const iv = new Uint8Array(12);
    iv[0] = direction;
    new DataView(iv.buffer).setUint32(8, n); // 11-byte counter; 2^32 messages is far beyond need
    return iv;
  }
  async seal(m: unknown): Promise<Text> {
    const ct = await crypto.subtle.encrypt({ name: "AES-GCM", iv: SignalBox.nonce(this.mine, this.sent++) }, await this.key, utf8(JSON.stringify(m)));
    return { t: "signal", c: toB64u(new Uint8Array(ct)) };
  }
  async open(m: Text): Promise<Record<string, unknown>> {
    const ct = fromB64u(String(m.c));
    if (!ct) throw new LiveError("damaged");
    try {
      const plain = await crypto.subtle.decrypt({ name: "AES-GCM", iv: SignalBox.nonce(3 - this.mine, this.received++) }, await this.key, ct);
      return JSON.parse(new TextDecoder().decode(plain)) as Record<string, unknown>;
    } catch {
      throw new LiveError("damaged"); // a changed or replayed message: the relay is not trusted
    }
  }
}

export interface Path {
  pipe: Pipe;
  direct: boolean;
  close(): void;
}

/**
 * Agrees on the path. Both say whether they can connect directly; if both can, the sender
 * tries for up to ten seconds and then announces the outcome. Everything after this travels
 * on the returned pipe.
 */
export async function choosePath(relay: Pipe, s: Session, peer: PeerFactory, timeout = DIRECT_TIMEOUT): Promise<Path> {
  const viaRelay: Path = { pipe: relay, direct: false, close: () => relay.close() };
  relay.send({ t: "hello", direct: peer !== null });
  const theirs = await relay.expect("hello");
  if (!peer || theirs.direct !== true) {
    if (s.role === "send") relay.send({ t: "path", via: "relay" });
    else await expectPath(relay, "relay");
    return viaRelay;
  }

  const box = new SignalBox(s.signalKey, s.role);
  const pc = peer();
  let failed: unknown = null;
  // Setup messages are handled strictly in order: a candidate must not overtake the offer.
  let chain = Promise.resolve();
  relay.handle("signal", (m) => {
    chain = chain.then(async () => {
      const sig = await box.open(m);
      if (sig.sdp) {
        await pc.setRemoteDescription(sig.sdp as RTCSessionDescriptionInit);
        if (s.role === "receive") {
          await pc.setLocalDescription(await pc.createAnswer());
          relay.send(await box.seal({ sdp: pc.localDescription!.toJSON() }));
        }
      } else if (sig.ice) await pc.addIceCandidate(sig.ice as RTCIceCandidateInit);
    }).catch((err) => void (failed = err));
  });
  pc.onicecandidate = async (e) => {
    if (e.candidate) relay.send(await box.seal({ ice: e.candidate.toJSON() }));
  };

  const closeAll = (direct: Pipe | null) => () => {
    direct?.close();
    pc.close();
    relay.close();
  };

  if (s.role === "send") {
    const dc = pc.createDataChannel("zarf", { ordered: true });
    const opened = waitOpen(dc, timeout);
    await pc.setLocalDescription(await pc.createOffer());
    relay.send(await box.seal({ sdp: pc.localDescription!.toJSON() }));
    if ((await opened) && !failed) {
      relay.send({ t: "path", via: "direct" });
      const pipe = new Pipe(dc as unknown as SocketLike, 1024 * 1024);
      return { pipe, direct: true, close: closeAll(pipe) };
    }
    relay.handle("signal", null);
    relay.send({ t: "path", via: "relay" });
    pc.close();
    return viaRelay;
  }

  // The receiver takes the channel as soon as it appears, so no early message is lost.
  let dcPipe: Pipe | null = null;
  const arrived = new Promise<RTCDataChannel>((resolve) => {
    pc.ondatachannel = (e) => {
      dcPipe = new Pipe(e.channel as unknown as SocketLike, 1024 * 1024);
      resolve(e.channel);
    };
  });
  const via = await expectPath(relay);
  relay.handle("signal", null);
  if (via === "relay") {
    pc.close();
    return viaRelay;
  }
  const dc = await Promise.race([arrived, new Promise<null>((r) => setTimeout(() => r(null), timeout))]);
  if (!dc || !(await waitOpen(dc, timeout))) throw new LiveError("network");
  return { pipe: dcPipe!, direct: true, close: closeAll(dcPipe) };
}

async function expectPath(relay: Pipe, want?: string): Promise<string> {
  const m = await relay.expect("path");
  const via = String(m.via);
  if ((via !== "relay" && via !== "direct") || (want && via !== want)) throw new LiveError("damaged");
  return via;
}

function waitOpen(dc: RTCDataChannel, timeout: number): Promise<boolean> {
  if (dc.readyState === "open") return Promise.resolve(true);
  return new Promise((resolve) => {
    const timer = setTimeout(() => resolve(false), timeout);
    dc.addEventListener("open", () => (clearTimeout(timer), resolve(true)), { once: true });
    dc.addEventListener("close", () => (clearTimeout(timer), resolve(false)), { once: true });
  });
}

// --- the transfer ---

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
