#!/usr/bin/env bash
# PeaceClaude macOS Universal (arm64 + x86_64) release packager.
# Produces self-contained PeaceClaude.app, .dmg, offline .tar.gz, and SHA256SUMS.txt in dist/.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${ROOT}/dist"
ALLOW_DIRTY=0
SKIP_DMG=0

usage() {
  cat <<'EOF'
PeaceClaude macOS Universal 打包脚本

用法:
  ./scripts/package-release.sh [选项]

选项:
  --allow-dirty       允许工作区存在未提交改动时打包
  --out-dir <path>    指定输出目录（默认: <repo>/dist）
  --skip-dmg          仅生成 tar.gz 与校验和，跳过 .dmg 构建
  -h, --help          显示帮助
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --allow-dirty) ALLOW_DIRTY=1; shift ;;
    --out-dir) OUT_DIR="$2"; shift 2 ;;
    --skip-dmg) SKIP_DMG=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 1 ;;
  esac
done

if [ "$(uname -s)" != "Darwin" ]; then
  echo "✖ 仅支持在 macOS (Darwin) 上打包 Universal .app/.dmg" >&2
  exit 1
fi

for cmd in go swiftc lipo codesign shasum tar ditto xattr; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "✖ 缺少必需工具: $cmd" >&2; exit 1; }
done
if [ "$SKIP_DMG" = "0" ]; then
  command -v hdiutil >/dev/null 2>&1 || { echo "✖ 缺少必需工具: hdiutil" >&2; exit 1; }
fi

if [ "$ALLOW_DIRTY" = "0" ]; then
  if [ -n "$(git -C "$ROOT" status --porcelain)" ]; then
    echo "✖ Git 工作区存在未提交修改，请先提交或使用 --allow-dirty" >&2
    git -C "$ROOT" status --short >&2
    exit 1
  fi
fi

VER="$(tr -d '[:space:]' < "$ROOT/VERSION")"
if [ -z "$VER" ]; then
  echo "✖ VERSION 文件为空" >&2
  exit 1
fi
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo dev)"
LDFLAGS="-s -w -X peaceclaude/internal/version.Version=${VER} -X peaceclaude/internal/version.Commit=${COMMIT}"

mkdir -p "$OUT_DIR"
OUT_DIR="$(cd "$OUT_DIR" && pwd)"

WORK="$(mktemp -d /tmp/peaceclaude-release.XXXXXX)"
cleanup() {
  rm -rf "$WORK"
}
trap cleanup EXIT

echo "=== 打包 PeaceClaude v${VER} (${COMMIT}) [macOS Universal arm64 + x86_64] ==="

# 1. Build Universal Go CLI binary
echo "→ 编译 Universal Go 二进制..."
(
  cd "$ROOT"
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o "$WORK/peaceclaude-arm64" ./cmd/peaceclaude
  CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$WORK/peaceclaude-amd64" ./cmd/peaceclaude
)
lipo -create "$WORK/peaceclaude-arm64" "$WORK/peaceclaude-amd64" -output "$WORK/peaceclaude"
chmod 755 "$WORK/peaceclaude"

# 2. Build Universal Swift macOS App launcher binary
echo "→ 编译 Universal Swift 桌面端二进制..."
SWIFT_SRC="$ROOT/internal/launcher/app_macos.swift"
swiftc -O -target arm64-apple-macosx12.0 -framework Cocoa -framework WebKit "$SWIFT_SRC" -o "$WORK/PeaceClaude-arm64"
swiftc -O -target x86_64-apple-macosx12.0 -framework Cocoa -framework WebKit "$SWIFT_SRC" -o "$WORK/PeaceClaude-x86_64"
lipo -create "$WORK/PeaceClaude-arm64" "$WORK/PeaceClaude-x86_64" -output "$WORK/PeaceClaude"
chmod 755 "$WORK/PeaceClaude"

# 3. Assemble self-contained PeaceClaude.app using built CLI for Info.plist & AppIcon.icns
echo "→ 组装自包含 Universal PeaceClaude.app..."
mkdir -p "$WORK/app-stage" "$WORK/fake-home"
PEACECLAUDE_HOME="$WORK/fake-home" "$WORK/peaceclaude" app install --dir "$WORK/app-stage"
APP="$WORK/app-stage/PeaceClaude.app"

cp "$WORK/PeaceClaude" "$APP/Contents/MacOS/PeaceClaude"
chmod 755 "$APP/Contents/MacOS/PeaceClaude"

