# Zarf

[English](README.md) | **Türkçe**

Uçtan uca şifreli dosya paylaşımı. Dosyalar cihazından çıkmadan önce şifrelenir; sunucu yalnızca okuyamadığı baytları saklar ve iletir. Tarayıcıda ve komut satırında (`zarf`) çalışır.

> **Durum:** geliştiriliyor. Bu README proje planıdır; ilk sürüm çıkınca tamamlanacak.

## Neden

Muhasebeciler, avukatlar ve İK ekipleri her gün kimlik fotokopisi, sözleşme ve sağlık raporu gibi belgeleri e-postayla gönderiyor. Sıradan dosya paylaşım servisleri sakladıkları dosyaları okuyabilir. Zarf sunucunun okuyamayacağı şekilde tasarlandı: anahtar sunucuya hiç ulaşmaz.

## Planlanan özellikler (v1.0)

- **Link ile gönder:** Dosya tarayıcıda ya da komut satırında şifrelenip yüklenir. Anahtar linkin `#` sonrasındaki kısmında durur; tarayıcılar bu kısmı sunucuya hiç göndermez.
  - Süre (1 saat, 1 gün, 7 gün) ve indirme sınırı (1, 5, 10).
  - İsteğe bağlı şifre: link sızsa bile tek başına dosyayı açamaz.
  - Gönderen dosyayı istediği an silebilir.
  - Dosya adları, boyutları ve türleri de şifrelenir.
- **Dosya iste:** Senin açık anahtarını taşıyan bir istek linki oluşturursun. Bu linkten yükleyen kişi dosyayı o anahtarla şifreler; dosyaları sadece sen açabilirsin.
- **Kodla canlı gönder:** İki taraf da çevrimiçiyken gönderen kısa bir kod okur (`7-elma-kule`). Parola doğrulamalı anahtar değişimi (PAKE) bu kodu güçlü bir ortak anahtara çevirir; aktarım doğrudan cihazdan cihaza (tarayıcıda WebRTC) yapılır, doğrudan bağlantı kurulamazsa sunucunun relay'i üzerinden geçer.
- **Tarayıcı ve komut satırı birlikte çalışır:** Birinden gönderilen dosya diğeriyle alınabilir.
- **Devam ettirilebilir yükleme:** Dosya şifreli parçalar hâlinde gider; büyük dosyaların belleğe sığması gerekmez.
- **Kendi sunucunda çalıştırma:** Web arayüzü içine gömülü tek bir Go binary'si.
- Türkçe ve İngilizce, açık ve koyu tema.

## Güvenlik tasarımı

- Yalnızca standart, iyi incelenmiş yapı taşlarından kurulan kendi protokolümüz: parçalı STREAM yapısında AES-256-GCM, HKDF-SHA256, X25519, Argon2id ve ristretto255 üzerinde CPace.
- Protokol ve tehdit modeli koddan önce yazılır ([docs/PROTOCOL.md](docs/PROTOCOL.md), [docs/THREAT-MODEL.md](docs/THREAT-MODEL.md)); kod bunlara göre yazılır.
- Ortak test vektörleri: Go ve TypeScript uygulamaları aynı baytları üretmek ve kabul etmek zorunda.
- Bilinen saldırılar için testler: yer değiştirmiş ya da kesilmiş parçalar, değiştirilmiş başlık, nonce tekrarı, yanlış anahtar.
- Web tabanlı şifrelemenin bilinen sınırı (ele geçirilmiş bir sunucu kötü niyetli JavaScript sunabilir) açıkça yazılır; önlemler sıkı CSP ve komut satırı aracıdır.

## Kullanılan teknolojiler

- **Sunucu ve komut satırı:** Go (standart kütüphane `net/http`, `crypto`), WebSocket relay, SQLite
- **Web arayüzü:** TypeScript, Vite, Web Crypto API, WebRTC (framework yok)
- **Barındırma:** Oracle Cloud (Always Free VM), Caddy (HTTPS)

## Gelecek planları

- Dosya indirildiğinde gönderene e-posta bildirimi
- Linkler ve canlı gönderim kodları için QR kod
- Ekipler ve hesaplar
