// The TypeScript envelope against the shared vectors (PROTOCOL.md section 9): it must rebuild
// every "accept" envelope the Go code made, byte for byte, and refuse every "reject" one.

import { test } from "node:test";
import assert from "node:assert/strict";
import { hkdfSync } from "node:crypto";
import { readFileSync } from "node:fs";
import { concat, fromB64u, fromHex, toB64u, toHex } from "../src/lib/bytes.ts";
import { EnvelopeReader, envelopeSize, keyMatches, Mode, openAll, parseHeader, sealAll, ZarfError, type KeyFunc, type Metadata, type WriterParams } from "../src/lib/envelope.ts";
import { downloadAuth, importX25519, linkKey, passwordKey, requestKey } from "../src/lib/keys.ts";

interface AcceptVector {
  name: string;
  mode: number;
  chunk_log2: number;
  salt: string;
  file_key?: string;
  password?: string;
  argon?: { m_kib: number; t: number; p: number; salt: string };
  eph_priv?: string;
  req_priv?: string;
  metadata: { files: { name: string; size: number; type?: string }[]; zip: boolean };
  content: string;
  envelope: string;
}

interface RejectVector {
  name: string;
  base: string;
  file_key?: string;
  envelope: string;
  error: string;
}

const vectors = JSON.parse(readFileSync(new URL("../../testdata/vectors/envelope.json", import.meta.url), "utf8")) as { accept: AcceptVector[]; reject: RejectVector[] };

async function inputs(v: AcceptVector): Promise<{ params: WriterParams; ikm: Uint8Array; key: KeyFunc }> {
  const params: WriterParams = { mode: v.mode as WriterParams["mode"], chunkLog2: v.chunk_log2, salt: fromHex(v.salt) };
  if (v.mode === Mode.Link) {
    const k = fromHex(v.file_key!);
    return { params, ikm: linkKey(k), key: async () => linkKey(k) };
  }
  if (v.mode === Mode.LinkPassword) {
    const a = v.argon!;
    params.argon = { memoryKiB: a.m_kib, time: a.t, threads: a.p, salt: fromHex(a.salt) };
    const k = fromHex(v.file_key!);
    return { params, ikm: await passwordKey(k, v.password!, params.argon), key: (h) => passwordKey(k, v.password!, h.argon!) };
  }
  const eph = await importX25519(fromHex(v.eph_priv!));
  const req = await importX25519(fromHex(v.req_priv!));
  params.ephPub = eph.publicKey;
  return {
    params,
    ikm: await requestKey(eph.privateKey, req.publicKey, eph.publicKey, req.publicKey),
    key: (h) => requestKey(req.privateKey, h.ephPub!, h.ephPub!, req.publicKey),
  };
}

const metadataOf = (v: AcceptVector): Metadata => ({ files: v.metadata.files, zip: v.metadata.zip });

for (const v of vectors.accept) {
  test(`writes the same bytes as Go: ${v.name}`, async () => {
    const { params, ikm } = await inputs(v);
    const env = await sealAll(params, ikm, metadataOf(v), fromHex(v.content));
    assert.equal(toHex(env), v.envelope);
  });

  test(`opens what Go wrote: ${v.name}`, async () => {
    const { key } = await inputs(v);
    const { metadata, content } = await openAll(fromHex(v.envelope), key);
    assert.equal(toHex(content), v.content);
    assert.equal(metadata.files[0]!.name, v.metadata.files[0]!.name);
  });
}

for (const v of vectors.reject) {
  test(`refuses: ${v.name}`, async () => {
    const base = { ...vectors.accept.find((a) => a.name === v.base)! };
    if (v.file_key) base.file_key = v.file_key;
    const { key } = await inputs(base);
    await assert.rejects(openAll(fromHex(v.envelope), key), (err: unknown) => err instanceof ZarfError && err.code === v.error);
  });
}

test("reads the same however the bytes arrive", async () => {
  const v = vectors.accept.find((a) => a.name === "link, three chunks")!;
  const { key } = await inputs(v);
  const env = fromHex(v.envelope);
  for (const step of [1, 7, 1000, 4112, 4113, 99999]) {
    const r = new EnvelopeReader(key);
    const parts: Uint8Array[] = [];
    for (let at = 0; at < env.length; at += step) parts.push(...(await r.push(env.subarray(at, at + step))));
    parts.push(...(await r.finish()));
    assert.equal(toHex(concat(...parts)), v.content, `step ${step}`);
  }
});

test("knows the envelope size before encrypting", async () => {
  for (const v of vectors.accept) {
    const { header, length } = parseHeader(fromHex(v.envelope))!;
    const size = envelopeSize(length + header.metaLen, fromHex(v.content).length, v.chunk_log2);
    assert.equal(size, fromHex(v.envelope).length, v.name);
  }
});

test("checks a password against the header without decrypting", async () => {
  const v = vectors.accept.find((a) => a.mode === Mode.LinkPassword)!;
  const { header } = parseHeader(fromHex(v.envelope))!;
  const k = fromHex(v.file_key!);
  assert.equal(await keyMatches(header, await passwordKey(k, v.password!, header.argon!)), true);
  assert.equal(await keyMatches(header, await passwordKey(k, "yanlış", header.argon!)), false);
});

test("the download value is HKDF with its own label, checked against Node's own HKDF", async () => {
  const k = fromHex("06".repeat(32));
  const a = await downloadAuth(k);
  const expected = new Uint8Array(hkdfSync("sha256", k, new Uint8Array(), "zarf v1 download auth", 32));
  assert.equal(toHex(a), toHex(expected));
  assert.notEqual(toHex(a), toHex(k));
});

test("base64url round trip and strictness", () => {
  for (const n of [0, 1, 2, 3, 16, 32]) {
    const b = Uint8Array.from({ length: n }, (_, i) => i * 37);
    assert.equal(toHex(fromB64u(toB64u(b))!), toHex(b));
  }
  assert.equal(fromB64u("ab+c"), null);
  assert.equal(fromB64u("a"), null);
  assert.equal(toB64u(fromHex("fbff")), "-_8");
});
