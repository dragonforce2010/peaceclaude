#!/usr/bin/env bash
# PeaceClaude local installer (modeled after recode.cat/install.sh)
# Builds from source into ~/.local/bin/peaceclaude (+ pclaude alias) and runs `peaceclaude setup`.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${PEACECLAUDE_INSTALL_DIR:-$HOME/.local/bin}"
NO_LAUNCHD=0
UNINSTALL=0

usage() {
  cat <<'EOF'
PeaceClaude 安装脚本 (macOS)

用法:
  ./install.sh [选项]

选项:
  -d, --dir <path>   安装目录（默认 ~/.local/bin）
      --no-launchd   不注册 LaunchAgent 登录自启
      --uninstall    卸载 PeaceClaude 服务与启动器（保留档案数据）
  -h, --help         显示帮助
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    -d|--dir) INSTALL_DIR="$2"; shift 2 ;;
    --no-launchd) NO_LAUNCHD=1; shift ;;
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

command -v go >/dev/null 2>&1 || { echo "✖ 缺少 go 工具链，请先安装 Go 1.24+" >&2; exit 1; }
mkdir -p "$INSTALL_DIR"

VER="$(cat "$SCRIPT_DIR/VERSION" 2>/dev/null || echo 0.1.0)"
COMMIT="$(git -C "$SCRIPT_DIR" rev-parse --short HEAD 2>/dev/null || echo local)"

echo "→ 编译 peaceclaude v${VER} (${COMMIT})..."
(cd "$SCRIPT_DIR" && go build -ldflags "-s -w -X peaceclaude/internal/version.Version=${VER} -X peaceclaude/internal/version.Commit=${COMMIT}" -o "$INSTALL_DIR/peaceclaude" ./cmd/peaceclaude)
chmod 755 "$INSTALL_DIR/peaceclaude"
ln -sf peaceclaude "$INSTALL_DIR/pclaude"
if [ "$(uname -s)" = "Darwin" ] && command -v xattr >/dev/null 2>&1; then
  xattr -dr com.apple.quarantine "$INSTALL_DIR/peaceclaude" 2>/dev/null || true
fi

echo "→ 运行 peaceclaude setup..."
if [ "$NO_LAUNCHD" = "1" ]; then
  "$INSTALL_DIR/peaceclaude" setup --no-launchd
  echo "→ 重启本地守护网关以生效新版本..."
  "$INSTALL_DIR/peaceclaude" service restart --no-launchd
else
  "$INSTALL_DIR/peaceclaude" setup
  echo "→ 重启本地守护网关以生效新版本..."
  "$INSTALL_DIR/peaceclaude" service restart
fi

echo ""
echo "✔ 已安装: $INSTALL_DIR/peaceclaude (别名: pclaude)"
"$INSTALL_DIR/peaceclaude" --version
