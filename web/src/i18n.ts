// Turkish and English. Elements carry data-i18n="key" (text), data-i18n-placeholder, or
// data-i18n-aria; apply() fills them all, and t() serves text built in code.

export type Lang = "tr" | "en";

const tr = {
  "meta.title": "Zarf: dosyanı sadece alıcı açabilsin",
  "lang.code": "EN",
  "lang.switch": "Switch to English",
  "theme.toggle": "Temayı değiştir",
  "modes.label": "Gönderme yolu",
  "mode.link": "Link ile gönder",
  "mode.request": "Dosya iste",
  "mode.live": "Kodla gönder",

  "link.title": "Dosyayı sadece alıcının açabileceği şekilde gönder.",
  "drop.prompt": "Dosyayı buraya bırak",
  "drop.or": "ya da",
  "drop.pick": "bilgisayardan seç",
  "drop.limit": "En fazla 1024 MB.",
  "drop.change": "Başka dosya seç",
  "expires.legend": "Link ne kadar süre açık kalsın?",
  "expires.1h": "1 saat",
  "expires.1d": "1 gün",
  "expires.7d": "7 gün",
  "downloads.legend": "Kaç kez indirilebilsin?",
  "password.check": "Bir de şifreyle koru",
  "password.placeholder": "En az 8 karakter",
  "password.label": "Şifre",
  "send.button": "Şifrele ve yükle",
  "send.note": "Dosya bu cihazda şifrelenir. Anahtar linkin # işaretinden sonraki kısmında kalır; sunucu dosyayı göremez.",
  "status.preparing": "{name} şifreleniyor…",
  "status.uploading": "{name} şifrelendi. Yükleniyor: %{p}",
  "status.sent": "{name} yola çıktı.",

  "receipt.title": "Linki alıcıya ilet",
  "receipt.copy": "Kopyala",
  "receipt.copied": "Kopyalandı",
  "receipt.terms": "{date} tarihine kadar açık, {n} kez indirilebilir.",
  "receipt.password": "Alıcı şifreyi de bilmeli; onu linkle aynı yerden gönderme.",
  "receipt.again": "Başka bir dosya gönder",

  "err.no_file": "Önce bir dosya seç.",
  "err.folder": "Klasörler gönderilemez. Önce klasörü .zip dosyası yap.",
  "err.too_large": "Bu dosya 1024 MB'tan büyük. Daha küçük bir dosya seç.",
  "err.empty_file": "Bu dosya boş. İçinde bir şey olan bir dosya seç.",
  "err.password": "Şifre en az 8 karakter olmalı.",
  "err.network": "Sunucuya ulaşılamadı. Bağlantını kontrol edip tekrar dene.",
  "err.too_many_requests": "Bu adresten bir saatte çok fazla yükleme yapıldı. Biraz sonra tekrar dene.",
  "err.storage_full": "Sunucunun depolama alanı şu an dolu. Daha sonra tekrar dene.",
  "err.generic": "Yükleme tamamlanamadı ({code}). Tekrar dene.",
  "err.read": "Dosya okunamadı. Dosyanın hâlâ yerinde olduğunu kontrol et.",

  "request.title": "Birinden dosya iste; sadece sen açabil.",
  "request.lead": "Bir istek linki oluştur ve gönder. O linkten yüklenen her dosya senin anahtarınla şifrelenir; anahtar bu tarayıcıdan çıkmaz.",
  "request.label": "Ne istiyorsun?",
  "request.placeholder": "Örneğin: kimlik fotokopisi ve vergi levhası",
  "request.button": "İstek linki oluştur",
  "live.title": "Karşındakiyle aynı anda, kısa bir kodla gönder.",
  "live.lead": "Kodu sesli oku ya da yaz; alıcı kodu girince dosya doğrudan onun cihazına gider. Kodu bilmeyen biri en fazla bir kez tahmin edebilir; yanlış tahminde bağlantı kapanır.",
  "live.button": "Dosya seç ve kod al",
  "soon": "Bu gönderme yolu hazırlanıyor. Şimdilik link ile gönderebilirsin.",

  "slip.title": "Sunucunun gördüğü",
  "slip.you": "Sen",
  "slip.server": "Sunucu",
  "slip.you.empty": "Bir dosya seç",
  "slip.server.empty": "Dosya seçince, sunucuya giden şifreli hâlinin ilk baytları burada görünür.",
  "slip.foot.empty": "Dosya adı, türü ve içeriği şifreli kısımdadır; sunucu sadece yaklaşık boyutu görür.",
  "slip.foot": "Ad, tür ve içerik bu baytların içinde şifreli. Sunucu sadece yaklaşık boyutu görür: {size}.",

  "log.title": "Bu cihazdan gönderdiklerin",
  "log.empty": "Henüz bir şey göndermedin.",
  "log.foot": "Bu liste sadece bu tarayıcıda tutulur.",
  "log.open": "{date} tarihine kadar açık, {n} indirme hakkı kaldı",
  "log.gone": "Süresi doldu ya da indirildi, artık açılamaz",
  "log.checking": "Durumu soruluyor…",
  "log.delete": "Sil",
  "log.remove": "Kaldır",
  "log.confirm": "{name} silinsin mi? Link bir daha açılmaz.",

  "uses.title": "Ne zaman işine yarar",
  "uses.a.title": "Muhasebeci, müşterisinden kimlik fotokopisi istiyor",
  "uses.a.text": "E-postayla fotokopi istemek yerine bir istek linki gönderir. Müşteri dosyayı yükler; sadece muhasebecinin tarayıcısı açabilir, sunucuda okunur bir kopya kalmaz.",
  "uses.a.link": "Dosya iste'yi aç",
  "uses.b.title": "Avukat, imzalı sözleşmeyi müvekkiline gönderiyor",
  "uses.b.text": "Bir gün açık, bir kez indirilebilen, şifreyle korunan bir link oluşturur. Şifreyi telefonla söyler; link başkasının eline geçse bile tek başına açmaz.",
  "uses.b.link": "Link ile gönder'i aç",
  "kvkk.lead": "KVKK.",
  "kvkk.text": "Kişisel veri içeren belgelerde kanun uygun güvenlik önlemi ister. Zarf'ta dosya, adı ve türü sunucuya şifreli gider; sunucu bunları çözecek anahtara hiç sahip olmaz.",

  "cli.title": "Komut satırından da çalışır",
  "cli.text": "Tarayıcıda gönderilen bir dosya komut satırında, komut satırından gönderilen bir dosya tarayıcıda açılır. İkisi aynı şifreli biçimi kullanır.",
  "cli.copy": "Kopyala",
  "cli.download": "İndir:",
  "foot.text": "Zarf açık kaynaklıdır.",
  "foot.link": "Kaynak kodu GitHub'da.",
};

