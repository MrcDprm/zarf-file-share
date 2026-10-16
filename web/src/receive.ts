// The receiving page (/d/<id>#<key>): it downloads the envelope, opens it in this browser, and
// saves the file only after every chunk has verified. The key never reaches the server.

import "./styles.css";
import { fromB64u } from "./lib/bytes.ts";
import { EnvelopeReader, keyMatches, Mode, ZarfError, type Header } from "./lib/envelope.ts";
import { downloadAuth, linkKey, passwordKey } from "./lib/keys.ts";
import { ApiError, downloadContent, fileInfo, reportFile, startDownload, type FileInfo } from "./lib/api.ts";
import { safeName } from "./lib/safename.ts";
import { saveURL } from "./lib/upload.ts";
import { apply, formatDate, formatSize, lang, setLang, t, type Key } from "./i18n.ts";
import { initTheme } from "./theme.ts";

const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;

apply();
initTheme($("[data-theme-toggle]"));
$("[data-lang]").addEventListener("click", () => setLang(lang === "tr" ? "en" : "tr"));

// The key is read once, then taken out of the address bar so it does not linger in the
// history, in a screenshot, or in a bookmark.
const id = /^\/d\/([A-Za-z0-9_-]{22})\/?$/.exec(location.pathname)?.[1] ?? null;
const fileKey = fromB64u(location.hash.slice(1));
if (location.hash) history.replaceState(null, "", location.pathname);
// Pasting a corrected link into this tab changes only the part after #, which the browser
// does not treat as a new page: start over with the new key.
addEventListener("hashchange", () => location.reload());

let info: FileInfo | null = null;
let saved: { url: string; name: string } | null = null;
let statusKey: { key: Key; vars?: Record<string, string | number>; kind?: string } | null = null;

function setStatus(key: Key, vars?: Record<string, string | number>, kind = ""): void {
  statusKey = { key, vars, kind };
  const s = $("[data-status]");
  s.textContent = t(key, vars);
  s.className = "status" + (kind ? ` status--${kind}` : "");
}

function showFacts(): void {
  if (!info) return;
  $("[data-facts]").textContent = t("recv.facts", { size: formatSize(info.size), date: formatDate(new Date(info.expires_at)), n: info.downloads_left });
}

// Texts built in code follow a language switch too.
document.addEventListener("zarf:lang", () => {
  showFacts();
  if (statusKey) setStatus(statusKey.key, statusKey.vars, statusKey.kind);
});

function failWith(err: unknown): void {
  let key: Key = "err.network";
  if (err instanceof ZarfError) key = err.code === "wrong_key" ? "recv.wrong_key" : err.code === "header" ? "recv.unsupported" : "recv.damaged";
  else if (err instanceof ApiError) {
    if (err.status === 404) key = "recv.not_found";
    else if (err.status === 410) key = "recv.gone";
    else if (err.code === "wrong_key") key = "recv.wrong_key";
  }
  $("[data-progress]").hidden = true;
  setStatus(key, undefined, "error");
}

/** Asks for the password until it fits the header's commitment: a typo never costs a download. */
function askPassword(h: Header, key: Uint8Array): Promise<Uint8Array> {
  const form = $<HTMLFormElement>("[data-password-form]");
  const field = $<HTMLInputElement>("[data-password]");
  form.hidden = false;
  setStatus("recv.password.waiting");
  field.focus();
  return new Promise((resolve) => {
    const onSubmit = async (e: Event) => {
      e.preventDefault();
      if (!field.value) return;
      setStatus("recv.checking");
      const ikm = await passwordKey(key, field.value, h.argon!);
      if (await keyMatches(h, ikm)) {
        form.removeEventListener("submit", onSubmit);
        form.hidden = true;
        field.value = "";
        resolve(ikm);
      } else {
        setStatus("recv.password.wrong", undefined, "error");
        field.select();
      }
    };
    form.addEventListener("submit", onSubmit);
  });
}

function save(): void {
  if (saved) saveURL(saved.url, saved.name);
}

async function open(): Promise<void> {
  if (!id || !fileKey || !info) return;
  const button = $<HTMLButtonElement>("[data-download]");
  button.hidden = true;
  const key = fileKey;
  try {
    const start = await startDownload(id, await downloadAuth(key));
    info.downloads_left = Math.max(0, info.downloads_left - 1);
    showFacts();
    const reader = new EnvelopeReader(async (h) => {
      if (h.mode === Mode.Link) return linkKey(key);
      if (h.mode === Mode.LinkPassword) return askPassword(h, key);
      throw new ZarfError("header", "not a link envelope");
    });
    const parts: Uint8Array[] = [];
    let received = 0;
    const bar = $("[data-progress]");
    bar.hidden = false;
    for await (const chunk of downloadContent(id, start.ticket, start.size)) {
      received += chunk.length;
      const p = Math.floor((received / start.size) * 100);
      setStatus("recv.downloading", { p });
      bar.style.setProperty("--done", `${p}%`);
      parts.push(...(await reader.push(chunk)));
    }
    parts.push(...(await reader.finish())); // only now is the last chunk known to be genuine
    bar.hidden = true;

    const file = reader.metadata!.files[0]!;
    const name = safeName(file.name);
    // octet-stream: the browser saves the bytes and never renders them as a page.
    const blob = new Blob(parts as BlobPart[], { type: "application/octet-stream" });
    saved = { url: URL.createObjectURL(blob), name };
    $("[data-file-name]").textContent = name;
    $("[data-file-meta]").textContent = formatSize(file.size);
    const envelope = $("[data-envelope]");
    envelope.classList.remove("is-instant");
    envelope.classList.add("is-opening");
    envelope.classList.remove("is-sealed"); // the tint slides away: the envelope opens
    setStatus("recv.opened", undefined, "sealed");
    $("[data-save]").hidden = false;
    save();
  } catch (err) {
    failWith(err);
    button.hidden = false;
  }
}

async function load(): Promise<void> {
  if (!id) return setStatus("recv.bad_link", undefined, "error");
  if (!fileKey || fileKey.length !== 32) return setStatus("recv.no_key", undefined, "error");
  setStatus("recv.sealed");
  try {
    info = await fileInfo(id);
  } catch (err) {
    return failWith(err);
  }
  showFacts();
  $("[data-download]").hidden = false;
  $("[data-report-row]").hidden = false;
}

$("[data-download]").addEventListener("click", open);
$("[data-save]").addEventListener("click", save);
$("[data-report]").addEventListener("click", async () => {
  if (!id) return;
  const reason = prompt(t("recv.report.prompt"));
  if (reason === null) return;
  try {
    await reportFile(id, reason.slice(0, 500));
    setStatus("recv.report.done");
  } catch (err) {
    failWith(err);
  }
});
addEventListener("pagehide", () => saved && URL.revokeObjectURL(saved.url));

load();
