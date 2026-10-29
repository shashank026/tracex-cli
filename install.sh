#!/bin/sh
# Install the TraceX CLI:
#
#   curl -fsSL https://app.tracexpro.com/install | sh
#
# Options (environment variables):
#   TRACEX_VERSION       a release such as v0.2.0 (default: the latest)
#   TRACEX_INSTALL_DIR   where to put the binary (default: ~/.local/bin)
#   TRACEX_RELEASES      where releases are downloaded from
set -eu

RELEASES="${TRACEX_RELEASES:-https://github.com/tracexpro/tracex-cli/releases}"
VERSION="${TRACEX_VERSION:-latest}"
INSTALL_DIR="${TRACEX_INSTALL_DIR:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
fail() { printf 'tracex install: %s\n' "$*" >&2; exit 1; }

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  MINGW* | MSYS* | CYGWIN*) fail "on Windows, download tracex_windows_amd64.zip from $RELEASES" ;;
  *) fail "unsupported system: $(uname -s)" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) fail "unsupported processor: $(uname -m)" ;;
esac

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q "$1" -O "$2"; }
else
  fail "curl or wget is required"
fi

if [ "$VERSION" = latest ]; then
  base="$RELEASES/latest/download"
else
  base="$RELEASES/download/$VERSION"
fi
archive="tracex_${os}_${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

say "Downloading TraceX CLI ($VERSION, $os/$arch)…"
fetch "$base/$archive" "$tmp/$archive" || fail "could not download $base/$archive"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || fail "could not download the checksums"

# Never install a binary whose checksum does not match the release
expected="$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)"
[ -n "$expected" ] || fail "no checksum for $archive"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$archive" | cut -d' ' -f1)"
else
  actual="$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)"
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" tracex
mkdir -p "$INSTALL_DIR"
mv "$tmp/tracex" "$INSTALL_DIR/tracex"
chmod 755 "$INSTALL_DIR/tracex"

say "Installed $("$INSTALL_DIR/tracex" version) to $INSTALL_DIR/tracex"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    say ""
    say "$INSTALL_DIR is not on your PATH. Add it, e.g.:"
    say "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.zshrc && source ~/.zshrc"
    ;;
esac
say ""
say "Get started:"
say "  tracex login     sign in through your browser"
say "  tracex init      link a repository to its TraceX project"
say "  tracex -c        ask TraceX about your data and code"
