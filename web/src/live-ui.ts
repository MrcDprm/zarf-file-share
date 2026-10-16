// Live mode on the page: the sender picks a file and gets a short code to read out; the
// receiver types it in the same tab, sees the file's name and size, and accepts.

import { createLive, liveSocketURL, ApiError } from "./lib/api.ts";
import { LiveError, Pipe, handshake, receiveFile, sendFile, type SocketLike } from "./lib/live.ts";
import { formatCode, parseCode, type Code } from "./lib/words.ts";
import { safeName } from "./lib/safename.ts";
import { saveFile, saveURL } from "./lib/upload.ts";
import type { FileInfo } from "./lib/envelope.ts";
import { formatSize, lang, t, type Key } from "./i18n.ts";

const MAX_FILE = 1024 * 1024 * 1024 - 64 * 1024 * 1024; // the relay passes 1100 MB at most
const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;

type Said = { key: Key; vars?: Record<string, string | number>; kind: string };
const said: Record<"send" | "receive", Said | null> = { send: null, receive: null };

function say(side: "send" | "receive", key: Key | null, vars?: Record<string, string | number>, kind = ""): void {
  said[side] = key ? { key, vars, kind } : null;
  const s = $(`[data-live-${side}-status]`);
  s.textContent = key ? t(key, vars) : "";
  s.className = "status" + (kind ? ` status--${kind}` : "");
}

function progress(side: "send" | "receive", done: number | null): void {
  const bar = $(`[data-live-${side}-progress]`);
  bar.hidden = done === null;
  if (done !== null) bar.style.setProperty("--done", `${done}%`);
}

function problemKey(err: unknown, side: "send" | "receive"): Key {
  if (err instanceof LiveError) {
    if (err.problem === "wrong_code") return side === "send" ? "live.err.wrong_code.send" : "live.err.wrong_code.receive";
    return `live.err.${err.problem}` as Key;
  }
  if (err instanceof ApiError && err.code === "too_many_requests") return "err.too_many_requests";
  return "live.err.network";
}

/** Opens the channel's WebSocket; rejects if it cannot connect at all. */
function connect(channel: number): Promise<Pipe> {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(liveSocketURL(channel));
    const pipe = new Pipe(ws as unknown as SocketLike); // a WebSocket is a SocketLike; its event types are just wider
    ws.addEventListener("open", () => resolve(pipe), { once: true });
    ws.addEventListener("error", () => reject(new LiveError("network")), { once: true });
  });
}

// --- sending ---

let sending: { pipe: Pipe | null; code: Code | null; file: File } | null = null;

function showCode(): void {
  if (sending?.code) $("[data-live-code]").textContent = formatCode(sending.code, lang);
}

function sendDone(): void {
  sending = null;
  $("[data-live-cancel]").hidden = true;
  $("[data-live-start]").hidden = false;
  $("[data-live-code-box]").hidden = true;
  $<HTMLInputElement>("[data-live-file]").value = "";
}

async function startSending(f: File): Promise<void> {
  if (f.size === 0) return say("send", "err.empty_file", undefined, "error");
  if (f.size > MAX_FILE) return say("send", "err.too_large", undefined, "error");
  const me = { pipe: null as Pipe | null, code: null as Code | null, file: f };
  sending = me;
  $("[data-live-start]").hidden = true;
  $("[data-live-file-name]").textContent = `${f.name} (${formatSize(f.size)})`;
  $("[data-live-file-name]").hidden = false;
  say("send", "live.getting");
  try {
    const { channel, token } = await createLive();
    // Two random positions: 16 bits of secret. The server only ever learns the channel number.
    const r = crypto.getRandomValues(new Uint8Array(2));
    const code: Code = { channel, positions: [r[0]!, r[1]!] };
    const pipe = await connect(channel);
    if (sending !== me) return pipe.close(); // cancelled meanwhile
    me.pipe = pipe;
    me.code = code;
    showCode();
    $("[data-live-code-box]").hidden = false;
    $("[data-live-cancel]").hidden = false;
    say("send", "live.waiting");
    // handshake() waits for the receiver; "start" arrives when they join.
    const waitStart = handshake(pipe, "send", code, token);
    const ikm = await waitStart;
    say("send", "live.offered");
    $("[data-live-cancel]").hidden = true;
    await sendFile(
      pipe,
      ikm,
      f,
      () => say("send", "live.offered"),
      (done, total) => {
        const p = Math.floor((done / total) * 100);
        say("send", "live.sending", { p }, "sealed");
        progress("send", p);
      },
    );
    progress("send", null);
    say("send", "live.sent", { name: f.name }, "sealed");
    pipe.close();
  } catch (err) {
    progress("send", null);
    me.pipe?.close();
    if (sending === me) say("send", problemKey(err, "send"), undefined, "error"); // not when cancelled
  } finally {
    sendDone();
  }
}

