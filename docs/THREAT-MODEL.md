# Zarf threat model

**Status:** draft, written before the code. It says what Zarf protects, against whom, and, just as plainly, what it does not. The byte-level design is in [PROTOCOL.md](PROTOCOL.md).

## What is protected

| Asset | Goal |
|---|---|
| File contents | Only people holding the key can read them. |
| File names, types and exact sizes of individual files | Same as contents (they are in the encrypted metadata). |
| Integrity | Any change, reordering, truncation or extension of a file is detected before it is shown as complete. |
| The live-mode code | Cannot be guessed offline; an online guess costs a whole connection attempt. |
| The requester's private key (request mode) | Never leaves the requester's device. |

## Who we defend against

1. **The server and its operator**, including someone who has fully taken it over: they can read every stored byte, every log and every request.
2. **Someone on the network** between a user and the server, or between two peers.
3. **Someone who learns a link or an id** without the rest (for example from server logs, or a leaked link without its password).
4. **A malicious peer or file sender**, who can craft any bytes it likes.

## Guarantees

| Threat | Why it fails |
|---|---|
| The server reads a file | Keys are made on the users' devices; the link key sits after `#`, which browsers never send. The server only holds envelopes. |
| The server changes a file | AES-GCM tags, the header bound as associated data, and the STREAM counter and last-chunk flag reject any change, reordering or cut. |
| A crafted envelope opens under two keys (e.g. to test passwords) | Key commitment in the header is checked before decryption. |
| A leaked link without its password opens the file | In password mode the key needs both the link key and the Argon2id output. |
| Someone with only the id burns the download limit | Spending a download needs a value derived from the link key. |
| Guessing ids or tokens | 128-bit random ids, 256-bit random tokens; only hashes of tokens are stored. |
| The server, or the network, impersonates a peer in live mode | CPace: without the code there is one guess per channel, and the channel closes after a failure. Key confirmation detects a wrong code before any file byte is sent. |
| Someone receives a file they did not want (live mode) | The receiver sees the name and size and accepts before any content is sent. |
| The server slips itself into a direct live connection | The WebRTC offer, answer and candidates are encrypted and authenticated with a key from the PAKE; the DTLS fingerprints inside them cannot be swapped. |
| A hostile header exhausts memory | Readers cap Argon2id parameters, chunk sizes and metadata length. |
| A file name escapes the download folder (`../../x`) | Names are reduced to a bare, cleaned file name; existing files are never overwritten silently. |
| Abuse of the service | Size, expiry and per-address limits, reporting and operator removal. |

## Not protected (and said so)

- **A compromised web server can send malicious JavaScript.** Encryption in the browser is only as trustworthy as the code the server delivers. Mitigations: a strict Content Security Policy with no third-party scripts, Subresource Integrity, published hashes of each release's web files, and the CLI, which does not depend on code from the server. Users who cannot accept this should use the CLI.
- **A compromised device.** Malware on the sender's or receiver's device sees the plaintext.
- **Where the link travels.** Whoever gets the full link (without a password) can open the file until it expires or runs out of downloads. Chat apps may also fetch links for previews; that costs nothing, because previews never send the part after `#`, but the password option exists for links that travel through untrusted places.
- **Addresses on a direct connection.** When live mode connects the two browsers directly, each sees the other's network address, as in any call. This happens only after both proved they hold the code.
- **Metadata visible to the server.** The total envelope size (rounded only by chunking), times of upload and download, the request's label, and the network addresses of connections. Zarf does not store addresses in its database or logs, but it cannot hide them from the network or the hosting provider.
- **Request-mode key substitution by whoever delivers the link.** The requester's public key is in the link itself; if an attacker can change the link on its way, they can change the key. The short fingerprint lets both sides check it over another channel.
- **Content scanning.** Because files are encrypted end to end, the server cannot scan them for malware or illegal content. Reports and operator removal are the only tools, and the README states this.
- **Availability.** A free, single server can be overloaded or go down. Zarf limits the damage per address, but does not promise uptime.

## Assumptions

- The platform CSPRNG, the Web Crypto API and Go's `crypto` packages are correct.
- The libraries used for Argon2id and ristretto255 in the browser (`@noble/hashes`, `@noble/curves`) and in Go (`golang.org/x/crypto`, `github.com/gtank/ristretto255`) are correct; they are widely used and audited.
- Users compare fingerprints when the stakes call for it.

## How this is checked

- Shared test vectors for every mode and every rejection case (PROTOCOL.md §9), run by both implementations.
- Tests that play each attack in the table above against the real code: tampered, reordered, cut and extended envelopes; wrong keys; bad commitments; hostile headers; path-escaping names; spending downloads without authorization; a wrong live code.
- A final review against this document before every release.
