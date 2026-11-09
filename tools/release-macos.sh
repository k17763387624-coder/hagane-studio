#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: tools/release-macos.sh --version VERSION [--arch arm64|amd64|universal|all]
                              [--skip-frontend] [--skip-checks]

Build native macOS application(s), the CLI and portable ZIPs in dist/release/VERSION.
The default architecture is the current Mac. Requires macOS 13+, Xcode Command Line
Tools, Go 1.25+, Node.js 20+ and Git with initialized submodules. A corresponding
source ZIP is always included. Output directories must not already exist.
USAGE
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
version=''
architecture="$(uname -m)"
skip_frontend=false
skip_checks=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version|--arch)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      if [[ "$1" == '--version' ]]; then version="$2"; else architecture="$2"; fi
      shift 2 ;;
    --skip-frontend) skip_frontend=true; shift ;;
    --skip-checks) skip_checks=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) printf 'Unknown argument: %s\n' "$1" >&2; usage >&2; exit 2 ;;
  esac
done
[[ -n "$version" && "$version" != *[!a-zA-Z0-9._-]* && "$version" != [._-]* ]] || { printf 'A safe --version is required.\n' >&2; exit 2; }
[[ "$(uname -s)" == 'Darwin' ]] || { printf 'Build on macOS with the Apple SDK.\n' >&2; exit 1; }
case "$architecture" in
  x86_64) architecture=amd64 ;;
  arm64|amd64|universal|all) ;;
  *) printf 'Unsupported architecture: %s\n' "$architecture" >&2; exit 2 ;;
esac
for command in go git xcrun codesign ditto plutil shasum; do
  command -v "$command" >/dev/null || { printf 'Missing build tool: %s\n' "$command" >&2; exit 1; }
done
clang="$(xcrun --sdk macosx --find clang)"
sdk="$(xcrun --sdk macosx --show-sdk-path)"
output="$repo_root/dist/release/$version"
[[ ! -e "$output" ]] || { printf 'Output already exists; use a new version: %s\n' "$output" >&2; exit 1; }
[[ -f "$repo_root/LICENSE" && -f "$repo_root/third_party/cwtools-hoi4-config/Config/modifiers.cwt" ]] || { printf 'License or CWTools submodule missing. Run git submodule update --init --recursive.\n' >&2; exit 1; }
cd "$repo_root"
if ! "$skip_frontend"; then
  npm --prefix web ci
  npm --prefix web run build
fi
[[ -f web/dist/index.html ]] || { printf 'Build the web frontend first.\n' >&2; exit 1; }
if ! "$skip_checks"; then
  go test -vet=off ./...
  npm --prefix web run typecheck
  npm --prefix web run lint
  npm --prefix web test -- --run
fi

stage="$(mktemp -d "${TMPDIR:-/tmp}/hagane-macos.XXXXXX")"
trap 'rm -rf "$stage"' EXIT
release="$stage/release"
mkdir -p "$release"
if [[ "$architecture" == all ]]; then targets=(arm64 amd64); else targets=("$architecture"); fi

build_binary() {
  local arch="$1" destination="$2" package="$3"
  local apple_arch="$arch"
  [[ "$arch" != amd64 ]] || apple_arch=x86_64
  env GOOS=darwin GOARCH="$arch" CGO_ENABLED=1 CC='/usr/bin/xcrun --sdk macosx clang' \
    MACOSX_DEPLOYMENT_TARGET=13.0 \
    CGO_CFLAGS="-isysroot \"$sdk\" -arch $apple_arch -mmacosx-version-min=13.0" \
    CGO_LDFLAGS="-isysroot \"$sdk\" -arch $apple_arch -mmacosx-version-min=13.0" \
    go build -trimpath -buildvcs=false -ldflags "-s -w -X main.version=$version" -o "$destination" "$package"
}

copy_rules() {
  local destination="$1"
  mkdir -p "$destination"
  cp -R third_party/cwtools-hoi4-config/Config "$destination/Config"
  cp third_party/cwtools-hoi4-config/LICENSE "$destination/LICENSE"
  if [[ -f third_party/cwtools-hoi4-config/README.md ]]; then cp third_party/cwtools-hoi4-config/README.md "$destination/README.md"; fi
}

make_icon() {
  local resources="$1" iconset="$stage/HaganeWorks.iconset" size
  if ! command -v sips >/dev/null || ! command -v iconutil >/dev/null; then return; fi
  mkdir -p "$iconset"
  for size in 16 32 128 256 512; do
    sips -z "$size" "$size" web/public/brand/hagane-icon-512.png --out "$iconset/icon_${size}x${size}.png" >/dev/null
    sips -z "$((size * 2))" "$((size * 2))" web/public/brand/hagane-icon-512.png --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
  done
  iconutil -c icns "$iconset" -o "$resources/HaganeWorks.icns"
}