mkdir -p "$APP/Contents/Resources/bin"
cp "$WORK/peaceclaude" "$APP/Contents/Resources/bin/peaceclaude"
chmod 755 "$APP/Contents/Resources/bin/peaceclaude"

xattr -cr "$APP" 2>/dev/null || true
codesign --force --sign - "$APP/Contents/Resources/bin/peaceclaude"
codesign --force --sign - "$APP/Contents/MacOS/PeaceClaude"
codesign --force --deep --sign - "$APP"

echo "→ 校验代码签名与双架构 slice..."
codesign --verify --deep --strict --verbose=2 "$APP"

assert_universal() {
  local target="$1"
  local info
  info="$(lipo -info "$target")"
  if [[ "$info" != *"arm64"* || "$info" != *"x86_64"* ]]; then
    echo "✖ 缺少 Universal 架构 ($target): $info" >&2
    exit 1
  fi
  echo "  ✔ $info"
}

assert_universal "$WORK/peaceclaude"
assert_universal "$APP/Contents/MacOS/PeaceClaude"
assert_universal "$APP/Contents/Resources/bin/peaceclaude"

SMOKE_VER="$("$WORK/peaceclaude" --version)"
if [ "$SMOKE_VER" != "peaceclaude ${VER} (${COMMIT})" ]; then
  echo "✖ 版本输出不符合预期: $SMOKE_VER (期望: peaceclaude ${VER} (${COMMIT}))" >&2
  exit 1
fi
echo "  ✔ CLI --version: $SMOKE_VER"

# 4. Generate offline README-INSTALL.txt
cat > "$WORK/README-INSTALL.txt" <<EOF
PeaceClaude v${VER} (${COMMIT}) — macOS Universal 离线安装包
===========================================================

本安装包免 Go 编译环境，同时支持 Apple Silicon (M 系列) 与 Intel Mac，
升级安装时完整保留已有 ~/.peaceclaude 账号与隔离配置数据。

【方式一：使用 .dmg 图形镜像安装（推荐）】
1. 将镜像中的 PeaceClaude.app 拖入 Applications 文件夹；
2. 如需同时安装终端命令行 (peaceclaude / pclaude) 与后台开机自启服务，
   双击镜像中的 Install-PeaceClaude-CLI.command 即可完成。
   （若仅双击已拖入 /Applications 的 PeaceClaude.app，它也会自动使用包内
    自带的 CLI 初始化配置并拉起本地守护网关。）

【方式二：使用 .tar.gz 命令行安装】
1. 解压压缩包并进入目录：
   tar -xzf peaceclaude-v${VER}-macos-universal.tar.gz
   cd peaceclaude-v${VER}-macos-universal
2. 运行免编译安装脚本：
   ./install.sh
   常用参数：
     -d, --dir <path>   指定 CLI 安装目录（默认 ~/.local/bin）
         --no-launchd   不注册 LaunchAgent 开机自启
         --no-app       仅升级 CLI，不覆盖安装 PeaceClaude.app
         --uninstall    卸载服务与 .app 启动器（保留 ~/.peaceclaude 账号数据）

【首次打开安全提示处理（未签名/ad-hoc 签名）】
如果 macOS 提示“无法验证开发者”或“已损坏”，在终端运行一次即可：
  xattr -dr com.apple.quarantine /Applications/PeaceClaude.app
  xattr -dr com.apple.quarantine ~/.local/bin/peaceclaude

【安装后常用验证命令】
  peaceclaude --version
  peaceclaude status
  peaceclaude doctor
  peaceclaude cli
  peaceclaude login-browser https://fuck-claude.app/

【卸载（保留隔离账号数据）】
  peaceclaude uninstall
EOF

# 5. Build offline tar.gz package
TAR_NAME="peaceclaude-v${VER}-macos-universal.tar.gz"
TAR_FOLDER="peaceclaude-v${VER}-macos-universal"
TAR_STAGE="$WORK/tar-parent/${TAR_FOLDER}"
mkdir -p "$TAR_STAGE"

cp "$WORK/peaceclaude" "$TAR_STAGE/peaceclaude"
chmod 755 "$TAR_STAGE/peaceclaude"
ditto "$APP" "$TAR_STAGE/PeaceClaude.app"
cp "$WORK/README-INSTALL.txt" "$TAR_STAGE/README-INSTALL.txt"

