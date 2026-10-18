# SEO: Zarf

Site: https://zarf.miracdeprem.com (Turkish by default, English with `?lang=en`)

Only the home page is meant for search. Receive (`/d/…`) and request (`/r/…`) pages are private links: they carry `noindex` and are closed in `robots.txt`.

## Keywords

| Type | Turkish | English |
|---|---|---|
| General | dosya gönderme, büyük dosya gönder, dosya paylaşımı | send large files, file sharing, file transfer |
| Specific | şifreli dosya gönderme, uçtan uca şifreli dosya paylaşımı, güvenli dosya gönderme, WeTransfer alternatifi | end-to-end encrypted file sharing, send files securely, encrypted file transfer, WeTransfer alternative |
| Long | ücretsiz şifreli büyük dosya gönderme, hesapsız dosya gönder, komut satırından dosya gönderme | free encrypted large file transfer, send files without an account, send files from the command line |
| Use cases | KVKK uyumlu dosya gönderme, müşteriden belge isteme | request files from a client, share sensitive documents |

## Where they are used

- `<title>` "Zarf: uçtan uca şifreli dosya gönderme" / "Zarf: end-to-end encrypted file sharing", and the meta description (both switched with the language)
- Canonical + `hreflang` (tr, en, x-default); `?lang=en` selects English before the saved choice
- Open Graph and Twitter card with `/og-image.png`
- JSON-LD `WebApplication` (free, web and command line, feature list, source code)
- `/sitemap.xml` and `/robots.txt`, served by the Go server from the built pages
- GitHub description and topics, README first paragraph

## Google Search Console

The `miracdeprem.com` domain property covers this subdomain. Under **Sitemaps**, submit `https://zarf.miracdeprem.com/sitemap.xml`.