for target in "${targets[@]}"; do
  name="HaganeWorks-$version-macos-$target"
  package_dir="$release/$name"
  app="$package_dir/Hagane Works.app"
  contents="$app/Contents"
  mkdir -p "$contents/MacOS" "$contents/Resources" "$package_dir/docs"
  if [[ "$target" == universal ]]; then
    for arch in arm64 amd64; do
      build_binary "$arch" "$stage/HaganeWorks-$arch" ./cmd/haganeworks
      build_binary "$arch" "$stage/hw-$arch" ./cmd/hw
    done
    xcrun lipo -create "$stage/HaganeWorks-arm64" "$stage/HaganeWorks-amd64" -output "$contents/MacOS/HaganeWorks"
    xcrun lipo -create "$stage/hw-arm64" "$stage/hw-amd64" -output "$package_dir/hw"
  else
    build_binary "$target" "$contents/MacOS/HaganeWorks" ./cmd/haganeworks
    build_binary "$target" "$package_dir/hw" ./cmd/hw
  fi
  cat > "$contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>org.haganeworks.studio</string>
<key>CFBundleName</key><string>Hagane Works</string>
<key>CFBundleDisplayName</key><string>Hagane Works</string>
<key>CFBundleExecutable</key><string>HaganeWorks</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleVersion</key><string>0.1.0</string>
<key>CFBundleShortVersionString</key><string>0.1.0</string>
<key>HaganeWorksBuildVersion</key><string>$version</string>
<key>CFBundleIconFile</key><string>HaganeWorks</string>
<key>LSMinimumSystemVersion</key><string>13.0</string>
<key>NSPrincipalClass</key><string>NSApplication</string>
<key>NSHighResolutionCapable</key><true/>
<key>NSAppTransportSecurity</key><dict><key>NSAllowsLocalNetworking</key><true/></dict>
</dict></plist>
PLIST
  plutil -lint "$contents/Info.plist"
  printf 'APPL????' > "$contents/PkgInfo"
  make_icon "$contents/Resources"
  copy_rules "$contents/Resources/third_party/cwtools-hoi4-config"
  copy_rules "$package_dir/third_party/cwtools-hoi4-config"
  for file in LICENSE THIRD_PARTY_NOTICES.md; do
    cp "$file" "$package_dir/$file"
    cp "$file" "$contents/Resources/$file"
  done
  cp docs/cli.md "$package_dir/docs/cli.md"
  cp docs/macos.md "$package_dir/docs/macos.md"
  printf 'Hagane Works %s\nmacOS %s\n' "$version" "$target" > "$package_dir/VERSION.txt"
  cat > "$package_dir/START-HERE.txt" <<'START'
Hagane Works for macOS / 钢之工坊 Mac 版

Extract the entire ZIP, then open Hagane Works.app. You can move the app to Applications.
完整解压后双击 Hagane Works.app，也可将它移到“应用程序”。

This local build is ad-hoc signed, not Developer ID signed or notarized. For a trusted
download macOS may require System Settings > Privacy & Security > Open Anyway.
本地构建采用临时签名，未经过公证；可信下载可能需要到“系统设置→隐私与安全性”允许打开。

Choose a separate creation folder and import a copy of your mod. Keep backups. Previews
and static checks help iteration but cannot replace final in-game checks.
另选创作目录，并用 Mod 副本进行导入。保留备份；预览与静态检查不能代替最终游戏验证。

The app uses the system's WKWebView; Go, Node and a separate browser are not required.
应用使用系统 WKWebView，运行时不需要 Go、Node 或单独的浏览器。

Your own HOI4 installation is needed for game-interface assets. No game files ship here.
游戏界面素材来自你自己的 HOI4 安装，本包不附带游戏文件。

The CLI is ./hw. See docs/macos.md and docs/cli.md for details.
命令行工具是 ./hw，详见 docs/macos.md 与 docs/cli.md。
START
  cat > "$package_dir/SOURCE-CODE.txt" <<SOURCE
Hagane Works is GNU GPL v3.0 only. The matching current-worktree source snapshot is
HaganeWorks-$version-source.zip, beside this package, including the CWTools submodule.
Keep that source archive available alongside these binaries when distributing them.
Build instructions are tools/release-macos.sh and docs/macos.md in that archive.
This build is a macOS adaptation of https://github.com/hagane-works/hagane-studio.
SOURCE
  codesign --force --sign - "$package_dir/hw"
  codesign --force --sign - "$app"
  codesign --verify --deep --strict "$app"
  codesign --verify --strict "$package_dir/hw"
  ditto -c -k --keepParent --norsrc "$package_dir" "$release/$name.zip"
done

source_name="HaganeWorks-$version-source"
source_dir="$stage/$source_name"
mkdir -p "$source_dir"
git ls-files --cached --others --exclude-standard -z > "$stage/source-files"
while IFS= read -r -d '' path; do
  [[ -f "$path" || -L "$path" ]] || continue
  mkdir -p "$source_dir/$(dirname "$path")"
  cp -pP "$path" "$source_dir/$path"
done < "$stage/source-files"
submodule=third_party/cwtools-hoi4-config
git -C "$submodule" ls-files --cached --others --exclude-standard -z > "$stage/submodule-files"
while IFS= read -r -d '' path; do
  [[ -f "$submodule/$path" || -L "$submodule/$path" ]] || continue
  mkdir -p "$source_dir/$submodule/$(dirname "$path")"
  cp -pP "$submodule/$path" "$source_dir/$submodule/$path"
done < "$stage/submodule-files"
ditto -c -k --keepParent --norsrc "$source_dir" "$release/$source_name.zip"
(cd "$release" && shasum -a 256 ./*.zip > SHA256SUMS.txt)
mkdir -p "$(dirname "$output")"
mv "$release" "$output"
printf 'Created macOS packages and corresponding source in %s\n' "$output"
