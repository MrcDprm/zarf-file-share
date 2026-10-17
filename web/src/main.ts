// The send page: language, theme, the three ways to send, the form and the log.

import "./styles.css";
import { apply, lang, setLang, t } from "./i18n.ts";
import { initTheme } from "./theme.ts";
import { initSend } from "./send.ts";
import { render as renderLog } from "./log.ts";
import { initRequest, renderInbox } from "./request.ts";
import { initLive } from "./live-ui.ts";

const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;

apply();
initTheme($("[data-theme-toggle]"));
$("[data-lang]").addEventListener("click", () => setLang(lang === "tr" ? "en" : "tr"));
document.addEventListener("zarf:lang", renderLog);

// The three ways to send, as tabs. The address keeps the choice (#link, #iste, #kod).
const modeOfHash: Record<string, string> = { "#iste": "request", "#kod": "live" };
function showMode(mode: string): void {
  if (mode === "request") renderInbox();
  // The slip and the log describe link mode; the other two ways have their own lists.
  $(".side").hidden = mode !== "link";
  document.querySelectorAll<HTMLElement>("[data-panel]").forEach((p) => (p.hidden = p.dataset.panel !== mode));
  document.querySelectorAll<HTMLElement>("[data-mode]").forEach((m) => {
    if (m.dataset.mode === mode) m.setAttribute("aria-current", "page");
    else m.removeAttribute("aria-current");
  });
}
document.querySelectorAll<HTMLAnchorElement>("[data-mode], [data-mode-link]").forEach((a) =>
  a.addEventListener("click", (e) => {
    e.preventDefault();
    showMode(a.dataset.mode ?? a.dataset.modeLink!);
    history.replaceState(null, "", a.getAttribute("href"));
    $("#gonder").scrollIntoView({ behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
  }),
);
showMode(modeOfHash[location.hash] ?? "link");
// A link straight to a way of sending (#iste, #kod) opens at the panel, not at the top.
if (location.hash in modeOfHash) $("#gonder").scrollIntoView();
// Back and forward, or a hand-typed #kod, switch the tab too.
// The page's own anchors (#nasil, #komut) leave the tab as it is.
addEventListener("hashchange", () => {
  if (location.hash in modeOfHash || location.hash === "#link") showMode(modeOfHash[location.hash] ?? "link");
});


// The installer box: one tab per system, opening on the visitor's own.
const osTabs = [...document.querySelectorAll<HTMLButtonElement>("[data-os-tab]")];
function showOS(tab: HTMLButtonElement, focus = false): void {
  for (const t of osTabs) {
    const on = t === tab;
    t.setAttribute("aria-selected", String(on));
    t.tabIndex = on ? 0 : -1;
    document.querySelector<HTMLElement>(`[data-os-panel="${t.dataset.osTab}"]`)!.hidden = !on;
  }
  if (focus) tab.focus();
}
osTabs.forEach((tab, i) => {
  tab.addEventListener("click", () => showOS(tab));
  tab.addEventListener("keydown", (e) => {
    const step = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    showOS(osTabs[(i + step + osTabs.length) % osTabs.length]!, true);
  });
});
const platform = navigator.userAgent;
const mine = /Mac OS X|Macintosh/.test(platform) && !/iPhone|iPad/.test(platform) ? "mac" : /Linux|X11/.test(platform) && !/Android/.test(platform) ? "linux" : "windows";
showOS(osTabs.find((t) => t.dataset.osTab === mine) ?? osTabs[0]!);
document.querySelectorAll<HTMLButtonElement>("[data-install-copy]").forEach((button) =>
  button.addEventListener("click", async () => {
    const code = button.parentElement!.querySelector("code")!;
    try {
      await navigator.clipboard.writeText(code.textContent ?? "");
      button.textContent = t("receipt.copied");
      setTimeout(() => (button.textContent = t("cli.copy")), 2000);
    } catch {
      getSelection()?.selectAllChildren(code); // clipboard blocked: select it for a manual copy
    }
  }),
);

// The command reference: one tab per way of sending, with arrow keys between tabs.
const cmdTabs = [...document.querySelectorAll<HTMLButtonElement>("[data-cmd-tab]")];
function showCommands(tab: HTMLButtonElement, focus = false): void {
  for (const t of cmdTabs) {
    const on = t === tab;
    t.setAttribute("aria-selected", String(on));
    t.tabIndex = on ? 0 : -1;
    document.querySelector<HTMLElement>(`[data-cmd-panel="${t.dataset.cmdTab}"]`)!.hidden = !on;
  }
  if (focus) tab.focus();
}
cmdTabs.forEach((tab, i) => {
  tab.addEventListener("click", () => showCommands(tab));
  tab.addEventListener("keydown", (e) => {
    const step = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    showCommands(cmdTabs[(i + step + cmdTabs.length) % cmdTabs.length]!, true);
  });
});

initSend();
initRequest();
initLive();
renderLog();
