// Light or dark: the system's choice until the person picks one, then theirs (kept in this browser).

const STORE = "zarf-theme";
const root = document.documentElement;
const systemDark = matchMedia("(prefers-color-scheme: dark)");

function saved(): string | null {
  try {
    return localStorage.getItem(STORE);
  } catch {
    return null;
  }
}

const isDark = () => root.dataset.theme === "dark" || (root.dataset.theme !== "light" && systemDark.matches);
const paint = () => root.classList.toggle("is-dark", isDark());

export function initTheme(toggle: HTMLElement): void {
  const choice = saved();
  if (choice === "dark" || choice === "light") root.dataset.theme = choice;
  paint();
  systemDark.addEventListener("change", paint);
  toggle.addEventListener("click", () => {
    const next = isDark() ? "light" : "dark";
    root.dataset.theme = next;
    try {
      localStorage.setItem(STORE, next);
    } catch {
      /* storage blocked: it still changes for this page */
    }
    paint();
  });
}
