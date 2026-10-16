// The word lists of live-mode codes and request fingerprints (PROTOCOL.md sections 4 and 5.1).
// Word k of the Turkish list means word k of the English one, so a code can be read out in
// either language. The Turkish words avoid ç, ğ, ı, ö, ş and ü: anyone can type them.

const PAIRS: readonly (readonly [string, string])[] = [
  ["elma", "apple"], ["armut", "pear"], ["kiraz", "cherry"], ["limon", "lemon"],
  ["muz", "banana"], ["erik", "plum"], ["incir", "fig"], ["portakal", "orange"],
  ["kavun", "melon"], ["ceviz", "walnut"], ["badem", "almond"], ["zeytin", "olive"],
  ["domates", "tomato"], ["biber", "pepper"], ["patates", "potato"], ["lahana", "cabbage"],
  ["marul", "lettuce"], ["fasulye", "bean"], ["mantar", "mushroom"], ["ekmek", "bread"],
  ["peynir", "cheese"], ["bal", "honey"], ["tuz", "salt"], ["kahve", "coffee"],
  ["su", "water"], ["yumurta", "egg"], ["simit", "bagel"], ["tavuk", "chicken"],
  ["kuzu", "lamb"], ["kedi", "cat"], ["at", "horse"], ["inek", "cow"],
  ["koyun", "sheep"], ["aslan", "lion"], ["kaplan", "tiger"], ["fil", "elephant"],
  ["zebra", "zebra"], ["maymun", "monkey"], ["tilki", "fox"], ["kurt", "wolf"],
  ["geyik", "deer"], ["sincap", "squirrel"], ["kartal", "eagle"], ["karga", "crow"],
  ["leylek", "stork"], ["kaz", "goose"], ["yunus", "dolphin"], ["balina", "whale"],
  ["ahtapot", "octopus"], ["kelebek", "butterfly"], ["timsah", "crocodile"], ["penguen", "penguin"],
  ["deve", "camel"], ["panda", "panda"], ["kanguru", "kangaroo"], ["horoz", "rooster"],
  ["deniz", "sea"], ["nehir", "river"], ["dere", "stream"], ["orman", "forest"],
  ["tepe", "hill"], ["vadi", "valley"], ["ada", "island"], ["kum", "sand"],
  ["kaya", "rock"], ["toprak", "soil"], ["bulut", "cloud"], ["kar", "snow"],
  ["ay", "moon"], ["gezegen", "planet"], ["sabah", "morning"], ["gece", "night"],
  ["yaz", "summer"], ["bahar", "spring"], ["sonbahar", "autumn"], ["buz", "ice"],
  ["duman", "smoke"], ["demir", "iron"], ["elmas", "diamond"], ["inci", "pearl"],
  ["kristal", "crystal"], ["cam", "glass"], ["kalem", "pen"], ["kitap", "book"],
  ["defter", "notebook"], ["masa", "table"], ["sandalye", "chair"], ["pencere", "window"],
  ["duvar", "wall"], ["merdiven", "ladder"], ["anahtar", "key"], ["kilit", "lock"],
  ["lamba", "lamp"], ["mum", "candle"], ["saat", "clock"], ["ayna", "mirror"],
  ["yatak", "bed"], ["battaniye", "blanket"], ["perde", "curtain"], ["dolap", "closet"],
  ["kutu", "box"], ["sepet", "basket"], ["valiz", "suitcase"], ["bardak", "mug"],
  ["tabak", "plate"], ["tava", "pan"], ["tencere", "pot"], ["kova", "bucket"],
  ["makas", "scissors"], ["ip", "rope"], ["kemer", "belt"], ["etek", "skirt"],
  ["ceket", "jacket"], ["palto", "coat"], ["eldiven", "glove"], ["terlik", "slipper"],
  ["bere", "beanie"], ["yelek", "vest"], ["kolye", "necklace"], ["bilezik", "bracelet"],
  ["araba", "car"], ["tren", "train"], ["gemi", "ship"], ["tekne", "boat"],
  ["bisiklet", "bicycle"], ["roket", "rocket"], ["kamyon", "truck"], ["taksi", "taxi"],
  ["motor", "motor"], ["tekerlek", "wheel"], ["yol", "road"], ["liman", "harbor"],
  ["istasyon", "station"], ["sokak", "street"], ["meydan", "plaza"], ["park", "park"],
  ["tarla", "field"], ["kale", "castle"], ["kule", "tower"], ["saray", "palace"],
  ["cami", "mosque"], ["okul", "school"], ["hastane", "hospital"], ["market", "market"],
  ["banka", "bank"], ["otel", "hotel"], ["sinema", "cinema"], ["tiyatro", "theater"],
  ["fabrika", "factory"], ["ev", "home"], ["oda", "room"], ["mutfak", "kitchen"],
  ["banyo", "bathroom"], ["balkon", "balcony"], ["bayrak", "flag"], ["harita", "map"],
  ["pusula", "compass"], ["kamera", "camera"], ["radyo", "radio"], ["telefon", "phone"],
  ["piyano", "piano"], ["gitar", "guitar"], ["davul", "drum"], ["keman", "violin"],
  ["zil", "bell"], ["melodi", "melody"], ["resim", "painting"], ["heykel", "statue"],
  ["boya", "paint"], ["tuval", "canvas"], ["oyun", "game"], ["top", "ball"],
  ["kart", "card"], ["zar", "dice"], ["bilmece", "riddle"], ["masal", "tale"],
  ["hikaye", "story"], ["mektup", "letter"], ["zarf", "envelope"], ["pul", "stamp"],
  ["posta", "mail"], ["haber", "news"], ["gazete", "newspaper"], ["dergi", "magazine"],
  ["sayfa", "page"], ["nokta", "dot"], ["daire", "circle"], ["kare", "square"],
  ["kalp", "heart"], ["ok", "arrow"], ["kalkan", "shield"], ["yay", "bow"],
  ["hazine", "treasure"], ["para", "money"], ["fener", "lantern"], ["kamp", "camp"],
  ["vida", "screw"], ["tahta", "plank"], ["beton", "concrete"], ["kablo", "cable"],
  ["pil", "battery"], ["kanal", "canal"], ["baraj", "dam"], ["kuyu", "well"],
  ["havuz", "pool"], ["volkan", "volcano"], ["buzul", "glacier"], ["vaha", "oasis"],
  ["kanyon", "canyon"], ["yaprak", "leaf"], ["dal", "branch"], ["tohum", "seed"],
  ["lale", "tulip"], ["papatya", "daisy"], ["zambak", "lily"], ["yosun", "moss"],
  ["bambu", "bamboo"], ["palmiye", "palm"], ["kavak", "poplar"], ["sedir", "cedar"],
  ["renk", "color"], ["mavi", "blue"], ["mor", "purple"], ["pembe", "pink"],
  ["lacivert", "navy"], ["beyaz", "white"], ["siyah", "black"], ["gri", "gray"],
  ["bordo", "maroon"], ["bej", "beige"], ["ritim", "rhythm"], ["dans", "dance"],
  ["bayram", "holiday"], ["parti", "party"], ["hediye", "gift"], ["pasta", "cake"],
  ["kurabiye", "cookie"], ["pizza", "pizza"], ["salata", "salad"], ["sos", "sauce"],
  ["limonata", "lemonade"], ["robot", "robot"], ["sihir", "magic"], ["hayal", "dream"],
  ["umut", "hope"], ["cesaret", "courage"], ["huzur", "peace"], ["dost", "friend"],
  ["kahraman", "hero"], ["korsan", "pirate"], ["kaptan", "captain"], ["pilot", "pilot"],
  ["doktor", "doctor"], ["terzi", "tailor"], ["ressam", "artist"], ["bekci", "guard"],
];