type Key = keyof typeof tr;

const en: Record<Key, string> = {
  "meta.title": "Zarf: files only the recipient can open",
  "lang.code": "TR",
  "lang.switch": "Türkçeye geç",
  "theme.toggle": "Change theme",
  "modes.label": "Ways to send",
  "mode.link": "Send a link",
  "mode.request": "Request files",
  "mode.live": "Send with a code",

  "link.title": "Send a file only the recipient can open.",
  "drop.prompt": "Drop the file here",
  "drop.or": "or",
  "drop.pick": "choose one",
  "drop.limit": "1024 MB at most.",
  "drop.change": "Choose another file",
  "expires.legend": "How long should the link work?",
  "expires.1h": "1 hour",
  "expires.1d": "1 day",
  "expires.7d": "7 days",
  "downloads.legend": "How many downloads?",
  "password.check": "Also protect it with a password",
  "password.placeholder": "At least 8 characters",
  "password.label": "Password",
  "send.button": "Encrypt and upload",
  "send.note": "The file is encrypted on this device. The key stays in the part of the link after #; the server cannot see the file.",
  "status.preparing": "Encrypting {name}…",
  "status.uploading": "{name} is encrypted. Uploading: {p}%",
  "status.sent": "{name} is on its way.",

  "receipt.title": "Send the link to the recipient",
  "receipt.copy": "Copy",
  "receipt.copied": "Copied",
  "receipt.terms": "Works until {date}, {n} download(s).",
  "receipt.password": "The recipient needs the password too; don't send it the same way as the link.",
  "receipt.again": "Send another file",

  "err.no_file": "Choose a file first.",
  "err.folder": "Folders can't be sent. Pack the folder into a .zip file first.",
  "err.too_large": "This file is over 1024 MB. Choose a smaller file.",
  "err.empty_file": "This file is empty. Choose a file with something in it.",
  "err.password": "The password needs at least 8 characters.",
  "err.network": "The server can't be reached. Check your connection and try again.",
  "err.too_many_requests": "Too many uploads from this address in the last hour. Try again a little later.",
  "err.storage_full": "The server's storage is full right now. Try again later.",
  "err.generic": "The upload didn't finish ({code}). Try again.",
  "err.read": "The file couldn't be read. Check that it is still there.",

  "request.title": "Request files that only you can open.",
  "request.lead": "Create a request link and send it. Every file uploaded through it is encrypted with your key; the key never leaves this browser.",
  "request.label": "What do you need?",
  "request.placeholder": "For example: ID copy and tax certificate",
  "request.button": "Create request link",
  "live.title": "Send to someone right now, with a short code.",
  "live.lead": "Read the code out or type it; when the recipient enters it, the file goes straight to their device. Someone without the code gets one guess at most; a wrong guess closes the connection.",
  "live.button": "Choose a file and get a code",
  "soon": "This way of sending is still being built. For now you can send a link.",

  "slip.title": "What the server sees",
  "slip.you": "You",
  "slip.server": "Server",
  "slip.you.empty": "Choose a file",
  "slip.server.empty": "Once you choose a file, the first bytes of its encrypted form, as sent to the server, show up here.",
  "slip.foot.empty": "The file's name, type and contents are in the encrypted part; the server only sees roughly how big it is.",
  "slip.foot": "Name, type and contents are encrypted inside these bytes. The server only sees roughly how big it is: {size}.",

  "log.title": "Sent from this device",
  "log.empty": "Nothing sent yet.",
  "log.foot": "This list is kept only in this browser.",
  "log.open": "Works until {date}, {n} download(s) left",
  "log.gone": "Expired or downloaded, can't be opened any more",
  "log.checking": "Checking…",
  "log.delete": "Delete",
  "log.remove": "Remove",
  "log.confirm": "Delete {name}? The link will stop working.",

  "uses.title": "When it helps",
  "uses.a.title": "An accountant needs a client's ID copy",
  "uses.a.text": "Instead of asking for it by e-mail, they send a request link. The client uploads the file; only the accountant's browser can open it, and no readable copy stays on the server.",
  "uses.a.link": "Open Request files",
  "uses.b.title": "A lawyer sends a signed contract to a client",
  "uses.b.text": "They create a link that works for one day, for one download, protected by a password. They give the password over the phone; even if the link leaks, it opens nothing on its own.",
  "uses.b.link": "Open Send a link",
  "kvkk.lead": "Personal data.",
  "kvkk.text": "Turkish data protection law (KVKK) asks for appropriate security for documents with personal data. With Zarf the file, its name and its type reach the server encrypted; the server never has the key to open them.",

  "cli.title": "It works from the command line too",
  "cli.text": "A file sent from the browser opens on the command line, and the other way round. Both use the same encrypted format.",
  "cli.copy": "Copy",
  "cli.download": "Download:",
  "foot.text": "Zarf is open source.",
  "foot.link": "Source code on GitHub.",
};