// --- receiving ---

let receiving = false;
let savedURL: { url: string; name: string } | null = null;
let answer: ((yes: boolean) => void) | null = null;

function decide(f: FileInfo): Promise<boolean> {
  $("[data-live-offer-name]").textContent = safeName(f.name);
  $("[data-live-offer-meta]").textContent = t("live.offer.meta", { size: formatSize(f.size) });
  $("[data-live-offer]").hidden = false;
  say("receive", null);
  $("[data-live-accept]").focus();
  return new Promise((resolve) => (answer = resolve));
}

async function startReceiving(text: string): Promise<void> {
  const parsed = parseCode(text);
  if ("problem" in parsed) return say("receive", `live.err.${parsed.problem}` as Key, { word: parsed.word ?? "" }, "error");
  receiving = true;
  $<HTMLButtonElement>("[data-live-connect]").disabled = true;
  $("[data-live-save]").hidden = true;
  say("receive", "live.connecting");
  let pipe: Pipe | null = null;
  try {
    pipe = await connect(parsed.channel);
    const ikm = await handshake(pipe, "receive", parsed);
    const got = await receiveFile(pipe, ikm, decide, (done, total) => {
      const p = total ? Math.floor((done / total) * 100) : 100;
      say("receive", "live.receiving", { p }, "sealed");
      progress("receive", p);
    });
    progress("receive", null);
    if (!got) {
      say("receive", "live.declined.receiver");
      return;
    }
    const name = safeName(got.file.name);
    if (savedURL) URL.revokeObjectURL(savedURL.url);
    savedURL = { url: saveFile(got.parts, name), name };
    say("receive", "live.received", undefined, "sealed");
    $("[data-live-save]").hidden = false;
    $<HTMLInputElement>("[data-live-code-input]").value = "";
  } catch (err) {
    progress("receive", null);
    say("receive", problemKey(err, "receive"), undefined, "error");
  } finally {
    pipe?.close();
    $("[data-live-offer]").hidden = true;
    $<HTMLButtonElement>("[data-live-connect]").disabled = false;
    receiving = false;
  }
}

export function initLive(): void {
  const input = $<HTMLInputElement>("[data-live-file]");
  $("[data-live-start]").addEventListener("click", () => input.click());
  input.addEventListener("change", () => {
    const f = input.files?.[0];
    if (f && !sending) startSending(f);
  });
  $("[data-live-cancel]").addEventListener("click", () => {
    const pipe = sending?.pipe;
    sending = null;
    pipe?.close();
    say("send", null);
    $("[data-live-file-name]").hidden = true;
  });
  $("[data-live-copy]").addEventListener("click", async (e) => {
    const button = e.currentTarget as HTMLButtonElement;
    try {
      await navigator.clipboard.writeText($("[data-live-code]").textContent ?? "");
      button.textContent = t("receipt.copied");
      setTimeout(() => (button.textContent = t("live.copy")), 2000);
    } catch {
      getSelection()?.selectAllChildren($("[data-live-code]"));
    }
  });

  $<HTMLFormElement>("[data-live-join]").addEventListener("submit", (e) => {
    e.preventDefault();
    if (!receiving) startReceiving($<HTMLInputElement>("[data-live-code-input]").value);
  });
  $("[data-live-accept]").addEventListener("click", () => {
    $("[data-live-offer]").hidden = true;
    answer?.(true);
    answer = null;
  });
  $("[data-live-decline]").addEventListener("click", () => {
    $("[data-live-offer]").hidden = true;
    answer?.(false);
    answer = null;
  });
  $("[data-live-save]").addEventListener("click", () => savedURL && saveURL(savedURL.url, savedURL.name));

  document.addEventListener("zarf:lang", () => {
    showCode();
    for (const side of ["send", "receive"] as const) {
      const s = said[side];
      if (s) say(side, s.key, s.vars, s.kind);
    }
  });
  // Closing the page ends the channel: while a code is out or a transfer runs, ask first.
  addEventListener("beforeunload", (e) => {
    if (sending || receiving) e.preventDefault();
  });
}
