#!/usr/bin/env bash
# collab-ai installer.
#
# Detects OS + arch, downloads the matching tarball from the latest GitHub
# Release, verifies the checksum, and installs the binary to a writable
# location on PATH.
#
#   curl -sSL https://raw.githubusercontent.com/Aman035/collab-ai/main/install.sh | sh
#
# Override targets:
#   COLLAB_AI_VERSION=v0.2.0 sh install.sh   # pin a version
#   COLLAB_AI_PREFIX=$HOME/bin sh install.sh # install elsewhere

set -eu

REPO="Aman035/collab-ai"
VERSION="${COLLAB_AI_VERSION:-latest}"
PREFIX="${COLLAB_AI_PREFIX:-/usr/local/bin}"

err() { printf '\033[31merror:\033[0m %s\n' "$1" >&2; exit 1; }
info() { printf '\033[2m%s\033[0m\n' "$1" >&2; }

# OS detection
case "$(uname -s)" in
  Darwin) OS=darwin ;;
  Linux)  OS=linux ;;
  *) err "unsupported OS: $(uname -s). Try 'go install github.com/$REPO/cmd/collab-ai@latest'." ;;
esac

# Arch detection
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) err "unsupported arch: $(uname -m)" ;;
esac

# Resolve VERSION → concrete tag
if [ "$VERSION" = "latest" ]; then
  info "resolving latest release..."
  VERSION="$(curl -sSL "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
  [ -n "$VERSION" ] || err "could not resolve latest release. Set COLLAB_AI_VERSION=vX.Y.Z."
fi

VERSION_NUM="${VERSION#v}"
ARCHIVE="collab-ai_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$VERSION/$ARCHIVE"
SUMS_URL="https://github.com/$REPO/releases/download/$VERSION/checksums.txt"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

info "downloading $ARCHIVE..."
curl -fSL --progress-bar "$URL" -o "$TMP/$ARCHIVE"

info "verifying checksum..."
curl -fSL "$SUMS_URL" -o "$TMP/checksums.txt"
EXPECTED="$(grep " $ARCHIVE\$" "$TMP/checksums.txt" | awk '{print $1}')"
[ -n "$EXPECTED" ] || err "no checksum for $ARCHIVE in checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "$TMP/$ARCHIVE" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "$TMP/$ARCHIVE" | awk '{print $1}')"
else
  err "neither sha256sum nor shasum found"
fi
[ "$ACTUAL" = "$EXPECTED" ] || err "checksum mismatch (expected $EXPECTED, got $ACTUAL)"

info "extracting..."
tar -xzf "$TMP/$ARCHIVE" -C "$TMP"

# Pick a writable install dir. Honor COLLAB_AI_PREFIX; otherwise try the
# default and fall back to $HOME/.local/bin if /usr/local/bin needs sudo.
if [ -w "$PREFIX" ] || [ -w "$(dirname "$PREFIX")" ]; then
  DEST="$PREFIX"
else
  DEST="$HOME/.local/bin"
  mkdir -p "$DEST"
  info "$PREFIX is not writable; installing to $DEST instead"
fi

mv "$TMP/collab-ai" "$DEST/collab-ai"
chmod +x "$DEST/collab-ai"

printf '\033[38;5;215mcollab-ai\033[0m installed to \033[1m%s/collab-ai\033[0m\n' "$DEST" >&2
case ":$PATH:" in
  *":$DEST:"*) ;;
  *) printf '\033[33mnote:\033[0m %s is not on your PATH yet. Add it to your shell rc.\n' "$DEST" >&2 ;;
esac

# Reminder about the Axl prereq.
if ! command -v node >/dev/null 2>&1 \
  || ! "$(command -v node)" --help 2>&1 | grep -qi axl 2>/dev/null; then
  cat >&2 <<'EOF'

Next: install Gensyn Axl (the peer-routing daemon collab-ai talks to):
  git clone https://github.com/gensyn-ai/axl.git
  cd axl && go build -o node ./cmd/node/
  # then put ./node on your PATH (or set COLLAB_AXL_NODE=/path/to/node)
EOF
fi
