#!/usr/bin/env bash
# Build a standard Ubuntu/Debian .deb package containing /usr/bin/peaceclaude and /usr/bin/pclaude.
# Uses dpkg-deb when available, falling back to pure Python ar/tar builder on macOS.
set -euo pipefail

VER="${1:?usage: build-deb.sh <ver> <arch:amd64|arm64> <binary-path> <out-dir>}"
ARCH="${2:?missing arch}"
BIN_PATH="${3:?missing binary path}"
OUT_DIR="${4:?missing output dir}"

mkdir -p "${OUT_DIR}"
DEB_NAME="PeaceClaude-v${VER}-ubuntu-${ARCH}.deb"
OUT_DEB="${OUT_DIR}/${DEB_NAME}"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/peaceclaude-deb.XXXXXX")"
trap 'rm -rf "${WORK}"' EXIT

PKG_ROOT="${WORK}/pkg"
mkdir -p "${PKG_ROOT}/DEBIAN" "${PKG_ROOT}/usr/bin"
cp "${BIN_PATH}" "${PKG_ROOT}/usr/bin/peaceclaude"
chmod 755 "${PKG_ROOT}/usr/bin/peaceclaude"
ln -sf peaceclaude "${PKG_ROOT}/usr/bin/pclaude"

cat > "${PKG_ROOT}/DEBIAN/control" <<EOF
Package: peaceclaude
Version: ${VER}
Section: net
Priority: optional
Architecture: ${ARCH}
Maintainer: PeaceClaude Contributors
Description: Zero-VM static-IP fail-closed security cockpit for Claude CLI & Desktop
 Local HTTP/SOCKS5/PAC fail-closed static-IP gateway with multi-account profile isolation,
 remote pairing, and systemd --user autostart.
EOF
chmod 644 "${PKG_ROOT}/DEBIAN/control"

if command -v dpkg-deb >/dev/null 2>&1; then
  dpkg-deb --build --root-owner-group "${PKG_ROOT}" "${OUT_DEB}" >/dev/null
else
  python3 - "${PKG_ROOT}" "${OUT_DEB}" <<'PY'
import io, os, sys, tarfile, time

pkg_root = sys.argv[1]
out_deb = sys.argv[2]
now = int(time.time())

def make_tar_gz(src_dir, exclude_top=None):
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as tf:
        for root, dirs, files in os.walk(src_dir):
            dirs.sort()
            files.sort()
            rel_root = os.path.relpath(root, src_dir)
            if rel_root == ".":
                if exclude_top:
                    dirs[:] = [d for d in dirs if d != exclude_top]
                arc_dir = "."
            else:
                arc_dir = "./" + rel_root.replace(os.sep, "/")
                ti = tf.gettarinfo(root, arcname=arc_dir)
                ti.uid = ti.gid = 0
                ti.uname = ti.gname = "root"
                ti.mtime = now
                tf.addfile(ti)
            for fn in files:
                full = os.path.join(root, fn)
                arc = ("." if rel_root == "." else arc_dir) + "/" + fn
                ti = tf.gettarinfo(full, arcname=arc)
                ti.uid = ti.gid = 0
                ti.uname = ti.gname = "root"
                ti.mtime = now
                if ti.islnk() or ti.issym():
                    tf.addfile(ti)
                else:
                    with open(full, "rb") as f:
                        tf.addfile(ti, f)
    return buf.getvalue()

control_tar = make_tar_gz(os.path.join(pkg_root, "DEBIAN"))
data_tar = make_tar_gz(pkg_root, exclude_top="DEBIAN")

def ar_member(name, payload):
    hdr = f"{name:<16}{now:<12}0     0     100644  {len(payload):<10}`\n".encode("ascii")
    out = hdr + payload
    if len(payload) % 2 == 1:
        out += b"\n"
    return out

with open(out_deb, "wb") as f:
    f.write(b"!<arch>\n")
    f.write(ar_member("debian-binary", b"2.0\n"))
    f.write(ar_member("control.tar.gz", control_tar))
    f.write(ar_member("data.tar.gz", data_tar))
PY
fi
