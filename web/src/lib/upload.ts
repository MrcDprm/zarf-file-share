// Uploading an envelope as it is sealed: the file is read in 64 KiB chunks, each chunk is
// encrypted, and the sealed bytes are gathered into upload parts sent one at a time. Even a
// 1024 MB file never sits in memory whole. Link and request mode both send this way.

import { putPart, retry, type Upload } from "./api.ts";
import type { EnvelopeWriter } from "./envelope.ts";

/** The browser could not read the chosen file (moved, deleted, or permission lost). */
export class ReadError extends Error {
  constructor() {
    super("could not read the file");
    this.name = "ReadError";
  }
}

/** Reads, seals and uploads part by part; a failed part is sent again, then the upload goes on. */
export async function uploadEnvelope(upload: Upload, writer: EnvelopeWriter, f: File, total: number, onProgress: (percent: number) => void): Promise<void> {
  const partSize = upload.part_size;
  let part = new Uint8Array(partSize);
  let filled = 0;
  let n = 0;
  let sent = 0;

  const flush = async () => {
    const bytes = part.slice(0, filled); // its own buffer, so the next part can be filled meanwhile
    const index = n++;
    await retry(() => putPart(upload, index, bytes));
    sent += bytes.length;
    onProgress(Math.floor((sent / total) * 100));
    part = new Uint8Array(partSize);
    filled = 0;
  };
  const push = async (bytes: Uint8Array) => {
    for (let at = 0; at < bytes.length; ) {
      const take = Math.min(bytes.length - at, partSize - filled);
      part.set(bytes.subarray(at, at + take), filled);
      filled += take;
      at += take;
      if (filled === partSize) await flush();
    }
  };

  await push(writer.head);
  for (let at = 0; ; at += writer.chunkSize) {
    const end = Math.min(at + writer.chunkSize, f.size);
    let plain: Uint8Array;
    try {
      plain = new Uint8Array(await f.slice(at, end).arrayBuffer());
    } catch {
      throw new ReadError();
    }
    const last = end >= f.size;
    await push(await writer.seal(plain, last));
    if (last) break;
  }
  if (filled > 0) await flush();
}

/** Saves verified bytes as a download. octet-stream: the browser saves them and never renders them. */
export function saveFile(parts: Uint8Array[], name: string): string {
  const url = URL.createObjectURL(new Blob(parts as BlobPart[], { type: "application/octet-stream" }));
  saveURL(url, name);
  return url;
}

export function saveURL(url: string, name: string): void {
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.rel = "noopener";
  document.body.append(a);
  a.click();
  a.remove();
}
