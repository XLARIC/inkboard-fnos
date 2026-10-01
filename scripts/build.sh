#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
command -v fnpack >/dev/null || { echo "请安装飞牛官方 fnpack 并加入 PATH" >&2; exit 1; }
version=$(sed -n 's/^const version = "\(.*\)"/\1/p' types.go)
[ -n "$version" ] || exit 1
mkdir -p dist
for arch in amd64 arm64; do
 CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags="-s -w" -o "dist/inkboard-linux-$arch" .
 release_dir="dist/release-$arch"
 mkdir -p "$release_dir/bin"
 cp "dist/inkboard-linux-$arch" "$release_dir/bin/inkboard"
 cp LICENSE THIRD_PARTY_NOTICES.md README.md "$release_dir/"
 cp -R licenses docs "$release_dir/"
 tar -czf "dist/inkboard-linux-$version-$arch.tar.gz" -C "$release_dir" .
done
if command -v fnpack >/dev/null;then
 for arch in amd64 arm64;do
  platform=x86;[ "$arch" = arm64 ] && platform=arm
  target="dist/fnos-$arch";mkdir -p "$target";cp -R packaging/fnos/. "$target/"
  mkdir -p "$target/app/bin" "$target/app/ui/images"
  cp "dist/inkboard-linux-$arch" "$target/app/bin/inkboard"
  cp LICENSE THIRD_PARTY_NOTICES.md "$target/app/"
  cp -R licenses "$target/app/"
  cp assets/icon64.png "$target/ICON.PNG";cp assets/icon.png "$target/ICON_256.PNG"
  cp assets/icon64.png "$target/app/ui/images/icon_64.png";cp assets/icon.png "$target/app/ui/images/icon_256.png"
  sed "s/platform = x86/platform = $platform/" packaging/fnos/manifest >"$target/manifest"
  chmod +x "$target"/cmd/* "$target"/app/bin/inkboard
  (cd "$target";fnpack build)
  mv "$target"/*.fpk "dist/inkboard-fnos-$version-$arch.fpk"
 done
fi
if command -v sha256sum >/dev/null;then (cd dist;sha256sum "inkboard-linux-$version-"*.tar.gz "inkboard-fnos-$version-"*.fpk >SHA256SUMS);else (cd dist;shasum -a 256 "inkboard-linux-$version-"*.tar.gz "inkboard-fnos-$version-"*.fpk >SHA256SUMS);fi
