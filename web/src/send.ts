// Sending with a link: the file is encrypted as it is read and uploaded part by part
// (lib/upload.ts). The key goes only into the link's "#" part.

import { randomBytes, toB64u, toHex } from "./lib/bytes.ts";
import { DEFAULT_ARGON, EnvelopeWriter, envelopeSize, Mode, sha256, type WriterParams } from "./lib/envelope.ts";
import { downloadAuth, linkKey, passwordKey } from "./lib/keys.ts";
import { ApiError, completeUpload, createUpload, retry } from "./lib/api.ts";
import { ReadError, uploadEnvelope } from "./lib/upload.ts";
import { formatDate, formatSize, t, type Key } from "./i18n.ts";
import { flyAway, resetEnvelope } from "./flight.ts";
import { remember } from "./log.ts";

const MAX_ENVELOPE = 1024 * 1024 * 1024; // the public server's limit: 1024 MB
const MIN_PASSWORD = 8;

const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;

let file: File | null = null;
let busy = false;

class UserError extends Error {}

function fail(key: Key, vars?: Record<string, string>): never {
  throw new UserError(t(key, vars));
}

function setStatus(text: string, kind: "" | "sealed" | "error" = ""): void {
  const s = $("[data-status]");
  s.textContent = text;
  s.className = "status" + (kind ? ` status--${kind}` : "");
}

function setProgress(done: number | null): void {
  const bar = $("[data-progress]");
  bar.hidden = done === null;
  if (done !== null) bar.style.setProperty("--done", `${done}%`);
}

const describe = (f: File) => `${formatSize(f.size)}${f.name.includes(".") ? ", " + f.name.split(".").pop()!.toUpperCase() : ""}`;

/** The slip beside the form: what the person sees, next to bytes the server would get. */
function showSlip(head: Uint8Array | null): void {
  const you = $("[data-you]");
  const bytes = $("[data-bytes]");
  const foot = $("[data-slip-foot]");
  if (!file || !head) return;
  const name = document.createElement("span");
  name.textContent = file.name;
  const size = document.createElement("span");
  size.className = "slip__size";
  size.textContent = formatSize(file.size);
  you.replaceChildren(name, document.createElement("br"), size);
  bytes.classList.add("is-live");
  bytes.textContent = `${toHex(head.subarray(0, 48)).match(/.{8}/g)!.join(" ")} …`;
  foot.removeAttribute("data-i18n");
  foot.textContent = t("slip.foot", { size: formatSize(file.size) });
}

async function choose(f: File | null | undefined): Promise<void> {
  if (!f || busy) return;
  file = f;
  setStatus("");
  $("[data-empty]").hidden = true;
  $("[data-chosen]").hidden = false;
  $("[data-file-name]").textContent = f.name;
  $("[data-file-meta]").textContent = describe(f);
  // A sample of the encrypted form (fresh key, same format): the real bytes replace it when sending.
  const sample = await EnvelopeWriter.create({ mode: Mode.Link }, randomBytes(32), { files: [{ name: f.name, size: f.size, type: f.type }] });
  if (file === f) showSlip(sample.head);
}

function checked(name: string): number {
  return Number(document.querySelector<HTMLInputElement>(`input[name="${name}"]:checked`)!.value);
}

async function send(): Promise<void> {
  if (!file) fail("err.no_file");
  const f = file;
  if (f.size === 0) fail("err.empty_file");
  const usePassword = $<HTMLInputElement>("[data-use-password]").checked;
  const password = $<HTMLInputElement>("[data-password]").value;
  if (usePassword && [...password].length < MIN_PASSWORD) fail("err.password");

  setStatus(t("status.preparing", { name: f.name }));
  const fileKey = randomBytes(32);
  const params: WriterParams = { mode: Mode.Link };
  let ikm = linkKey(fileKey);
  if (usePassword) {
    params.mode = Mode.LinkPassword;
    params.argon = { ...DEFAULT_ARGON, salt: randomBytes(16) };
    ikm = await passwordKey(fileKey, password, params.argon);
  }
  const writer = await EnvelopeWriter.create(params, ikm, { files: [{ name: f.name, size: f.size, type: f.type || undefined }] });
  const total = envelopeSize(writer.head.length, f.size);
  if (total > MAX_ENVELOPE) fail("err.too_large");

  const upload = await createUpload(total, checked("expires"), checked("downloads"), await sha256(await downloadAuth(fileKey)));
  showSlip(writer.head);
  $("[data-envelope]").classList.add("is-sealed");
  await uploadEnvelope(upload, writer, f, total, (done) => {
    setStatus(t("status.uploading", { name: f.name, p: done }), "sealed");
    setProgress(done);
  });
  await retry(() => completeUpload(upload));

  remember({ id: upload.id, name: f.name, expiresAt: upload.expires_at, deleteToken: upload.delete_token });
  setStatus(t("status.sent", { name: f.name }), "sealed");
  setProgress(null);
  await flyAway($("[data-envelope]"));

  const link = `${location.origin}/d/${upload.id}#${toB64u(fileKey)}`;
  $("[data-envelope]").hidden = true;
  $("[data-choices]").hidden = true;
  $("[data-submit]").hidden = true;
  $("[data-receipt-done]").textContent = t("status.sent", { name: f.name });
  $("[data-link]").textContent = link;
  $("[data-receipt-terms]").textContent = t("receipt.terms", { date: formatDate(new Date(upload.expires_at)), n: checked("downloads") });
  $("[data-receipt-password]").hidden = !usePassword;
  $("[data-receipt]").hidden = false;
  $("[data-copy-link]").focus();
}

