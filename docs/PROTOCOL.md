# Zarf protocol, version 1

**Status:** draft. Code follows this document; when they disagree, the document is fixed first.

Zarf moves files between people so that the server only ever stores and relays bytes it cannot read. This document defines the bytes: the encrypted container, how its keys are made in each of the three modes, and the messages exchanged with the server. Why each choice was made, and what Zarf does *not* protect against, is in [THREAT-MODEL.md](THREAT-MODEL.md).

Zarf invents no cryptographic primitive. Everything below is a composition of standard, well-studied building blocks, and every composition is pinned down by shared test vectors (section 9) that both the Go and the TypeScript implementation must pass.

## 1. Building blocks

| Purpose | Primitive | Reference |
|---|---|---|
| Authenticated encryption | AES-256-GCM, 96-bit nonce, 128-bit tag | NIST SP 800-38D |
| Key derivation | HKDF with SHA-256 | RFC 5869 |
| Public-key agreement (request mode) | X25519 | RFC 7748 |
| Password hashing (optional link password) | Argon2id | RFC 9106 |
| Password-authenticated key exchange (live mode) | CPace over ristretto255 with SHA-512 | draft-irtf-cfrg-cpace-20 |
| Hashing, commitments | SHA-256, HMAC-SHA-256 | FIPS 180-4, RFC 2104 |
| Randomness | the platform CSPRNG (`crypto/rand`, `crypto.getRandomValues`) | |

AES-GCM was chosen over ChaCha20-Poly1305 because browsers offer it natively through the Web Crypto API (hardware-accelerated, no extra library) and Go has it in its standard library.

**Conventions.** Integers are unsigned big-endian. `||` is concatenation. `b64u` is base64url without padding (RFC 4648 §5). Strings are UTF-8. `HKDF(salt, ikm, info, L)` is Extract-then-Expand producing `L` bytes. Labels are ASCII and start with `zarf v1 `, so keys from different purposes can never collide.

## 2. The envelope

Every file, in every mode, travels as one *envelope*: a header, an encrypted metadata block, and the encrypted content in chunks.

```
envelope = header || meta_ct || chunk_ct[0] || chunk_ct[1] || ... || chunk_ct[n-1]
```

### 2.1 Header

| Field | Size | Value |
|---|---|---|
| `magic` | 4 | `ZARF` (0x5A 0x41 0x52 0x46) |
| `version` | 1 | `0x01` |
| `mode` | 1 | `0x01` link, `0x02` link + password, `0x03` request, `0x04` live |
| `chunk_log2` | 1 | `16` (64 KiB chunks); readers accept 12–20 |
| `salt` | 32 | random, new for every envelope |
| `mode_params` | varies | see below |
| `meta_ct_len` | 4 | length of `meta_ct` in bytes |
| `commitment` | 32 | key commitment, section 2.3 |

`mode_params`:

- `0x01` link, `0x04` live: empty.
- `0x02` link + password: `argon_m_kib` (4) ‖ `argon_t` (4) ‖ `argon_p` (1) ‖ `pw_salt` (16). Defaults: m = 47 104 KiB (46 MB), t = 1, p = 1 (the first OWASP setting, chosen so that phones finish in a few seconds). Readers refuse m > 1024 MB or t > 10, so a hostile header cannot exhaust memory.
- `0x03` request: `eph_pub` (32), the sender's one-time X25519 public key.

`hh = SHA-256(header)` is used as associated data everywhere below, so changing any header byte makes every decryption fail.

### 2.2 Keys

Each mode first produces input keying material `ikm` (section 3–5). Then:

```
prk         = HKDF-Extract(salt, ikm)
content_key = HKDF-Expand(prk, "zarf v1 content",    32)
meta_key    = HKDF-Expand(prk, "zarf v1 metadata",   32)
commit_key  = HKDF-Expand(prk, "zarf v1 commitment", 32)
```

Because `salt` is random for every envelope, `content_key` and `meta_key` are unique per envelope even if `ikm` were ever reused. That is what makes the counter nonces in 2.4 safe.

### 2.3 Key commitment

AES-GCM does not bind a ciphertext to one key: a crafted ciphertext can decrypt under several keys. To rule that out (and the "partitioning oracle" attacks it enables against password guesses), the header carries

```
commitment = HMAC-SHA-256(commit_key, "zarf v1 commit" || hh_without_commitment)
```