cat > "$TAR_STAGE/install.sh" <<'EOF'
#!/usr/bin/env bash
# PeaceClaude 离线免 Go 安装脚本（支持 macOS Apple Silicon & Intel）
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${PEACECLAUDE_INSTALL_DIR:-$HOME/.local/bin}"
NO_LAUNCHD=0
NO_APP=0
UNINSTALL=0

usage() {
  cat <<'USAGE'
PeaceClaude 离线安装脚本 (macOS Universal)

用法:
  ./install.sh [选项]

选项:
  -d, --dir <path>   CLI 安装目录（默认 ~/.local/bin）
      --no-launchd   不注册 LaunchAgent 登录自启
      --no-app       跳过安装 /Applications/PeaceClaude.app
      --uninstall    卸载 PeaceClaude 服务与启动器（保留档案数据）
  -h, --help         显示帮助
USAGE
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    -d|--dir) INSTALL_DIR="$2"; shift 2 ;;
    --no-launchd) NO_LAUNCHD=1; shift ;;
    --no-app) NO_APP=1; shift ;;
    --uninstall) UNINSTALL=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 1 ;;
  esac
done

if [ "$UNINSTALL" = "1" ]; then
  if [ -x "$INSTALL_DIR/peaceclaude" ]; then
    "$INSTALL_DIR/peaceclaude" uninstall
    rm -f "$INSTALL_DIR/peaceclaude" "$INSTALL_DIR/pclaude"
    echo "✔ 已移除 $INSTALL_DIR/peaceclaude 与 pclaude"
  else
    echo "未检测到 $INSTALL_DIR/peaceclaude"
  fi
  exit 0
fi

SRC_BIN="$SCRIPT_DIR/peaceclaude"
if [ ! -x "$SRC_BIN" ]; then
  echo "✖ 未找到离线二进制: $SRC_BIN" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
echo "→ 安装 CLI 到 $INSTALL_DIR..."
cp "$SRC_BIN" "$INSTALL_DIR/peaceclaude"
chmod 755 "$INSTALL_DIR/peaceclaude"
ln -sf peaceclaude "$INSTALL_DIR/pclaude"
if command -v xattr >/dev/null 2>&1; then
  xattr -dr com.apple.quarantine "$INSTALL_DIR/peaceclaude" 2>/dev/null || true
fi

if [ "$NO_APP" = "0" ] && [ -d "$SCRIPT_DIR/PeaceClaude.app" ]; then
  APP_TARGET_DIR="/Applications"
  if ! touch "$APP_TARGET_DIR/.peaceclaude-write-test" 2>/dev/null; then
    APP_TARGET_DIR="$HOME/Applications"
    mkdir -p "$APP_TARGET_DIR"
  else
    rm -f "$APP_TARGET_DIR/.peaceclaude-write-test"
  fi
  rm -rf "$APP_TARGET_DIR/CageClaude.app" "$APP_TARGET_DIR/Claude (cageclaude).app" "$APP_TARGET_DIR/Claude (peaceclaude).app"
  echo "→ 安装自包含应用到 $APP_TARGET_DIR/PeaceClaude.app..."
  rm -rf "$APP_TARGET_DIR/PeaceClaude.app"
  ditto "$SCRIPT_DIR/PeaceClaude.app" "$APP_TARGET_DIR/PeaceClaude.app"
  if command -v xattr >/dev/null 2>&1; then
    xattr -dr com.apple.quarantine "$APP_TARGET_DIR/PeaceClaude.app" 2>/dev/null || true
  fi
  /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$APP_TARGET_DIR/PeaceClaude.app" >/dev/null 2>&1 || true
fi

echo "→ 运行 peaceclaude setup（保留已有档案数据）..."
if [ "$NO_LAUNCHD" = "1" ]; then
  "$INSTALL_DIR/peaceclaude" setup --no-app --no-launchd
  echo "→ 重启本地守护网关以生效新版本..."
  "$INSTALL_DIR/peaceclaude" service restart --no-launchd
else
  "$INSTALL_DIR/peaceclaude" setup --no-app
  echo "→ 重启本地守护网关以生效新版本..."
  "$INSTALL_DIR/peaceclaude" service restart
fi

echo ""
echo "✔ 离线安装完成: $INSTALL_DIR/peaceclaude (别名: pclaude)"
"$INSTALL_DIR/peaceclaude" --version
EOF
chmod 755 "$TAR_STAGE/install.sh"

echo "→ 生成离线压缩包 ${TAR_NAME}..."
COPYFILE_DISABLE=1 tar -czf "${OUT_DIR}/${TAR_NAME}" -C "$WORK/tar-parent" "$TAR_FOLDER"
ARTIFACTS=("$TAR_NAME")

