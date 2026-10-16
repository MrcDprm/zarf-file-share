// Design preview only: the send screen's states (?state=file|sealed|fly) before the real logic exists.
export {}; // a module: its names must not clash with browser globals such as window.status

const root = document.documentElement;
const params = new URLSearchParams(location.search);
const theme = params.get("theme");
if (theme === "dark" || theme === "light") root.dataset.theme = theme;

const isDark = () => root.dataset.theme === "dark" || (root.dataset.theme !== "light" && matchMedia("(prefers-color-scheme: dark)").matches);
const paintTheme = () => root.classList.toggle("is-dark", isDark());
paintTheme();
document.querySelector("[data-theme-toggle]")!.addEventListener("click", () => {
  root.dataset.theme = isDark() ? "light" : "dark";
  paintTheme();
});

const win = document.querySelector<HTMLElement>("[data-window]")!;
const envelope = document.querySelector<HTMLElement>("[data-envelope]")!;
const status = document.querySelector<HTMLElement>("[data-status]")!;
const state = params.get("state");
const fileName = "Müşteri sözleşmesi, imzalı.pdf";

function showFile() {
  win.querySelector(".window__prompt")?.remove();
  win.querySelector(".window__hint")?.remove();
  const name = document.createElement("p");
  name.className = "window__name";
  name.textContent = fileName;
  const hint = document.createElement("p");
  hint.className = "window__hint";
  hint.textContent = "2,4 MB, PDF belgesi";
  win.prepend(name, hint);
}

function seal(done: number) {
  envelope.classList.add("is-sealed");
  status.className = "status status--sealed";
  status.textContent = `${fileName} şifrelendi. Yükleniyor: %${done}`;
}

const reducedMotion = matchMedia("(prefers-reduced-motion: reduce)").matches;
const wait = (ms: number) => new Promise((r) => setTimeout(r, reducedMotion ? 0 : ms));

/** The flap closes, the envelope shrinks, the pigeon comes in, picks it up and flies off. */
async function send() {
  const flight = document.querySelector<HTMLElement>("[data-flight]")!;
  const pigeon = document.querySelector<SVGSVGElement>("[data-pigeon]")!;
  seal(100);
  await wait(800);
  envelope.classList.add("is-closing");
  await wait(600);

  const box = envelope.getBoundingClientRect();
  const meetX = box.left + box.width / 2 - 70;
  const meetY = box.top + box.height / 2 - 70;
  if (reducedMotion) return done();
  flight.hidden = false;

  // The envelope folds into a small parcel at the meeting point while the pigeon comes in.
  const shrink = envelope.animate(
    [{ transform: "none" }, { transform: `scale(${34 / box.width}, ${22 / box.height})` }],
    { duration: 500, delay: 500, easing: "cubic-bezier(.5,0,.3,1)", fill: "forwards" },
  );
  const arrive = pigeon.animate(
    [
      { transform: `translate(-160px, ${meetY - 140}px) rotate(-6deg)` },
      { transform: `translate(${meetX}px, ${meetY}px) rotate(4deg)` },
    ],
    { duration: 1100, easing: "cubic-bezier(.2,.7,.3,1)", fill: "forwards" },
  );
  await Promise.all([shrink.finished, arrive.finished]);
  envelope.style.opacity = "0";
  pigeon.classList.add("has-parcel");
  await wait(150);
  const leave = pigeon.animate(
    [
      { transform: `translate(${meetX}px, ${meetY}px) rotate(4deg)` },
      { transform: `translate(${meetX + 140}px, ${meetY - 90}px) rotate(-10deg)`, offset: 0.35 },
      { transform: `translate(${innerWidth + 160}px, -180px) rotate(-14deg)` },
    ],
    { duration: 1400, easing: "cubic-bezier(.45,0,.75,.6)", fill: "forwards" },
  );
  await leave.finished;
  done();

  function done() {
    flight.hidden = true;
    envelope.hidden = true;
    document.querySelector<HTMLElement>(".choices")!.hidden = true;
    document.querySelector<HTMLElement>("[data-submit]")!.hidden = true;
    document.querySelector<HTMLElement>("[data-receipt]")!.hidden = false;
  }
}

// The slip shows the real first bytes of an envelope made by the page's own encryption code.
async function showServerView() {
  const { EnvelopeWriter, Mode } = await import("./lib/envelope.ts");
  const { toHex, randomBytes } = await import("./lib/bytes.ts");
  const w = await EnvelopeWriter.create({ mode: Mode.Link }, randomBytes(32), { files: [{ name: fileName, size: 2516582, type: "application/pdf" }] });
  const hex = toHex(w.head.subarray(0, 48)).match(/.{8}/g)!.join(" ");
  const you = document.querySelector<HTMLElement>("[data-you]")!;
  you.textContent = fileName;
  const size = document.createElement("span");
  size.className = "slip__size";
  size.textContent = "2,4 MB";
  you.append(document.createElement("br"), size);
  const bytes = document.querySelector<HTMLElement>("[data-bytes]")!;
  bytes.classList.add("is-live");
  bytes.textContent = `${hex} …`;
  document.querySelector("[data-slip-foot]")!.textContent = "Ad, tür ve içerik bu baytların içinde şifreli. Sunucu sadece yaklaşık boyutu görür: 2,4 MB.";
}

if (state === "file" || state === "sealed" || state === "fly") {
  showFile();
  showServerView();
}

// The three ways to send, as tabs.
function showMode(mode: string) {
  document.querySelectorAll<HTMLElement>("[data-panel]").forEach((p) => (p.hidden = p.dataset.panel !== mode));
  document.querySelectorAll<HTMLElement>("[data-mode]").forEach((m) => {
    if (m.dataset.mode === mode) m.setAttribute("aria-current", "page");
    else m.removeAttribute("aria-current");
  });
}
document.querySelectorAll<HTMLElement>("[data-mode], [data-mode-link]").forEach((el) =>
  el.addEventListener("click", (e) => {
    e.preventDefault();
    showMode(el.dataset.mode ?? el.dataset.modeLink!);
    scrollTo({ top: 0, behavior: reducedMotion ? "auto" : "smooth" });
  }),
);
const startMode = params.get("mode");
if (startMode) showMode(startMode);
if (state === "sealed") requestAnimationFrame(() => requestAnimationFrame(() => seal(42)));
if (state === "fly") setTimeout(send, 300);

document.querySelector<HTMLFormElement>("[data-send]")!.addEventListener("submit", (e) => {
  e.preventDefault();
  if (!win.querySelector(".window__name")) {
    showFile();
    showServerView();
  }
  send();
});
document.querySelector<HTMLInputElement>("[data-use-password]")!.addEventListener("change", (e) => {
  document.querySelector<HTMLInputElement>("[data-password]")!.hidden = !(e.target as HTMLInputElement).checked;
});