where `hh_without_commitment` is SHA-256 of the header up to and excluding the `commitment` field. A reader derives the keys, checks the commitment in constant time, and stops with "wrong key" before touching any ciphertext.

### 2.4 Content: chunked AEAD (STREAM)

The plaintext content is split into chunks of `2^chunk_log2` bytes; the last chunk may be shorter, and is empty (0 bytes) only when the whole content is empty. Chunk `i` of `n` is sealed as

```
nonce_i    = uint88(i) || last_i          (11-byte counter, then 0x01 for the last chunk, else 0x00)
chunk_ct_i = AES-256-GCM-Seal(content_key, nonce_i, chunk_i, aad = hh)
```

This is the STREAM construction (Hoang, Reyhanitabar, Rogaway, Vizár, 2015). It gives:

- **No reordering or duplication:** chunk `i` only opens with counter `i`.
- **No truncation:** a reader that reaches the end of the data without having opened a chunk flagged *last* reports an error; a stream cut at a chunk boundary is detected.
- **No extension:** any bytes after the last chunk are an error.
- **Streaming:** the reader can write each chunk out as soon as it verifies, so files never have to fit in memory. A writer must not show a file as complete until the last chunk verified.

### 2.5 Metadata

```
meta    = JSON, UTF-8: {"v":1, "files":[{"name":..., "size":..., "type":...}], "zip": false}
padded  = meta || 0x80 || 0x00 ... up to a multiple of 1024 bytes
meta_ct = AES-256-GCM-Seal(meta_key, 0^12, padded, aad = hh)
```

The JSON is written in one canonical form, so every implementation produces the same bytes: the fields in the order shown (`type` left out when empty), no spaces or line breaks, and no escaping beyond what JSON requires (`<`, `>` and `&` stay as they are). The nonce can be zero because `meta_key` is used exactly once. Padding hides the exact length of file names. With several files, the sender packs them into one ZIP archive without compression (`"zip": true`) and lists them in `files`; the content is that archive.

Names are data, never paths: readers keep only the last path component, drop control characters, `/`, `\`, `..` and leading dots, and never overwrite an existing file without asking.

## 3. Link mode

1. The sender draws `file_key` = 32 random bytes.
2. Without a password: `ikm = file_key`, mode `0x01`.
   With a password: `pw_key = Argon2id(password, pw_salt, m, t, p, 32)` and `ikm = file_key || pw_key`, mode `0x02`. Both the link and the password are needed; neither alone opens the file.
3. The sender uploads the envelope (section 6) and shares
   `https://<host>/d/<id>#<b64u(file_key)>`
   The part after `#` is never sent to the server by browsers. The page reads it and then removes it from the address bar (`history.replaceState`) so it does not linger in history or screenshots.

**Download authorization.** So that someone who only learned an `id` cannot use up a file's download limit, the server keeps `dl_auth_hash = SHA-256(HKDF(salt = "", ikm = file_key, info = "zarf v1 download auth", 32))` and asks the downloader for the preimage. This value is derived separately from the encryption keys and reveals nothing about them.

## 4. Request mode (asking someone for files)

1. For each request the requester draws a fresh X25519 key pair `(req_priv, req_pub)`. In the browser the private key is a non-extractable Web Crypto key, kept in that browser's IndexedDB together with the owner token; the request can be opened only there. A key per request means one lost or leaked key exposes one request, nothing else.
2. The requester creates a request (section 6) and shares
   `https://<host>/r/<rid>#<b64u(req_pub)>`
   The public key travels in the `#` part, so the server never handles it and cannot swap in a key of its own.
3. The uploader draws an ephemeral key pair, computes
   `shared = X25519(eph_priv, req_pub)` (rejecting the all-zero result),
   `ikm = shared || eph_pub || req_pub`, mode `0x03`, `eph_pub` in the header,
   and uploads the envelope to the request.
4. The requester recomputes `shared = X25519(req_priv, eph_pub)` and opens the envelope.

Including both public keys in `ikm` binds the key to this exact pair, as HPKE does. Both pages show a short *fingerprint* of `req_pub`: the first 5 bytes of SHA-256(req_pub), each byte a position in the word list of section 5.1, so five words to compare over another channel when the link itself passed through hands that are not trusted.

The request's *label* ("ID copy and tax certificate") is stored in plain text so the uploader's page can show it; the server can read it, and the interface says so. It is one line of at most 100 characters.

## 5. Live mode (send with a short code)

### 5.1 The code

