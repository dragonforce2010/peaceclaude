#!/usr/bin/env bash
# Package multi-platform PeaceClaude archives for Linux (amd64/arm64 + Ubuntu .deb) and Windows (amd64/arm64).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VER="${1:-$(tr -d '[:space:]' < "${ROOT}/VERSION")}"
VER="${VER#v}"
COMMIT="$(git -C "${ROOT}" rev-parse --short HEAD 2>/dev/null || echo dev)"
OUT_DIR="${ROOT}/dist/multi"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/peaceclaude-multi.XXXXXX")"
trap 'rm -rf "${WORK}"' EXIT

mkdir -p "${OUT_DIR}"
LDFLAGS="-s -w -X peaceclaude/internal/version.Version=${VER} -X peaceclaude/internal/version.Commit=${COMMIT}"

copy_docs() {
  local dst="$1"
  cp "${ROOT}/README.md" "${dst}/README.md"
  cp "${ROOT}/README.zh-CN.md" "${dst}/README.zh-CN.md"
  mkdir -p "${dst}/docs"
  cp "${ROOT}/docs/CLI_GUIDE.md" "${dst}/docs/CLI_GUIDE.md"
}

echo "→ Building multi-platform archives v${VER} (${COMMIT})..."

for arch in amd64 arm64; do
  stage="${WORK}/peaceclaude-v${VER}-linux-${arch}"
  mkdir -p "${stage}"
  (cd "${ROOT}" && CGO_ENABLED=0 GOOS=linux GOARCH="${arch}" go build -ldflags "${LDFLAGS}" -o "${stage}/peaceclaude" ./cmd/peaceclaude)
  chmod 755 "${stage}/peaceclaude"
  ln -sf peaceclaude "${stage}/pclaude"
  copy_docs "${stage}"
  tar -C "${WORK}" -czf "${OUT_DIR}/PeaceClaude-v${VER}-linux-${arch}.tar.gz" "peaceclaude-v${VER}-linux-${arch}"
  "${ROOT}/scripts/build-deb.sh" "${VER}" "${arch}" "${stage}/peaceclaude" "${OUT_DIR}"
  echo "  ✔ linux/${arch}: tar.gz + .deb"
done

for arch in amd64 arm64; do
  stage="${WORK}/peaceclaude-v${VER}-windows-${arch}"
  mkdir -p "${stage}"
  (cd "${ROOT}" && CGO_ENABLED=0 GOOS=windows GOARCH="${arch}" go build -ldflags "${LDFLAGS}" -o "${stage}/peaceclaude.exe" ./cmd/peaceclaude)
  copy_docs "${stage}"
  (cd "${WORK}" && zip -rq "${OUT_DIR}/PeaceClaude-v${VER}-windows-${arch}.zip" "peaceclaude-v${VER}-windows-${arch}")
  echo "  ✔ windows/${arch}: .zip"
done

(
  cd "${OUT_DIR}"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 PeaceClaude-* > SHA256SUMS.txt
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum PeaceClaude-* > SHA256SUMS.txt
  fi
)

echo "✔ Multi-platform artifacts ready in ${OUT_DIR}:"
ls -lh "${OUT_DIR}"