function explain(err: unknown): string {
  if (err instanceof UserError) return err.message;
  if (err instanceof ReadError) return t("err.read");
  if (err instanceof ApiError) {
    if (err.code === "network") return t("err.network");
    if (err.code === "too_large") return t("err.too_large");
    if (err.code === "too_many_requests") return t("err.too_many_requests");
    if (err.code === "storage_full") return t("err.storage_full");
    return t("err.generic", { code: err.code });
  }
  return t("err.generic", { code: (err as Error)?.name ?? "error" });
}

function reset(): void {
  file = null;
  resetEnvelope($("[data-envelope]"));
  $("[data-envelope]").hidden = false;
  $("[data-choices]").hidden = false;
  $("[data-submit]").hidden = false;
  $("[data-receipt]").hidden = true;
  $("[data-empty]").hidden = false;
  $("[data-chosen]").hidden = true;
  $<HTMLInputElement>("[data-file-input]").value = "";
  setStatus("");
  setProgress(null);
}

export function initSend(): void {
  const input = $<HTMLInputElement>("[data-file-input]");
  const envelope = $("[data-envelope]");
  document.querySelectorAll("[data-pick]").forEach((b) => b.addEventListener("click", () => input.click()));
  input.addEventListener("change", () => choose(input.files?.[0]));

  // Drop anywhere on the envelope; elsewhere on the page a dropped file must not open in the tab.
  addEventListener("dragover", (e) => e.preventDefault());
  addEventListener("drop", (e) => e.preventDefault());
  envelope.addEventListener("dragenter", () => envelope.classList.add("is-dragging"));
  envelope.addEventListener("dragleave", (e) => {
    if (!envelope.contains(e.relatedTarget as Node)) envelope.classList.remove("is-dragging");
  });
  envelope.addEventListener("drop", (e) => {
    envelope.classList.remove("is-dragging");
    const item = e.dataTransfer?.items?.[0];
    if (item && item.webkitGetAsEntry?.()?.isDirectory) return setStatus(t("err.folder"), "error");
    choose(e.dataTransfer?.files?.[0]);
  });

  $<HTMLInputElement>("[data-use-password]").addEventListener("change", (e) => {
    const field = $<HTMLInputElement>("[data-password]");
    field.hidden = !(e.target as HTMLInputElement).checked;
    if (!field.hidden) field.focus();
  });

  // Leaving the page mid-upload loses it: the browser asks first.
  addEventListener("beforeunload", (e) => {
    if (busy) e.preventDefault();
  });

  $<HTMLFormElement>("[data-send]").addEventListener("submit", async (e) => {
    e.preventDefault();
    if (busy) return;
    busy = true;
    const submit = $<HTMLButtonElement>("[data-submit]");
    submit.disabled = true;
    try {
      await send();
    } catch (err) {
      resetEnvelope(envelope);
      setProgress(null);
      setStatus(explain(err), "error");
    } finally {
      busy = false;
      submit.disabled = false;
    }
  });

  $("[data-copy-link]").addEventListener("click", async (e) => {
    const button = e.currentTarget as HTMLButtonElement;
    try {
      await navigator.clipboard.writeText($("[data-link]").textContent ?? "");
      button.textContent = t("receipt.copied");
      setTimeout(() => (button.textContent = t("receipt.copy")), 2000);
    } catch {
      getSelection()?.selectAllChildren($("[data-link]")); // clipboard blocked: select it for a manual copy
    }
  });
  $("[data-again]").addEventListener("click", reset);
}