`<channel>-<word>-<word>`, for example `7-elma-kule` or `7-apple-tower`. `channel` is a number the server hands out (it only routes the two peers to each other). The two words carry 16 bits of secret: each is one of 256 positions, written in Turkish or in English.

- There are two word lists (`web/src/lib/words.ts`) with 256 short, ASCII-only words each (the Turkish list has no ç, ğ, ı, ö, ş or ü, so anyone can type it). Word *k* of one list means the same as word *k* of the other.
- A code is shown in the sender's interface language, and the receiver may type either language, or even mix them: `7-elma-tower` is the same code as `7-apple-kule`.
- What enters the PAKE is therefore the two **positions**, never the words. A word that appears in both lists must sit at the same position in both; a test checks this, so no word can mean two different positions.
- Typing is forgiving: case is ignored, and spaces or dots work as separators.

16 bits are enough because of the PAKE (magic-wormhole makes the same choice): an attacker gets exactly one guess per channel, because a channel serves a single session and closes for good when either side leaves. One guess in 65 536, and a failed guess is visible to the sender.

### 5.2 Messages

Both peers open a WebSocket to `/api/v1/live/<channel>`. Messages are JSON text frames with a `t` (type) field, except envelope bytes, which are binary frames of at most 1 MiB.

1. **Joining.** The sender's first message is `{"t":"join","role":"send","token":<b64u>}` with the token from `POST /live`; the receiver's is `{"t":"join","role":"receive"}`. When both are in, the server sends both `{"t":"start","sid":<b64u, 16 random bytes>}`. From then on it forwards every message to the other peer unchanged, and nothing else.
2. **PAKE.** CPace (draft-irtf-cfrg-cpace-20) with ristretto255 and SHA-512, initiator/responder transcript (`transcript_ir`), the sender as initiator A:
   `PRS = "zarf v1 live" || uint16(position_1) || uint16(position_2)`, `CI = lv_cat("zarf v1 live", channel as decimal ASCII)`, `ADa = "send"`, `ADb = "receive"`.
   Each side sends `{"t":"pake","y":b64u(Y)}` and derives the 64-byte `ISK`. An invalid or identity point aborts.
