# Zarf

**English** | [Türkçe](README.tr.md)

End-to-end encrypted file sharing. Files are encrypted on your device before they leave it, so the server only ever stores and relays bytes it cannot read. Works in the browser and from the command line (`zarf`).

> **Status:** in development. This README is the project plan; it will be completed when the first release is out.

## Why

Accountants, lawyers and HR teams send ID scans, contracts and medical reports by e-mail every day. Ordinary file-sharing services can read what they store. Zarf is built so that the server cannot: the key never reaches it.

## Planned features (v1.0)

- **Send with a link:** the file is encrypted in the browser or CLI and uploaded. The key lives in the part of the link after `#`, which browsers never send to the server.
  - Expiry (1 hour, 1 day, 7 days) and a download limit (1, 5, 10).
  - Optional password: a leaked link alone cannot open the file.
  - The sender can delete the file at any time.
  - File names, sizes and types are encrypted too.
- **Request files:** you create a request link that carries your public key. Whoever uploads through it encrypts to that key, so only you can open the files.
- **Live send with a code:** while both sides are online, the sender reads out a short code (`7-apple-tower`). A password-authenticated key exchange (PAKE) turns it into a strong shared key; the transfer goes peer to peer (WebRTC in the browser), or through the server's relay when a direct connection is not possible.
- **Browser and CLI work together:** a file sent from one can be received with the other.
- **Resumable uploads** in encrypted chunks, so large files never have to fit in memory.
- **Self-hosting:** one Go binary with the web app built in.
- Turkish and English, light and dark theme.

## Security design

- Own protocol built only from standard, well-studied primitives: AES-256-GCM in a chunked STREAM construction, HKDF-SHA256, X25519, Argon2id and CPace over ristretto255.
- The protocol and the threat model are written down first ([docs/PROTOCOL.md](docs/PROTOCOL.md), [docs/THREAT-MODEL.md](docs/THREAT-MODEL.md)), and the code follows them.
- Shared test vectors: the Go and TypeScript implementations must produce and accept the same bytes.
- Tests for known attacks: reordered or truncated chunks, tampered headers, nonce reuse, wrong keys.
- The known limit of web-based encryption (a compromised server could serve malicious JavaScript) is stated openly; strict CSP and the CLI are the mitigations.

## Tech stack

- **Server and CLI:** Go (standard library `net/http`, `crypto`), WebSocket relay, SQLite
- **Web client:** TypeScript, Vite, Web Crypto API, WebRTC (no framework)
- **Hosting:** Oracle Cloud (Always Free VM), Caddy (HTTPS)

## Future plans

- Notify the sender by e-mail when a file is downloaded
- QR code for links and live-send codes
- Teams and accounts
