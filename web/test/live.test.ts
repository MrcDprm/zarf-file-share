import { test } from "node:test";
import assert from "node:assert/strict";
import { LiveError, Pipe, choosePath, handshake, receiveFile, sendFile, type SocketLike } from "../src/lib/live.ts";
import { concat } from "../src/lib/bytes.ts";
import type { Code } from "../src/lib/words.ts";

/** A relay like the server's: it pairs the two joins, sends both a session id, then forwards. */
class Relay {
  sockets: FakeSocket[] = [];
  joined = 0;
  tamper?: (bytes: Uint8Array, n: number) => Uint8Array;
  private binaries = 0;

  socket(): FakeSocket {
    const s = new FakeSocket(this);
    this.sockets.push(s);
    return s;
  }

  receive(from: FakeSocket, data: string | Uint8Array): void {
    if (typeof data === "string" && JSON.parse(data).t === "join") {
      if (++this.joined === 2) {
        const start = JSON.stringify({ t: "start", sid: "AAECAwQFBgcICQoLDA0ODw" });
        for (const s of this.sockets) s.deliver(start);
      }
      return;
    }
    const to = this.sockets.find((s) => s !== from)!;
    if (typeof data !== "string" && this.tamper) data = this.tamper(data, this.binaries++);
    to.deliver(typeof data === "string" ? data : data.slice().buffer);
  }

  closeAll(from: FakeSocket): void {
    for (const s of this.sockets) if (s !== from) s.ended(4410);
  }
}

class FakeSocket implements SocketLike {
  binaryType = "blob";
  bufferedAmount = 0;
  onmessage: ((e: { data: unknown }) => void) | null = null;
  onclose: ((e: { code?: number }) => void) | null = null;
  private open = true;
  private readonly relay: Relay;
  constructor(relay: Relay) {
    this.relay = relay;
  }
  send(data: string | Uint8Array): void {
    if (this.open) queueMicrotask(() => this.relay.receive(this, data));
  }
  close(): void {
    if (!this.open) return;
    this.open = false;
    queueMicrotask(() => this.relay.closeAll(this));
  }
  deliver(data: unknown): void {
    if (this.open) queueMicrotask(() => this.onmessage?.({ data }));
  }
  ended(code: number): void {
    if (!this.open) return;
    this.open = false;
    queueMicrotask(() => this.onclose?.({ code }));
  }
}

const CODE: Code = { channel: 7, positions: [0, 138] };
const content = Uint8Array.from({ length: 200_000 }, (_, i) => (i * 31) & 0xff);
const file = new File([content], "rapor ğüş.pdf", { type: "application/pdf" });

async function run(relay: Relay, senderCode: Code, receiverCode: Code, accept = true) {
  const a = new Pipe(relay.socket());
  const b = new Pipe(relay.socket());
  const sending = (async () => {
    try {
      const session = await handshake(a, "send", senderCode, "token");
      const path = await choosePath(a, session, null); // no WebRTC in Node: the relay path
      assert.equal(path.direct, false);
      await sendFile(path.pipe, session.ikm, file, () => {}, () => {});
      return "sent";
    } catch (err) {
      a.close();
      return (err as LiveError).problem;
    }
  })();
  const receiving = (async () => {
    try {
      const session = await handshake(b, "receive", receiverCode);
      const path = await choosePath(b, session, null);
      return await receiveFile(path.pipe, session.ikm, async () => accept, () => {});
    } catch (err) {
      b.close();
      return (err as LiveError).problem;
    }
  })();
  return Promise.all([sending, receiving]);
}

test("the same code moves the file", async () => {
  const [sent, got] = await run(new Relay(), CODE, { channel: 7, positions: [0, 138] });
  assert.equal(sent, "sent");
  assert.ok(got && typeof got === "object");
  assert.equal(got.file.name, "rapor ğüş.pdf");
  assert.deepEqual(concat(...got.parts), content);
});

test("a wrong code is caught on both sides before any file byte", async () => {
  let binaries = 0;
  const relay = new Relay();
  relay.tamper = (b) => (binaries++, b);
  const [sent, got] = await run(relay, CODE, { channel: 7, positions: [0, 139] });
  assert.equal(sent, "wrong_code");
  assert.equal(got, "wrong_code");
  assert.equal(binaries, 0);
});

test("the receiver can say no", async () => {
  const [sent, got] = await run(new Relay(), CODE, CODE, false);
  assert.equal(sent, "declined");
  assert.equal(got, null);
});

test("a changed byte on the way is refused", async () => {
  const relay = new Relay();
  relay.tamper = (b, n) => {
    if (n === 2) b[10]! ^= 1;
    return b;
  };
  const [, got] = await run(relay, CODE, CODE);
  assert.equal(got, "damaged");
});
