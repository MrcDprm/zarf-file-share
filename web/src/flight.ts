// The one moment of motion: the flap closes and a carrier pigeon takes the envelope away.
// With reduced motion the page skips straight to the end.

const reducedMotion = () => matchMedia("(prefers-reduced-motion: reduce)").matches;
const wait = (ms: number) => new Promise((r) => setTimeout(r, reducedMotion() ? 0 : ms));

export async function flyAway(envelope: HTMLElement): Promise<void> {
  const flight = document.querySelector<HTMLElement>("[data-flight]")!;
  const pigeon = document.querySelector<SVGSVGElement>("[data-pigeon]")!;
  envelope.classList.add("is-closing");
  await wait(600);
  if (reducedMotion()) return;

  const box = envelope.getBoundingClientRect();
  const meetX = box.left + box.width / 2 - 70;
  const meetY = box.top + box.height / 2 - 70;
  flight.hidden = false;
  pigeon.classList.remove("has-parcel");

  // The envelope folds into a small parcel while the pigeon comes in to fetch it.
  const shrink = envelope.animate([{ transform: "none" }, { transform: `scale(${34 / box.width}, ${22 / box.height})` }], {
    duration: 500,
    delay: 500,
    easing: "cubic-bezier(.5,0,.3,1)",
    fill: "forwards",
  });
  const arrive = pigeon.animate(
    [{ transform: `translate(-160px, ${meetY - 140}px) rotate(-6deg)` }, { transform: `translate(${meetX}px, ${meetY}px) rotate(4deg)` }],
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
  flight.hidden = true;
  shrink.cancel();
  leave.cancel();
  arrive.cancel();
}

/** Puts the envelope back for the next file. */
export function resetEnvelope(envelope: HTMLElement): void {
  envelope.classList.remove("is-closing", "is-sealed");
  envelope.style.opacity = "";
  envelope.getAnimations().forEach((a) => a.cancel());
}
