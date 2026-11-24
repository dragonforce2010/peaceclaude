#!/usr/bin/env bash
# PeaceClaude One-Line Installer (macOS Universal & Linux amd64/arm64)
# Usage: curl -fsSL https://dragonforce2010.github.io/peaceclaude/install.sh | bash
set -euo pipefail

REPO="dragonforce2010/peaceclaude"
RAW_VER="${PEACECLAUDE_VERSION:-v0.2.0}"
TAG="v${RAW_VER#v}"
INSTALL_DIR="${PEACECLAUDE_INSTALL_DIR:-$HOME/.local/bin}"

OS="$(uname -s)"
MACHINE="$(uname -m)"

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/peaceclaude-webinstall.XXXXXX")"
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

atomic_install_bin() {
  local src="$1"
  mkdir -p "$INSTALL_DIR"
  local tmp_dst="${INSTALL_DIR}/peaceclaude.new.$$"
  cp "$src" "$tmp_dst"
  chmod 755 "$tmp_dst"
  mv -f "$tmp_dst" "${INSTALL_DIR}/peaceclaude"
  ln -sf peaceclaude "${INSTALL_DIR}/pclaude"
}

echo "==> Installing PeaceClaude ${TAG} (${OS}/${MACHINE})..."

case "$OS" in
  Darwin)
    ARCHIVE="peaceclaude-${TAG}-macos-universal.tar.gz"
    URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE}"
    echo "==> Downloading ${URL}"
    curl -fSL --progress-bar "$URL" -o "${TMP_DIR}/${ARCHIVE}"
    tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "$TMP_DIR"

    INSTALLER=""
    if [[ -f "${TMP_DIR}/peaceclaude-${TAG}-macos-universal/install.sh" ]]; then
      INSTALLER="${TMP_DIR}/peaceclaude-${TAG}-macos-universal/install.sh"
    else
      INSTALLER="$(find "$TMP_DIR" -maxdepth 2 -type f -name 'install.sh' | head -n 1 || true)"
    fi

    if [[ -n "$INSTALLER" ]]; then
      chmod +x "$INSTALLER"
      PEACECLAUDE_INSTALL_DIR="$INSTALL_DIR" bash "$INSTALLER"
    else
      BIN_SRC="$(find "$TMP_DIR" -maxdepth 3 -type f -name 'peaceclaude' | head -n 1 || true)"
      if [[ -z "$BIN_SRC" ]]; then
        echo "Error: failed to locate install.sh or peaceclaude binary inside ${ARCHIVE}" >&2
        exit 1
      fi
      atomic_install_bin "$BIN_SRC"
    fi
    ;;

  Linux)
    case "$MACHINE" in
      x86_64|amd64) ARCH="amd64" ;;
      aarch64|arm64) ARCH="arm64" ;;
      *)
        echo "Unsupported Linux architecture: ${MACHINE} (supported: x86_64/amd64, arm64/aarch64)" >&2
        exit 1
        ;;
    esac

    ARCHIVE="PeaceClaude-${TAG}-linux-${ARCH}.tar.gz"
    URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE}"
    echo "==> Downloading ${URL}"
    curl -fSL --progress-bar "$URL" -o "${TMP_DIR}/${ARCHIVE}"
    tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "$TMP_DIR"

    BIN_SRC=""
    if [[ -f "${TMP_DIR}/peaceclaude-${TAG}-linux-${ARCH}/peaceclaude" ]]; then
      BIN_SRC="${TMP_DIR}/peaceclaude-${TAG}-linux-${ARCH}/peaceclaude"
    else
      BIN_SRC="$(find "$TMP_DIR" -maxdepth 3 -type f -name 'peaceclaude' | head -n 1 || true)"
    fi

    if [[ -z "$BIN_SRC" ]]; then
      echo "Error: failed to locate peaceclaude binary inside ${ARCHIVE}" >&2
      exit 1
    fi

    atomic_install_bin "$BIN_SRC"
    echo "==> Installed to ${INSTALL_DIR}/peaceclaude (alias: ${INSTALL_DIR}/pclaude)"
    echo "    Next: ${INSTALL_DIR}/peaceclaude service install"
    ;;

  *)
    echo "Unsupported OS: ${OS}. For Windows, download the .zip archive from https://github.com/${REPO}/releases/tag/${TAG}" >&2
    exit 1
    ;;
esac

if [[ -x "${INSTALL_DIR}/peaceclaude" ]]; then
  echo -n "==> Verified CLI: "
  "${INSTALL_DIR}/peaceclaude" --version || true
fi

if [[ ":${PATH}:" != *":${INSTALL_DIR}:"* ]]; then
  echo ""
  echo "Note: Add ${INSTALL_DIR} to your PATH if 'peaceclaude' is not found:"
  echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
fi