const dictionaries: Record<Lang, Record<Key, string>> = { tr, en };
const STORE = "zarf-lang";

export let lang: Lang = initialLang();

function initialLang(): Lang {
  try {
    const saved = localStorage.getItem(STORE);
    if (saved === "tr" || saved === "en") return saved;
  } catch {
    /* storage blocked */
  }
  return navigator.language.toLowerCase().startsWith("tr") ? "tr" : "en";
}

/** A text by key, with {name} placeholders filled in. */
export function t(key: Key, vars: Record<string, string | number> = {}): string {
  return dictionaries[lang][key].replace(/\{(\w+)\}/g, (_, v: string) => String(vars[v] ?? `{${v}}`));
}

export function apply(root: ParentNode = document): void {
  document.documentElement.lang = lang;
  document.title = t("meta.title");
  root.querySelectorAll<HTMLElement>("[data-i18n]").forEach((el) => (el.textContent = t(el.dataset.i18n as Key)));
  root.querySelectorAll<HTMLElement>("[data-i18n-placeholder]").forEach((el) => el.setAttribute("placeholder", t(el.dataset.i18nPlaceholder as Key)));
  root.querySelectorAll<HTMLElement>("[data-i18n-aria]").forEach((el) => el.setAttribute("aria-label", t(el.dataset.i18nAria as Key)));
}

export function setLang(next: Lang): void {
  lang = next;
  try {
    localStorage.setItem(STORE, next);
  } catch {
    /* storage blocked: it still changes for this page */
  }
  apply();
  document.dispatchEvent(new CustomEvent("zarf:lang"));
}

/** "2,4 MB" in Turkish, "2.4 MB" in English. */
export function formatSize(bytes: number): string {
  const units = ["B", "KB", "MB", "GB"];
  let v = bytes;
  let u = 0;
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024;
    u++;
  }
  return `${new Intl.NumberFormat(lang, { maximumFractionDigits: u === 0 ? 0 : 1 }).format(v)} ${units[u]}`;
}

export function formatDate(d: Date): string {
  return new Intl.DateTimeFormat(lang, { day: "numeric", month: "long", hour: "2-digit", minute: "2-digit" }).format(d);
}

export type { Key };
