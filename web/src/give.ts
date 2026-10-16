// The page a request link opens (/r/<rid>#<public key>): the person asked chooses a file, and
// it is encrypted here with the requester's public key (X25519 with a one-time key of this
// page), so only the requester can open it. Not even this page can, once it is sent.

import "./styles.css";
import { fromB64u } from "./lib/bytes.ts";
import { EnvelopeWriter, envelopeSize, Mode } from "./lib/envelope.ts";
import { generateX25519, requestKey } from "./lib/keys.ts";
import { ApiError, completeUpload, createRequestUpload, requestInfo, retry, type RequestInfo } from "./lib/api.ts";
import { ReadError, uploadEnvelope } from "./lib/upload.ts";
import { fingerprint } from "./lib/words.ts";
import { apply, formatDate, formatSize, lang, setLang, t, type Key } from "./i18n.ts";
import { initTheme } from "./theme.ts";
import { flyAway, resetEnvelope } from "./flight.ts";

const MAX_ENVELOPE = 1024 * 1024 * 1024;
const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;

apply();
initTheme($("[data-theme-toggle]"));
$("[data-lang]").addEventListener("click", () => setLang(lang === "tr" ? "en" : "tr"));

const rid = /^\/r\/([A-Za-z0-9_-]{22})\/?$/.exec(location.pathname)?.[1] ?? null;
const reqPub = fromB64u(location.hash.slice(1));
// The public key is no secret, so it stays in the address: a reload keeps working.
addEventListener("hashchange", () => location.reload());

let info: RequestInfo | null = null;
let file: File | null = null;
let busy = false;
let status: { key: Key; vars?: Record<string, string | number>; kind: string } | null = null;

function setStatus(key: Key | null, vars?: Record<string, string | number>, kind = ""): void {
  status = key ? { key, vars, kind } : null;
  const s = $("[data-status]");
  s.textContent = key ? t(key, vars) : "";
  s.className = "status" + (kind ? ` status--${kind}` : "");
}

function setProgress(done: number | null): void {
  const bar = $("[data-progress]");
  bar.hidden = done === null;
  if (done !== null) bar.style.setProperty("--done", `${done}%`);
}

async function showFacts(): Promise<void> {
  if (!info || !reqPub) return;
  $("[data-label]").textContent = info.label;
  $("[data-asked]").hidden = !info.label;
  $("[data-facts]").textContent = t("give.facts", { date: formatDate(new Date(info.expires_at)), n: info.uploads_left });
  $("[data-fp]").textContent = await fingerprint(reqPub, lang);
}

document.addEventListener("zarf:lang", () => {
  showFacts();
  if (status) setStatus(status.key, status.vars, status.kind);
  if (file) $("[data-file-meta]").textContent = formatSize(file.size);
});

/** Stops the page with a message; nothing more can be done from this link. The envelope
 * stays, closed, with the message under its window. */
function dead(key: Key): void {
  setStatus(key, undefined, "error");
  $("[data-envelope]").classList.add("is-sealed", "is-instant");
  $("[data-submit]").hidden = true;
}

function choose(f: File | null | undefined): void {
  if (!f || busy || !info) return;
  file = f;
  setStatus(null);
  $("[data-empty]").hidden = true;
  $("[data-chosen]").hidden = false;
  $("[data-file-name]").textContent = f.name;
  $("[data-file-meta]").textContent = formatSize(f.size);
}

class StopError extends Error {
  readonly key: Key;
  constructor(key: Key) {
    super(key);
    this.key = key;
  }
}