# 6. Build DMG package unless skipped
if [ "$SKIP_DMG" = "0" ]; then
  DMG_NAME="PeaceClaude-v${VER}-macos-universal.dmg"
  DMG_STAGE="$WORK/dmg-root"
  mkdir -p "$DMG_STAGE"
  ditto "$APP" "$DMG_STAGE/PeaceClaude.app"
  ln -s /Applications "$DMG_STAGE/Applications"
  cp "$WORK/README-INSTALL.txt" "$DMG_STAGE/README-INSTALL.txt"

  cat > "$DMG_STAGE/Install-PeaceClaude-CLI.command" <<'EOF'
#!/usr/bin/env bash
# 一键安装 PeaceClaude.app 与 peaceclaude/pclaude CLI（完整保留 ~/.peaceclaude 数据）
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC_APP="$SCRIPT_DIR/PeaceClaude.app"
SRC_BIN="$SRC_APP/Contents/Resources/bin/peaceclaude"
INSTALL_DIR="${PEACECLAUDE_INSTALL_DIR:-$HOME/.local/bin}"

if [ ! -x "$SRC_BIN" ]; then
  echo "✖ 未找到内嵌 CLI 二进制: $SRC_BIN" >&2
  exit 1
fi

echo "=== PeaceClaude 一键安装与升级 ==="
mkdir -p "$INSTALL_DIR"
cp "$SRC_BIN" "$INSTALL_DIR/peaceclaude"
chmod 755 "$INSTALL_DIR/peaceclaude"
ln -sf peaceclaude "$INSTALL_DIR/pclaude"
xattr -dr com.apple.quarantine "$INSTALL_DIR/peaceclaude" 2>/dev/null || true

APP_TARGET_DIR="/Applications"
if ! touch "$APP_TARGET_DIR/.peaceclaude-write-test" 2>/dev/null; then
  APP_TARGET_DIR="$HOME/Applications"
  mkdir -p "$APP_TARGET_DIR"
else
  rm -f "$APP_TARGET_DIR/.peaceclaude-write-test"
fi
rm -rf "$APP_TARGET_DIR/CageClaude.app" "$APP_TARGET_DIR/Claude (cageclaude).app" "$APP_TARGET_DIR/Claude (peaceclaude).app"
echo "→ 更新 $APP_TARGET_DIR/PeaceClaude.app..."
rm -rf "$APP_TARGET_DIR/PeaceClaude.app"
ditto "$SRC_APP" "$APP_TARGET_DIR/PeaceClaude.app"
xattr -dr com.apple.quarantine "$APP_TARGET_DIR/PeaceClaude.app" 2>/dev/null || true
/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$APP_TARGET_DIR/PeaceClaude.app" >/dev/null 2>&1 || true

echo "→ 初始化/更新本地网关服务（完整保留 ~/.peaceclaude 账号数据）..."
"$INSTALL_DIR/peaceclaude" setup --no-app
"$INSTALL_DIR/peaceclaude" service restart

echo ""
echo "✔ 安装完成！当前版本: $("$INSTALL_DIR/peaceclaude" --version)"
echo "  - CLI 入口: $INSTALL_DIR/peaceclaude (短别名: pclaude)"
echo "  - 桌面应用: $APP_TARGET_DIR/PeaceClaude.app"
echo "  - Web 台地址: http://127.0.0.1:18899"
if [ -t 0 ]; then
  read -r -p "按回车键结束..." _ || true
fi
EOF
  chmod 755 "$DMG_STAGE/Install-PeaceClaude-CLI.command"

  echo "→ 生成并校验 ${DMG_NAME}..."
  hdiutil create -volname "PeaceClaude v${VER}" -srcfolder "$DMG_STAGE" -ov -format UDZO -imagekey zlib-level=9 "${OUT_DIR}/${DMG_NAME}" >/dev/null
  hdiutil verify "${OUT_DIR}/${DMG_NAME}" >/dev/null
  ARTIFACTS=("$DMG_NAME" "${ARTIFACTS[@]}")
fi

# 7. Write and verify SHA256SUMS.txt
echo "→ 计算并核验 SHA256SUMS.txt..."
(
  cd "$OUT_DIR"
  shasum -a 256 "${ARTIFACTS[@]}" > SHA256SUMS.txt
  shasum -a 256 -c SHA256SUMS.txt
)

echo ""
echo "✔ 打包完成！输出目录: $OUT_DIR"
ls -lh "$OUT_DIR"
