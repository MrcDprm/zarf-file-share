// Talking to the Zarf server (docs/PROTOCOL.md, section 6). Only envelopes, the download value
// and tokens the server handed out ever leave the browser; never a key.

import { toB64u } from "./bytes.ts";

/** The server said no; `code` is its short error code (or "network" when it could not be reached). */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  constructor(status: number, code: string) {
    super(code);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

const BASE = "/api/v1";

async function call<T>(method: string, path: string, opts: { token?: string; json?: unknown; body?: BodyInit; signal?: AbortSignal } = {}): Promise<T> {
  const headers: Record<string, string> = {};
  if (opts.token) headers.Authorization = `Bearer ${opts.token}`;
  let body = opts.body;
  if (opts.json !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(opts.json);
  }
  let res: Response;
  try {
    res = await fetch(BASE + path, { method, headers, body, signal: opts.signal, cache: "no-store", credentials: "omit" });
  } catch (err) {
    if ((err as Error).name === "AbortError") throw err;
    throw new ApiError(0, "network");
  }
  if (!res.ok) {
    let code = "unknown";
    try {
      code = ((await res.json()) as { error?: string }).error ?? code;
    } catch {
      /* not JSON */
    }
    throw new ApiError(res.status, code);
  }
  return res.status === 204 ? (undefined as T) : ((await res.json()) as T);
}

/** Retries network failures and server errors; a clear "no" (4xx) is final. */
export async function retry<T>(f: () => Promise<T>, attempts = 4, signal?: AbortSignal): Promise<T> {
  for (let i = 0; ; i++) {
    try {
      return await f();
    } catch (err) {
      const final = err instanceof ApiError && err.status >= 400 && err.status < 500;
      if (final || i + 1 >= attempts || signal?.aborted) throw err;
      await new Promise((r) => setTimeout(r, (i + 1) * (i + 1) * 500));
    }
  }
}

export interface Upload {
  id: string;
  upload_token: string;
  delete_token: string;
  part_size: number;
  expires_at: string;
}

export const createUpload = (size: number, expiresIn: number, maxDownloads: number, dlAuthHash: Uint8Array) =>
  call<Upload>("POST", "/uploads", { json: { size, expires_in: expiresIn, max_downloads: maxDownloads, dl_auth_hash: toB64u(dlAuthHash) } });

export const putPart = (u: Upload, n: number, bytes: Uint8Array<ArrayBuffer>, signal?: AbortSignal) =>
  call<void>("PUT", `/uploads/${u.id}/parts/${n}`, { token: u.upload_token, body: bytes, signal });

export const completeUpload = (u: Upload) => call<{ id: string; expires_at: string }>("POST", `/uploads/${u.id}/complete`, { token: u.upload_token });

export interface FileInfo {
  size: number;
  expires_at: string;
  downloads_left: number;
}

export const fileInfo = (id: string) => call<FileInfo>("GET", `/files/${id}`);

export const startDownload = (id: string, dlAuth: Uint8Array) =>
  call<{ ticket: string; size: number }>("POST", `/files/${id}/download`, { json: { dl_auth: toB64u(dlAuth) } });

export const deleteFile = (id: string, deleteToken: string) => call<void>("DELETE", `/files/${id}`, { token: deleteToken });

/**
 * Streams the envelope. If the connection breaks, it asks for the rest with a Range request
 * on the same ticket, so a long download does not start over.
 */
export async function* downloadContent(id: string, ticket: string, size: number, signal?: AbortSignal): AsyncGenerator<Uint8Array> {
  let offset = 0;
  let failures = 0;
  while (offset < size) {
    let res: Response;
    try {
      res = await fetch(`${BASE}/files/${id}/content`, {
        headers: { Authorization: `Bearer ${ticket}`, ...(offset > 0 ? { Range: `bytes=${offset}-` } : {}) },
        cache: "no-store",
        credentials: "omit",
        signal,
      });
    } catch (err) {
      if ((err as Error).name === "AbortError" || ++failures > 3) throw err instanceof Error && err.name === "AbortError" ? err : new ApiError(0, "network");
      await new Promise((r) => setTimeout(r, failures * 500));
      continue;
    }
    if (res.status !== (offset > 0 ? 206 : 200) || !res.body) throw new ApiError(res.status, "download_failed");
    const reader = res.body.getReader();
    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        offset += value.length;
        yield value;
      }
    } catch (err) {
      if ((err as Error).name === "AbortError" || ++failures > 3) throw err;
      await new Promise((r) => setTimeout(r, failures * 500));
      continue;
    }
    // The stream ended early without an error: that counts as a failure too, or a server
    // that keeps cutting the response short would keep us here for ever.
    if (offset < size && ++failures > 3) throw new ApiError(0, "network");
  }
}
