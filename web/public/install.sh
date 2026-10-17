#!/bin/sh
# Installs the zarf command line tool on macOS or Linux, for the current user.
#   curl -fsSL https://zarf.miracdeprem.com/install.sh | sh
# It downloads the latest release from GitHub, checks its SHA-256, and puts zarf in
# ~/.local/bin (or $ZARF_INSTALL_DIR). No administrator rights are needed.
set -eu

repo="MrcDprm/zarf-file-share"
dir="${ZARF_INSTALL_DIR:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
fail() { say "zarf: $*" >&2; exit 1; }

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "this installer is for macOS and Linux; on Windows use zarf-setup.exe from https://github.com/$repo/releases/latest" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) fail "no build for this processor ($(uname -m))" ;;
esac

if command -v curl >/dev/null 2>&1; then
  get() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  get() { wget -q -O "$2" "$1"; }
else
  fail "curl or wget is needed"
fi
if command -v sha256sum >/dev/null 2>&1; then
  sum() { sha256sum "$1" | cut -d' ' -f1; }
else
  sum() { shasum -a 256 "$1" | cut -d' ' -f1; }
fi

name="zarf-$os-$arch"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
base="https://github.com/$repo/releases/latest/download"
say "Downloading $name ..."
get "$base/$name.tar.gz" "$tmp/$name.tar.gz" || fail "download failed"
get "$base/SHA256SUMS" "$tmp/SHA256SUMS" || fail "download failed"

want="$(grep " $name.tar.gz\$" "$tmp/SHA256SUMS" | cut -d' ' -f1)"
[ -n "$want" ] || fail "$name.tar.gz is not in SHA256SUMS"
[ "$(sum "$tmp/$name.tar.gz")" = "$want" ] || fail "checksum mismatch: the download is damaged or was changed"

tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
mkdir -p "$dir"
install -m 755 "$tmp/$name/zarf" "$dir/zarf" 2>/dev/null || { cp "$tmp/$name/zarf" "$dir/zarf" && chmod 755 "$dir/zarf"; }

say ""
say "zarf $("$dir/zarf" version | cut -d' ' -f2) is installed in $dir"
case ":$PATH:" in
  *":$dir:"*)
    say "Run it from any terminal:  zarf"
    say "Herhangi bir terminalde çalıştır:  zarf"
    ;;
  *)
    case "${SHELL:-}" in
      */zsh) rc="$HOME/.zshrc" ;;
      */bash) rc="$HOME/.bashrc" ;;
      *) rc="$HOME/.profile" ;;
    esac
    say "$dir is not on your PATH yet. Add it once, then open a new terminal:"
    say "$dir henüz PATH'te değil. Bir kez ekle, sonra yeni bir terminal aç:"
    say ""
    say "  echo 'export PATH=\"$dir:\$PATH\"' >> $rc"
    say ""
    say "Until then / O zamana kadar:  $dir/zarf"
    ;;
esac
