#!/usr/bin/env bash
# collab installer. One curl, one binary, one global command.
#
#   curl -sSL https://raw.githubusercontent.com/Aman035/collab-ai/main/install.sh | sh
#
# Detects OS+arch and downloads the matching `collab` binary from the latest
# GitHub Release. Verifies the SHA-256 checksum, places it on a writable PATH
# location. The Gensyn Axl daemon is NOT installed here — it's auto-built
# into ~/.collab/bin/axl-node on first `collab create` / `collab connect`.
#
# Overrides:
#   COLLAB_VERSION=v0.2.0   pin a specific release
#   COLLAB_PREFIX=$HOME/bin install elsewhere

set -eu

REPO="Aman035/collab-ai"
VERSION="${COLLAB_VERSION:-latest}"
PREFIX="${COLLAB_PREFIX:-/usr/local/bin}"

err()  { printf '\033[31merror:\033[0m %s\n' "$1" >&2; exit 1; }
info() { printf '\033[2m%s\033[0m\n' "$1" >&2; }
ok()   { printf '\033[38;5;215m%s\033[0m %s\n' "$1" "$2" >&2; }

# OS / arch detection.
case "$(uname -s)" in
  Darwin) OS=darwin ;;
  Linux)  OS=linux ;;
  *) err "unsupported OS: $(uname -s). Try: go install github.com/$REPO/cmd/collab@latest" ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) err "unsupported arch: $(uname -m)" ;;
esac

# Resolve VERSION → concrete tag.
if [ "$VERSION" = "latest" ]; then
  info "→ resolving latest release..."
  VERSION="$(curl -sSL "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
  [ -n "$VERSION" ] || err "could not resolve latest release. Try: COLLAB_VERSION=vX.Y.Z $0"
fi
VERSION_NUM="${VERSION#v}"
ARCHIVE="collab-ai_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$VERSION/$ARCHIVE"
SUMS_URL="https://github.com/$REPO/releases/download/$VERSION/checksums.txt"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

info "→ downloading $ARCHIVE..."
curl -fSL --progress-bar "$URL" -o "$TMP/$ARCHIVE"

info "→ verifying checksum..."
curl -fSL "$SUMS_URL" -o "$TMP/checksums.txt"
EXPECTED="$(grep " $ARCHIVE\$" "$TMP/checksums.txt" | awk '{print $1}')"
[ -n "$EXPECTED" ] || err "no checksum for $ARCHIVE"
if   command -v sha256sum >/dev/null 2>&1; then ACTUAL="$(sha256sum "$TMP/$ARCHIVE" | awk '{print $1}')"
elif command -v shasum    >/dev/null 2>&1; then ACTUAL="$(shasum -a 256 "$TMP/$ARCHIVE" | awk '{print $1}')"
else err "neither sha256sum nor shasum available"; fi
[ "$ACTUAL" = "$EXPECTED" ] || err "checksum mismatch (expected $EXPECTED, got $ACTUAL)"

info "→ extracting..."
tar -xzf "$TMP/$ARCHIVE" -C "$TMP"

# Pick a writable install dir: $COLLAB_PREFIX, then $HOME/.local/bin if
# /usr/local/bin needs sudo.
if [ -w "$PREFIX" ] || [ -w "$(dirname "$PREFIX")" ]; then DEST="$PREFIX"
else
  DEST="$HOME/.local/bin"
  mkdir -p "$DEST"
  info "$PREFIX is not writable; installing to $DEST instead"
fi

mv "$TMP/collab" "$DEST/collab"
chmod +x "$DEST/collab"

ok "✓" "collab installed to $DEST/collab"
case ":$PATH:" in
  *":$DEST:"*) ;;
  *) printf '\033[33mnote:\033[0m %s is not on your PATH yet — add it to your shell rc.\n' "$DEST" >&2 ;;
esac

cat >&2 <<EOF

Next:
  $ collab create               # host a new pairing session
  $ collab connect COLLAB-...   # join one

On first run, collab auto-builds the Gensyn Axl daemon into
~/.collab/bin/axl-node. Requires \`git\` and \`go\` for that one-time step.

Repo: https://github.com/$REPO
EOF