3. **Key confirmation.** `confirm_key = HKDF(salt = sid, ikm = ISK, "zarf v1 confirm", 32)`. The sender sends `{"t":"confirm","mac":b64u(HMAC-SHA-256(confirm_key, "send" || Ya || Yb))}`, the receiver the same with `"receive"`. A wrong value means a wrong code (or an attacker's guess): that side closes, and the channel is gone.
4. **Path.** Each side sends `{"t":"hello","direct":true|false}`: whether it can open a direct WebRTC connection (browsers can; the command line tool says `false`). If both can:
   - `signal_key = HKDF(salt = sid, ikm = ISK, "zarf v1 signal", 32)`.
   - The sender creates a peer connection with one ordered, reliable data channel named `zarf`, and the two sides exchange the offer, the answer and their ICE candidates as `{"t":"signal","c":b64u(AES-256-GCM(signal_key, nonce, JSON))}`. The nonce is one byte for the direction (`0x01` sender, `0x02` receiver) and an 11-byte counter, so no nonce repeats. Because the SDP carries the DTLS certificate fingerprints and the server cannot forge or read these messages, it cannot place itself inside the direct connection.
   - The ICE servers are the Zarf server's own STUN responder (UDP port 3478 on the same host), so no third party learns anyone's address.
   - If the data channel opens within 10 seconds, the sender sends `{"t":"path","via":"direct"}` over the relay, and every following message travels on the data channel. Otherwise it sends `{"t":"path","via":"relay"}` and everything stays on the relay.
   When either side cannot, the sender sends `{"t":"path","via":"relay"}` at once.
5. **Offer.** `ikm = HKDF(salt = sid, ikm = ISK, "zarf v1 live content", 32)`, mode `0x04`. The sender sends the first part of the envelope, `header || meta_ct`, as one binary message. The receiver opens the metadata, shows the file's name and size, and answers `{"t":"accept"}` or `{"t":"decline"}`, so nobody receives a file they did not agree to.
6. **Transfer.** After `accept` the sender sends the encrypted chunks as binary messages, then `{"t":"done"}`. The receiver verifies the last chunk, answers `{"t":"received"}`, and both close.

The envelope is encrypted end to end on either path; the path changes speed, not security. On the relay the server sees only sizes and timing. On a direct connection the two peers see each other's network addresses, which is why the ICE exchange starts only after key confirmation: someone without the code never learns them.

The server closes a channel when either side leaves, after 10 minutes without a receiver, after 2 hours in total, or when more than 1100 MB have passed through it.

## 6. Server API

The server also answers STUN binding requests (RFC 8489, the binding method only) on UDP port 3478, for the direct connections of section 5.2.


All paths start with `/api/v1`. Bodies are JSON unless noted. Errors are `{"error": "<code>"}` with a fitting status. IDs are 16 random bytes in `b64u` (22 characters); secrets returned once are 32 random bytes in `b64u`, and the server stores only their SHA-256.

**Link uploads**

| Method and path | Purpose |
|---|---|
| `POST /uploads` `{size, expires_in, max_downloads, dl_auth_hash}` | Reserve space. Returns `{id, upload_token, delete_token, part_size}`. |
| `PUT /uploads/{id}/parts/{n}` (binary, `Authorization: Bearer upload_token`) | Store part `n` (8 MiB, the last may be shorter). Repeating a part is allowed. |
| `GET /uploads/{id}` (upload token) | Which parts arrived, so an interrupted upload resumes. |
| `POST /uploads/{id}/complete` (upload token) | Check every part is there and the total size matches; publish. |
| `GET /files/{id}` | `{size, expires_at, downloads_left}` — no secrets. `404` if it never existed, `410` if it expired or was used up. |
| `POST /files/{id}/download` `{dl_auth}` | Spend one download; returns a short-lived `ticket`. |
| `GET /files/{id}/content` (`Authorization: Bearer ticket`) | The envelope bytes; `Range` requests allowed for resuming. The ticket travels in a header, never in the address, so it stays out of logs. |
| `DELETE /files/{id}` (`Authorization: Bearer delete_token`) | Delete now. |
| `POST /files/{id}/report` `{reason}` | Report abuse to the operator. |

**Requests**

| Method and path | Purpose |
|---|---|
| `POST /requests` `{label, expires_in, max_uploads}` | Returns `{rid, owner_token, expires_at}`. |
| `GET /requests/{rid}` | `{label, expires_at, uploads_left}` for the uploader's page. With the owner token it also lists the finished uploads: `files: [{id, size, created_at, downloads_left}]`. `404` / `410` as for files. |
| `POST /requests/{rid}/uploads` `{size}` | Like `POST /uploads`; the file inherits the request's expiry. Refused with `410` when the request expired or has all its uploads. Then the same `parts` and `complete` calls. |
| `POST /requests/{rid}/files/{id}/download` (owner token) | Spend one of the file's 20 downloads; returns a `ticket` for `GET /files/{id}/content`. |
| `DELETE /requests/{rid}` (owner token) | Close the request and delete what was uploaded to it. |

**Live**

| Method and path | Purpose |
|---|---|
| `POST /live` | Returns `{channel, token}`; the token lets only the creator take the sender's place. |
| `GET /live/{channel}` | Upgrades to the WebSocket of section 5.2. |

## 6.1 The command line

The `zarf` tool speaks the same protocol for every mode: `zarf send` and `zarf receive` for links, `zarf send --to <request link>` to answer a request, `zarf request` to create, list, fetch and close requests (the private key is kept in the user's configuration folder with owner-only permissions), and `zarf send --code` / `zarf receive <code>` for live mode, always over the relay.

## 7. Limits of the public instance

| | Value |
|---|---|
| Envelope size | 1024 MB at most |
| Expiry | 1 hour, 1 day or 7 days |
| Downloads per link | 1, 5, 10 or 20 |
| Uploads per address | 20 per hour |
| Requests | up to 1, 5, 10 or 20 uploads; each upload can be downloaded 20 times |
| Live relay | 1100 MB per channel, one transfer per channel, 2 hours at most |
| Storage in total | 100 GB; new uploads are refused when full |

Self-hosters change these in the server's configuration.

## 8. Versioning

A reader refuses any `version` it does not know. New modes take new `mode` values. A change that alters existing bytes gets a new version number, never a silent change.

## 9. Test vectors

`testdata/vectors/*.json` hold fixed inputs (keys, salts, passwords, plaintexts) and the exact expected bytes for every mode, plus vectors that must be *rejected*: a reordered chunk, a truncated stream, trailing bytes, a flipped header byte, a wrong key, a bad commitment, oversized Argon2 parameters. The Go and TypeScript test suites both run every vector, so the two implementations cannot drift apart unnoticed.
