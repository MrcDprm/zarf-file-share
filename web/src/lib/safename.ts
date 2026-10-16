// A file name from the sender is data, never a path: the same rules as the Go package
// internal/safename, so the browser and the command line save under the same name.

const FALLBACK = "zarf-dosya";
const MAX_BYTES = 200;
const RESERVED = new Set(["CON", "PRN", "AUX", "NUL", ...[1, 2, 3, 4, 5, 6, 7, 8, 9].flatMap((i) => [`COM${i}`, `LPT${i}`])]);

const utf8Length = (s: string) => new TextEncoder().encode(s).length;

export function safeName(name: string): string {
  let n = name.slice(Math.max(name.lastIndexOf("/"), name.lastIndexOf("\\")) + 1);
  n = n
    .replace(/[\p{Cc}\p{Cf}]/gu, "") // control and invisible format characters, direction overrides included
    .replace(/[<>:"|?*]/g, "_")
    .replace(/^[. ]+/, "")
    .replace(/[. ]+$/, "");
  if (!n) return FALLBACK;
  const dot = n.lastIndexOf(".");
  let ext = dot > 0 && n.length - dot <= 16 ? n.slice(dot) : "";
  let stem = ext ? n.slice(0, dot) : n;
  if (RESERVED.has(stem.toUpperCase())) stem = "_" + stem;
  const chars = [...stem]; // cut on whole characters, never inside one
  while (chars.length > 0 && utf8Length(chars.join("") + ext) > MAX_BYTES) chars.pop();
  stem = chars.join("") || FALLBACK;
  if (utf8Length(ext) > MAX_BYTES) ext = "";
  return stem + ext;
}
