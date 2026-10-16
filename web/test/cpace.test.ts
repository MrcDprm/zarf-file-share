import { test } from "node:test";
import assert from "node:assert/strict";
import { fromHex, toHex, utf8 } from "../src/lib/bytes.ts";
import { CPaceError, generatorString, calculateGenerator, isk, lvCat, prependLen, scalarFromLE, scalarMultVfy, share } from "../src/lib/cpace.ts";

// draft-irtf-cfrg-cpace-20, appendices A.1 and B.3 (ristretto255, SHA-512).
const PRS = utf8("Password");
const CI = fromHex("0b415f696e69746961746f720b425f726573706f6e646572");
const SID = fromHex("7e4b4791d6a8ef019b936c79fb7f2c57");
const YA_SCALAR = scalarFromLE(fromHex("da3d23700a9e5699258aef94dc060dfda5ebb61f02a5ea77fad53f4ff0976d08"));
const YB_SCALAR = scalarFromLE(fromHex("d2316b454718c35362d83d69df6320f38578ed5984651435e2949762d900b80d"));
const YA = "d6bac480f2c386c394efc7c47adb9925dcd2630b64f240c50f8d0eec482b9157";
const YB = "3ea7e0b19560d7c0b0f5734f63b955286dfa8232b5ebe63324e2d9e7433f7258";
const ISK_IR =
  "b69effbf61b51d56401c0f65601abe428de8206feaaf0e32198896dcae7b35cd2b38950a39dfd5d4a79164614c2984f7daa460b588c1e80c3fa2068af7900447";

test("string helpers", () => {
  assert.equal(toHex(prependLen(new Uint8Array())), "00");
  assert.equal(toHex(prependLen(utf8("1234"))), "0431323334");
  assert.equal(toHex(prependLen(Uint8Array.from({ length: 128 }, (_, i) => i))).slice(0, 6), "800100");
  assert.equal(toHex(lvCat(utf8("1234"), utf8("5"), new Uint8Array(), utf8("678"))), "043132333401350003363738");
});

test("generator", () => {
  assert.equal(generatorString(PRS, CI, SID).length, 170);
  assert.equal(toHex(calculateGenerator(PRS, CI, SID).toBytes()), "222b6b195fe84b1652badb6f6a3ae3d24341e7306967f0b8115b40d5698c7e56");
});

test("messages, shared point and ISK", () => {
  const a = share(PRS, CI, SID, YA_SCALAR);
  const b = share(PRS, CI, SID, YB_SCALAR);
  assert.equal(toHex(a.Y), YA);
  assert.equal(toHex(b.Y), YB);
  assert.equal(toHex(scalarMultVfy(a.y, b.Y)), "80b69a8a76457ab6a4d7f887a4bf6b55a2f80ac19c333f917a05fc9887c8b40f");
  const ada = utf8("ADa");
  const adb = utf8("ADb");
  assert.equal(toHex(isk(a, b.Y, SID, a.Y, ada, b.Y, adb)), ISK_IR);
  assert.equal(toHex(isk(b, a.Y, SID, a.Y, ada, b.Y, adb)), ISK_IR);
});

test("invalid points abort", () => {
  const s = scalarFromLE(fromHex("7cd0e075fa7955ba52c02759a6c90dbbfc10e6d40aea8d283e407d88cf538a05"));
  assert.equal(
    toHex(scalarMultVfy(s, fromHex("2c3c6b8c4f3800e7aef6864025b4ed79bd599117e427c41bd47d93d654b4a51c"))),
    "7c13645fe790a468f62c39beb7388e541d8405d1ade69d1778c5fe3e7f6b600e",
  );
  assert.throws(() => scalarMultVfy(s, fromHex("2b3c6b8c4f3800e7aef6864025b4ed79bd599117e427c41bd47d93d654b4a51c")), CPaceError);
  assert.throws(() => scalarMultVfy(s, new Uint8Array(32)), CPaceError);
});

test("different codes give different keys", () => {
  const a = share(utf8("one"), CI, SID);
  const b = share(utf8("two"), CI, SID);
  const ka = isk(a, b.Y, SID, a.Y, utf8("send"), b.Y, utf8("receive"));
  const kb = isk(b, a.Y, SID, a.Y, utf8("send"), b.Y, utf8("receive"));
  assert.notEqual(toHex(ka), toHex(kb));
});
