# Hagane Works on macOS

This fork adds a native Cocoa window backed by the system's WKWebView, a `.app` bundle,
macOS packaging and Apple Silicon/Intel builds. The editor and local backend remain the
same application used on Windows. It is an unofficial macOS adaptation of Hagane Works.

## Run

Use macOS 13 or newer. Choose `macos-arm64` for Apple Silicon, `macos-amd64` for Intel,
or a `macos-universal` build for both. Extract the whole ZIP, open **Hagane Works.app**,
and choose a creation folder. The app can be moved to `/Applications` or another folder;
its interface and CWTools configuration are included. Go and Node.js are build tools,
not runtime requirements. The separate `hw` executable is the optional CLI.

Local builds are ad-hoc signed. They are not signed with a Developer ID certificate or
notarized by Apple. macOS may block a downloaded copy until it is allowed in
**System Settings → Privacy & Security → Open Anyway**. Do not disable Gatekeeper globally.

The settings file is `~/Library/Application Support/Hagane Works/settings.json`. Default
Steam detection looks in `~/Library/Application Support/Steam`, including libraries
listed in `steamapps/libraryfolders.vdf`. The game directory can also be selected in
settings. An empty Steam directory or a Workshop mod alone is not a complete HOI4
installation; game-view assets require the user's actual game files.

Import an independent copy of a mod first. Game-view reconstructions, focus/event
previews and **Check my mod** can find problems without launching HOI4, but are not the
game engine. They cannot establish event timing, AI behavior or complete compatibility.
Keep the final in-game validation step. No Paradox assets or private mod files are
included in this fork or its release packages.

## Build

Install Xcode Command Line Tools (`xcode-select --install`), Go 1.25 or newer, Node.js
20 or newer and Git. Build from a clone with its submodule initialized:

```sh
git submodule update --init --recursive
bash tools/release-macos.sh --version macos-local --arch arm64
```

Use `--arch amd64`, `--arch universal` or `--arch all` for Intel, a universal binary, or
two separate portable packages. Cross-building still requires macOS and Apple's SDK.
The script builds the frontend, runs checks, builds both application and CLI, verifies
the bundle's ad-hoc signature, and writes packages beneath `dist/release/<version>/`.
An existing version directory is left intact; choose a new version for another build.
Paths with spaces are supported.

When checks and the frontend were already built, use:

```sh
bash tools/release-macos.sh --version macos-local-2 --arch universal --skip-frontend --skip-checks
```

`--skip-frontend` requires an existing `web/dist/index.html`; it does not build or check
the frontend. `--skip-checks` does not establish that tests passed. The deployment target
is macOS 13.0. Native windows require CGO; a `CGO_ENABLED=0` development binary opens the
existing local interface in the default browser instead.

For separate frontend/backend development:

```sh
go run ./cmd/haganeworks --no-window --port 7330
HAGANEWORKS_API=http://127.0.0.1:7330 npm --prefix web run dev
```

## Source, signing and CI

Each package contains GPL and third-party notices, documentation, the native app, CLI
and CWTools rules. A sibling `HaganeWorks-<version>-source.zip` contains the matching
current worktree sources, including local modifications and the initialized submodule;
generated frontend output, dependency caches, Git metadata and ignored files are omitted.
Keep that source ZIP available beside the binaries when distributing them under GPL-3.0-only.
`SHA256SUMS.txt` covers the portable ZIPs and the source ZIP. Preserve the upstream attribution.

The `macOS` workflow runs checks and uploads Apple Silicon/Intel ZIPs plus matching source
as CI artifacts. It does not create tags, publish releases, notarize applications or
upload game files. Commercial signing/notarization would need separately configured
Apple credentials and is not part of the local packaging script.

## 中文快速说明

- 将 ZIP 完整解压，双击 **Hagane Works.app**；也可把应用移到“应用程序”。
- Apple Silicon 选 `arm64`，Intel 选 `amd64`；`universal` 同时包含两种架构。
- 设置在 `~/Library/Application Support/Hagane Works/`，游戏素材来自你自己的 HOI4 安装。
- 先用 Mod 副本导入。编辑器的预览和静态检查可以减少启动游戏的次数，最终仍需在游戏中验证。
- 构建方式见上文。发布包采用临时签名，未公证；源码 ZIP、许可证和第三方声明与二进制一起保留。
