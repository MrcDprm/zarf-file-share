// Request mode, the requester's side: create a request link, then open what arrives. The
// private key of each request stays in this browser (lib/requests.ts); the link carries only
// the public key, after "#", where the server never sees it.

import { toB64u } from "./lib/bytes.ts";
import { EnvelopeReader, Mode, ZarfError } from "./lib/envelope.ts";
import { generateX25519, requestKey } from "./lib/keys.ts";
import { ApiError, closeRequest, createRequest, downloadContent, requestInfo, startRequestDownload, type RequestFile, type RequestInfo } from "./lib/api.ts";
import { forgetRequest, listRequests, saveRequest, type MyRequest } from "./lib/requests.ts";
import { fingerprint } from "./lib/words.ts";
import { safeName } from "./lib/safename.ts";
import { saveFile } from "./lib/upload.ts";
import { formatDate, formatSize, lang, t, type Key } from "./i18n.ts";

const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;
const el = <K extends keyof HTMLElementTagNameMap>(tag: K, className = "", text = ""): HTMLElementTagNameMap[K] => {
  const e = document.createElement(tag);
  if (className) e.className = className;
  if (text) e.textContent = text; // textContent everywhere: labels and names are data, never markup
  return e;
};

const linkOf = (r: MyRequest) => `${location.origin}/r/${r.rid}#${toB64u(r.publicKey)}`;
const checked = (name: string) => Number(document.querySelector<HTMLInputElement>(`input[name="${name}"]:checked`)!.value);

let created: MyRequest | null = null;
// What each file row last said, so a refresh of the list keeps it; and how many files are
// being opened right now, so the list is not redrawn under them.
const fileStatus = new Map<string, { key: Key; vars?: Record<string, string | number>; error: boolean }>();
let opening = 0;

function showFileStatus(id: string, status: HTMLElement): void {
  const s = fileStatus.get(id);
  status.textContent = s ? t(s.key, s.vars) : "";
  status.className = "inbox__status" + (s?.error ? " status--error" : s?.key === "inbox.saved" ? " status--sealed" : "");
}

function setAskStatus(text: string, error = false): void {
  const s = $("[data-ask-status]");
  s.textContent = text;
  s.className = "status" + (error ? " status--error" : "");
}

async function showReceipt(r: MyRequest): Promise<void> {
  $("[data-ask]").hidden = true;
  $("[data-ask-link]").textContent = linkOf(r);
  $("[data-ask-terms]").textContent = t("request.receipt.terms", { date: formatDate(new Date(r.expiresAt)), n: checked("req-uploads") });
  $("[data-ask-fp]").textContent = await fingerprint(r.publicKey, lang);
  $("[data-ask-receipt]").hidden = false;
  $("[data-ask-copy]").focus();
}

async function create(): Promise<void> {
  const label = $<HTMLInputElement>("[data-ask-label]").value.trim();
  setAskStatus(t("request.creating"));
  const pair = await generateX25519(); // non-extractable: the private key cannot be read out
  const res = await createRequest(label, checked("req-expires"), checked("req-uploads"));
  const r: MyRequest = {
    rid: res.rid, ownerToken: res.owner_token, label, expiresAt: res.expires_at, createdAt: new Date().toISOString(),
    privateKey: pair.privateKey, publicKey: pair.publicKey, names: {},
  };
  try {
    await saveRequest(r);
  } catch {
    // Without storage the key would be lost with the tab, and nothing could ever be opened.
    await closeRequest(r.rid, r.ownerToken).catch(() => {});
    throw new Error("storage");
  }
  created = r;
  setAskStatus("");
  await showReceipt(r);
  await renderInbox();
}

function explain(err: unknown): string {
  if ((err as Error)?.message === "storage") return t("inbox.no_storage");
  if (err instanceof ApiError) {
    if (err.code === "network") return t("err.network");
    if (err.code === "too_many_requests") return t("err.too_many_requests");
    return t("err.generic", { code: err.code });
  }
  return t("err.generic", { code: (err as Error)?.name ?? "error" });
}

/** Downloads one upload with the owner token and opens it with this request's private key. */
async function openFile(r: MyRequest, f: RequestFile, status: HTMLElement, button: HTMLButtonElement, nameEl: HTMLElement): Promise<void> {
  button.disabled = true;
  opening++;
  const say = (key: Key, vars?: Record<string, string | number>, error = false) => {
    fileStatus.set(f.id, { key, vars, error });
    showFileStatus(f.id, status);
  };
  try {
    say("inbox.opening", { p: 0 });
    const start = await startRequestDownload(r.rid, f.id, r.ownerToken);
    const reader = new EnvelopeReader(async (h) => {
      if (h.mode !== Mode.Request || !h.ephPub) throw new ZarfError("header", "not a request envelope");
      return requestKey(r.privateKey, h.ephPub, h.ephPub, r.publicKey);
    });
    const parts: Uint8Array[] = [];
    let received = 0;
    for await (const chunk of downloadContent(f.id, start.ticket, start.size)) {
      received += chunk.length;
      say("inbox.opening", { p: Math.floor((received / start.size) * 100) });
      parts.push(...(await reader.push(chunk)));
    }
    parts.push(...(await reader.finish())); // nothing is saved before the last chunk verified
    const name = safeName(reader.metadata!.files[0]!.name);
    r.names[f.id] = name;
    await saveRequest(r).catch(() => {});
    nameEl.textContent = name;
    const url = saveFile(parts, name);
    setTimeout(() => URL.revokeObjectURL(url), 60_000);
    say("inbox.saved");
  } catch (err) {
    if (err instanceof ZarfError) say(err.code === "header" ? "recv.unsupported" : "recv.damaged", undefined, true);
    else if (err instanceof ApiError && err.status === 410) say("recv.gone", undefined, true);
    else say("err.network", undefined, true);
  } finally {
    button.disabled = false;
    opening--;
  }
}

