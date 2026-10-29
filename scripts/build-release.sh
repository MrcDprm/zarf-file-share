#!/bin/sh
# Builds every file of a GitHub release into dist/: the command line tool for six systems, the
# Windows installer, the server for Linux, and SHA256SUMS. File names carry no version, so links
# to releases/latest/download/<name> (the website, install.sh) keep working for every release.
# Usage, from the repository root:  sh scripts/build-release.sh 1.1.0
set -eu
v="${1:?usage: sh scripts/build-release.sh VERSION}"
rm -rf dist
mkdir -p dist

(cd web && npm ci --silent && npm run build --silent) # the pages the server embeds

for target in windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
  os="${target%/*}"
  arch="${target#*/}"
  name="zarf-$os-$arch"
  ext=""
  [ "$os" = windows ] && ext=".exe"
  mkdir -p "dist/$name"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$v" -o "dist/$name/zarf$ext" ./cmd/zarf
  cp LICENSE "dist/$name/"
  if [ "$os" = windows ]; then
    (cd dist && python -c "import shutil; shutil.make_archive('$name', 'zip', '.', '$name')")
  else
    # Built on Windows, the files carry no execute bit and the local user name; set
    # 0755 for zarf, 0644 for the rest and root as owner, so "./zarf" runs after tar xzf.
    (cd dist && python -c "
import tarfile
def fix(ti):
    ti.uid = ti.gid = 0
    ti.uname = ti.gname = 'root'
    ti.mode = 0o755 if ti.isdir() or ti.name.endswith('/zarf') else 0o644
    return ti
with tarfile.open('$name.tar.gz', 'w:gz') as t:
    t.add('$name', filter=fix)
")
  fi
  rm -rf "dist/$name"
done

for arch in amd64 arm64; do
  GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$v" -o "dist/zarf-server-linux-$arch" ./cmd/zarf-server
done

powershell -NoProfile -File installer/build.ps1 -Version "$v"
cp installer/Output/zarf-setup.exe dist/

# The usual "<sum>  <file>" form; sha256sum on Windows would mark every file with "*".
(cd dist && sha256sum zarf-* | sed 's/ \*/  /' > SHA256SUMS)
ls -1 dist