async function send(): Promise<void> {
  if (!file) throw new StopError("err.no_file");
  if (file.size === 0) throw new StopError("err.empty_file");
  const f = file;
  setStatus("status.preparing", { name: f.name });
  // A fresh key pair for this file: the shared key binds both public keys (PROTOCOL.md section 4).
  const eph = await generateX25519();
  const ikm = await requestKey(eph.privateKey, reqPub!, eph.publicKey, reqPub!);
  const writer = await EnvelopeWriter.create({ mode: Mode.Request, ephPub: eph.publicKey }, ikm, {
    files: [{ name: f.name, size: f.size, type: f.type || undefined }],
  });
  const total = envelopeSize(writer.head.length, f.size);
  if (total > MAX_ENVELOPE) throw new StopError("err.too_large");

  const upload = await createRequestUpload(rid!, total);
  $("[data-envelope]").classList.add("is-sealed");
  await uploadEnvelope(upload, writer, f, total, (p) => {
    setStatus("give.sending", { p }, "sealed");
    setProgress(p);
  });
  await retry(() => completeUpload(upload));
  setProgress(null);
  setStatus("give.done", { name: f.name }, "sealed");
  await flyAway($("[data-envelope]"));

  $("[data-envelope]").hidden = true;
  $("[data-submit]").hidden = true;
  $("[data-done-text]").textContent = t("give.done", { name: f.name });
  info = await requestInfo(rid!).catch(() => info);
  await showFacts();
  $("[data-again]").hidden = !info || info.uploads_left === 0;
  $("[data-done]").hidden = false;
}

function explain(err: unknown): { key: Key; vars?: Record<string, string> } {
  if (err instanceof StopError) return { key: err.key };
  if (err instanceof ReadError) return { key: "err.read" };
  if (err instanceof ApiError) {
    if (err.code === "request_full") return { key: "give.full" };
    if (err.status === 410) return { key: "give.gone" };
    if (err.status === 404) return { key: "give.not_found" };
    if (err.code === "network") return { key: "err.network" };
    if (err.code === "too_large") return { key: "err.too_large" };
    if (err.code === "too_many_requests") return { key: "err.too_many_requests" };
    if (err.code === "storage_full") return { key: "err.storage_full" };
    return { key: "err.generic", vars: { code: err.code } };
  }
  return { key: "err.generic", vars: { code: (err as Error)?.name ?? "error" } };
}

function again(): void {
  file = null;
  resetEnvelope($("[data-envelope]"));
  $("[data-envelope]").hidden = false;
  $("[data-submit]").hidden = false;
  $("[data-done]").hidden = true;
  $("[data-empty]").hidden = false;
  $("[data-chosen]").hidden = true;
  $<HTMLInputElement>("[data-file-input]").value = "";
  setStatus(null);
}

async function load(): Promise<void> {
  if (!rid) return dead("give.bad_link");
  if (!reqPub || reqPub.length !== 32) return dead("give.no_key");
  try {
    info = await requestInfo(rid);
  } catch (err) {
    return dead(explain(err).key);
  }
  await showFacts();
  if (info.uploads_left === 0) return dead("give.full");
  $("[data-submit]").hidden = false;
}

const input = $<HTMLInputElement>("[data-file-input]");
const envelope = $("[data-envelope]");
document.querySelectorAll("[data-pick]").forEach((b) => b.addEventListener("click", () => input.click()));
input.addEventListener("change", () => choose(input.files?.[0]));
addEventListener("dragover", (e) => e.preventDefault());
addEventListener("drop", (e) => e.preventDefault());
envelope.addEventListener("dragenter", () => envelope.classList.add("is-dragging"));
envelope.addEventListener("dragleave", (e) => {
  if (!envelope.contains(e.relatedTarget as Node)) envelope.classList.remove("is-dragging");
});
envelope.addEventListener("drop", (e) => {
  envelope.classList.remove("is-dragging");
  if (e.dataTransfer?.items?.[0]?.webkitGetAsEntry?.()?.isDirectory) return setStatus("err.folder", undefined, "error");
  choose(e.dataTransfer?.files?.[0]);
});
addEventListener("beforeunload", (e) => {
  if (busy) e.preventDefault();
});
$<HTMLFormElement>("[data-give]").addEventListener("submit", async (e) => {
  e.preventDefault();
  if (busy || !info) return;
  busy = true;
  const submit = $<HTMLButtonElement>("[data-submit]");
  submit.disabled = true;
  try {
    await send();
  } catch (err) {
    resetEnvelope(envelope);
    setProgress(null);
    const why = explain(err);
    setStatus(why.key, why.vars, "error");
  } finally {
    busy = false;
    submit.disabled = false;
  }
});
$("[data-again]").addEventListener("click", again);

load();
