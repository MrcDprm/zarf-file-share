// The send page: language, theme, the three ways to send, the form and the log.

import "./styles.css";
import { apply, lang, setLang } from "./i18n.ts";
import { initTheme } from "./theme.ts";
import { initSend } from "./send.ts";
import { render as renderLog } from "./log.ts";

const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;

apply();
initTheme($("[data-theme-toggle]"));
$("[data-lang]").addEventListener("click", () => setLang(lang === "tr" ? "en" : "tr"));
document.addEventListener("zarf:lang", renderLog);

// The three ways to send, as tabs. The address keeps the choice (#link, #iste, #kod).
const modeOfHash: Record<string, string> = { "#iste": "request", "#kod": "live" };
function showMode(mode: string): void {
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
    scrollTo({ top: 0, behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
  }),
);
showMode(modeOfHash[location.hash] ?? "link");

// Request files and send with a code are being built; their buttons say so.
document.querySelectorAll<HTMLElement>("[data-soon]").forEach((b) =>
  b.addEventListener("click", () => ((b.nextElementSibling as HTMLElement).hidden = false)),
);

$("[data-copy-command]").addEventListener("click", (e) => {
  navigator.clipboard?.writeText($("[data-command]").textContent ?? "").catch(() => {});
  (e.currentTarget as HTMLElement).blur();
});

initSend();
renderLog();