export const WORDS_TR: readonly string[] = PAIRS.map((p) => p[0]);
export const WORDS_EN: readonly string[] = PAIRS.map((p) => p[1]);

const positionOf = new Map<string, number>();
PAIRS.forEach(([tr, en], i) => {
  positionOf.set(tr, i);
  positionOf.set(en, i);
});

export interface Code {
  channel: number;
  positions: [number, number];
}

export const wordsOf = (lang: "tr" | "en") => (lang === "tr" ? WORDS_TR : WORDS_EN);

export const formatCode = (c: Code, lang: "tr" | "en"): string => `${c.channel}-${wordsOf(lang)[c.positions[0]]}-${wordsOf(lang)[c.positions[1]]}`;

/** Why a typed code was not understood, so the page can say what to fix. */
export type CodeProblem = "shape" | "channel" | "word";

/**
 * Reads a code as people type it: any case, and dashes, spaces or dots between the parts.
 * Each word may be Turkish or English.
 */
export function parseCode(text: string): Code | { problem: CodeProblem; word?: string } {
  const parts = text
    .trim()
    .toLowerCase()
    .split(/[\s.\-_]+/)
    .filter(Boolean);
  if (parts.length !== 3) return { problem: "shape" };
  const [num, a, b] = parts as [string, string, string];
  if (!/^\d{1,6}$/.test(num) || Number(num) < 1) return { problem: "channel" };
  const pa = positionOf.get(a);
  const pb = positionOf.get(b);
  if (pa === undefined) return { problem: "word", word: a };
  if (pb === undefined) return { problem: "word", word: b };
  return { channel: Number(num), positions: [pa, pb] };
}

/** Five words from the first five bytes of SHA-256(public key): the request fingerprint. */
export async function fingerprint(publicKey: Uint8Array, lang: "tr" | "en"): Promise<string> {
  const hash = new Uint8Array(await crypto.subtle.digest("SHA-256", publicKey as BufferSource));
  return [...hash.subarray(0, 5)].map((b) => wordsOf(lang)[b]).join(" ");
}