function renderRequest(r: MyRequest, info: RequestInfo | "gone" | "error"): HTMLLIElement {
  const li = el("li", "inbox__item");
  li.append(el("p", "inbox__label", r.label || t("inbox.unnamed")));
  const meta = el("p", "inbox__meta");
  const actions = el("div", "inbox__actions");
  li.append(meta, actions);

  if (info === "gone" || info === "error") {
    meta.textContent = info === "gone" ? t("inbox.gone") : t("err.network");
    const remove = el("button", "pick", t("inbox.remove"));
    remove.type = "button";
    remove.addEventListener("click", async () => {
      await forgetRequest(r.rid).catch(() => {});
      renderInbox();
    });
    actions.append(remove);
    return li;
  }

  const files = info.files ?? [];
  const date = formatDate(new Date(r.expiresAt));
  meta.textContent = files.length ? t("inbox.meta", { n: files.length, date }) : t("inbox.meta.none", { date });

  const copy = el("button", "pick", t("inbox.copy"));
  copy.type = "button";
  copy.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(linkOf(r));
      copy.textContent = t("receipt.copied");
      setTimeout(() => (copy.textContent = t("inbox.copy")), 2000);
    } catch {
      prompt(t("inbox.copy"), linkOf(r)); // clipboard blocked: show it for a manual copy
    }
  });
  const close = el("button", "pick pick--danger", t("inbox.close"));
  close.type = "button";
  close.addEventListener("click", async () => {
    if (!confirm(t("inbox.close.confirm"))) return;
    try {
      await closeRequest(r.rid, r.ownerToken);
    } catch (err) {
      if (!(err instanceof ApiError && (err.status === 404 || err.status === 410))) return alert(explain(err));
    }
    await forgetRequest(r.rid).catch(() => {});
    renderInbox();
  });
  actions.append(copy, close);

  if (files.length) {
    const list = el("ol", "inbox__files");
    files.forEach((f, i) => {
      const row = el("li", "inbox__file");
      const name = el("span", "inbox__name", r.names[f.id] ?? t("inbox.file", { n: i + 1 }));
      const fmeta = el("span", "inbox__fmeta", t("inbox.file.meta", { size: formatSize(f.size), date: formatDate(new Date(f.created_at)) }));
      const open = el("button", "button button--small", t("inbox.open"));
      open.type = "button";
      const status = el("span", "inbox__status");
      status.setAttribute("role", "status");
      showFileStatus(f.id, status);
      open.addEventListener("click", () => openFile(r, f, status, open, name));
      row.append(name, fmeta, open, status);
      list.append(row);
    });
    li.append(list);
  }
  return li;
}

let rendering: Promise<void> | null = null;

/** Lists this browser's requests with what has arrived; runs one at a time. */
export function renderInbox(): Promise<void> {
  rendering ??= (async () => {
    try {
      const mine = await listRequests();
      const rows = await Promise.all(
        mine.map(async (r) => {
          try {
            return renderRequest(r, await requestInfo(r.rid, r.ownerToken));
          } catch (err) {
            const gone = err instanceof ApiError && (err.status === 404 || err.status === 410);
            return renderRequest(r, gone ? "gone" : "error");
          }
        }),
      );
      $("[data-inbox]").replaceChildren(...rows);
      $("[data-inbox]").hidden = rows.length === 0;
      $("[data-inbox-empty]").hidden = rows.length > 0;
    } finally {
      rendering = null;
    }
  })();
  return rendering;
}

export function initRequest(): void {
  $<HTMLFormElement>("[data-ask]").addEventListener("submit", async (e) => {
    e.preventDefault();
    const button = $<HTMLButtonElement>("[data-ask-submit]");
    button.disabled = true;
    try {
      await create();
    } catch (err) {
      setAskStatus(explain(err), true);
    } finally {
      button.disabled = false;
    }
  });
  $("[data-ask-copy]").addEventListener("click", async (e) => {
    const button = e.currentTarget as HTMLButtonElement;
    try {
      await navigator.clipboard.writeText($("[data-ask-link]").textContent ?? "");
      button.textContent = t("receipt.copied");
      setTimeout(() => (button.textContent = t("receipt.copy")), 2000);
    } catch {
      getSelection()?.selectAllChildren($("[data-ask-link]"));
    }
  });
  $("[data-ask-again]").addEventListener("click", () => {
    created = null;
    $<HTMLInputElement>("[data-ask-label]").value = "";
    $("[data-ask-receipt]").hidden = true;
    $("[data-ask]").hidden = false;
    $("[data-ask-label]").focus();
  });
  $("[data-inbox-refresh]").addEventListener("click", () => renderInbox());
  // The fingerprint words follow the language.
  document.addEventListener("zarf:lang", async () => {
    if (created) $("[data-ask-fp]").textContent = await fingerprint(created.publicKey, lang);
    renderInbox();
  });
  // While the panel is open, new arrivals show up on their own.
  setInterval(() => {
    if (!document.hidden && !$("[data-panel=request]").hidden && opening === 0) renderInbox();
  }, 20_000);
}
