import { test } from "node:test";
import assert from "node:assert/strict";
import { WORDS_EN, WORDS_TR, formatCode, parseCode, fingerprint } from "../src/lib/words.ts";

test("two lists of 256 words, typeable and unambiguous", () => {
  assert.equal(WORDS_TR.length, 256);
  assert.equal(WORDS_EN.length, 256);
  assert.equal(new Set(WORDS_TR).size, 256, "a Turkish word repeats");
  assert.equal(new Set(WORDS_EN).size, 256, "an English word repeats");
  for (const w of [...WORDS_TR, ...WORDS_EN]) assert.match(w, /^[a-z]{2,9}$/, w);
  // A word in both lists must mean the same position in both.
  WORDS_TR.forEach((w, i) => {
    const j = WORDS_EN.indexOf(w);
    assert.ok(j === -1 || j === i, `"${w}" is word ${i} in Turkish but ${j} in English`);
  });
});

test("codes read back in either language, or mixed", () => {
  const code = { channel: 7, positions: [0, 138] as [number, number] };
  assert.equal(formatCode(code, "tr"), "7-elma-kule");
  assert.equal(formatCode(code, "en"), "7-apple-tower");
  for (const typed of ["7-elma-kule", "7-apple-tower", "7-elma-tower", " 7 APPLE kule ", "7.elma.kule"]) {
    assert.deepEqual(parseCode(typed), code, typed);
  }
});

test("typos are explained", () => {
  assert.deepEqual(parseCode("7-elma"), { problem: "shape" });
  assert.deepEqual(parseCode("x-elma-kule"), { problem: "channel" });
  assert.deepEqual(parseCode("0-elma-kule"), { problem: "channel" });
  assert.deepEqual(parseCode("7-elma-kulee"), { problem: "word", word: "kulee" });
});

test("a fingerprint is five words and depends on the key", async () => {
  const a = await fingerprint(new Uint8Array(32), "tr");
  const b = await fingerprint(new Uint8Array(32).fill(1), "tr");
  assert.equal(a.split(" ").length, 5);
  assert.notEqual(a, b);
  assert.equal((await fingerprint(new Uint8Array(32), "en")).split(" ").length, 5);
});
