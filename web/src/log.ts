// "Sent from this device": a list kept only in this browser, so the sender can see what is
// still open and delete it early. It never holds a link key: only the id, the name, the
// expiry and the delete code, so even a script reading the storage could not open a file.

import { ApiError, deleteFile, fileInfo } from "./lib/api.ts";
import { formatDate, t } from "./i18n.ts";

const STORE = "zarf-sent";
const MAX = 30;

export interface Sent {
  id: string;
  name: string;
  expiresAt: string;
  deleteToken: string;
}

function read(): Sent[] {
  try {
    const list = JSON.parse(localStorage.getItem(STORE) ?? "[]") as unknown;
    if (!Array.isArray(list)) return [];
    // Data from storage is checked like any input: wrong shapes are dropped.
    return list.filter(
      (s): s is Sent =>
        typeof s === "object" && s !== null && typeof s.id === "string" && typeof s.name === "string" && typeof s.expiresAt === "string" && typeof s.deleteToken === "string",
    );
  } catch {
    return [];
  }
}

function write(list: Sent[]): void {
  try {
    localStorage.setItem(STORE, JSON.stringify(list.slice(0, MAX)));
  } catch {
    /* storage blocked or full: the list just isn't kept */
  }
}

export function remember(s: Sent): void {
  write([s, ...read().filter((x) => x.id !== s.id)]);
  render();
}

function forget(id: string): void {
  write(read().filter((x) => x.id !== id));
  render();
}

export function render(): void {
  const list = document.querySelector<HTMLOListElement>("[data-log]")!;
  const empty = document.querySelector<HTMLElement>("[data-log-empty]")!;
  const items = read();
  list.replaceChildren();
  list.hidden = items.length === 0;
  empty.hidden = items.length > 0;

  for (const s of items) {
    const li = document.createElement("li");
    const name = document.createElement("span");
    name.className = "log__name";
    name.textContent = s.name; // textContent: a file name is data, never markup
    const meta = document.createElement("span");
    meta.className = "log__meta";
    meta.textContent = t("log.checking");
    const action = document.createElement("button");
    action.type = "button";
    action.className = "log__delete";
    action.textContent = t("log.delete");
    li.append(name, meta, action);
    list.append(li);

    let gone = new Date(s.expiresAt) <= new Date();
    const showGone = () => {
      gone = true;
      meta.textContent = t("log.gone");
      action.textContent = t("log.remove");
    };
    if (gone) showGone();
    else
      fileInfo(s.id)
        .then((info) => (meta.textContent = t("log.open", { date: formatDate(new Date(info.expires_at)), n: info.downloads_left })))
        .catch((err) => (err instanceof ApiError && (err.status === 404 || err.status === 410) ? showGone() : (meta.textContent = "")));

    action.addEventListener("click", async () => {
      if (gone) return forget(s.id);
      if (!confirm(t("log.confirm", { name: s.name }))) return;
      action.disabled = true;
      try {
        await deleteFile(s.id, s.deleteToken);
        forget(s.id);
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) forget(s.id);
        else action.disabled = false;
      }
    });
  }
}
