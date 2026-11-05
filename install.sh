#!/bin/sh
# Install the TraceX CLI:
#
#   curl -fsSL https://tracexpro.com/install | sh
#   curl -fsSL https://tracexpro.com/install | sh -s -- v0.1.1
#
# Options (environment variables):
#   TRACEX_VERSION       a release such as v0.2.0 (default: the latest)
#   TRACEX_INSTALL_DIR   where to put the binary (default: ~/.local/bin)
#   TRACEX_RELEASES      where releases are downloaded from
set -eu

RELEASES="${TRACEX_RELEASES:-https://github.com/shashank026/tracex-cli/releases}"
VERSION="${1:-${TRACEX_VERSION:-latest}}"
case "$VERSION" in latest | v*) ;; *) VERSION="v$VERSION" ;; esac
INSTALL_DIR="${TRACEX_INSTALL_DIR:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
fail() { printf 'tracex install: %s\n' "$*" >&2; exit 1; }

# Progress goes to stderr: with `curl … | sh` that is still the terminal. Colours only on a
# terminal, and never when NO_COLOR is set.
if [ -t 2 ] && [ -z "${NO_COLOR:-}" ]; then
  green="$(printf '\033[32m')" red="$(printf '\033[31m')" dim="$(printf '\033[2m')" off="$(printf '\033[0m')"
else
  green="" red="" dim="" off=""
fi
done_line() { printf '%s✓%s %s\n' "$green" "$off" "$*" >&2; }

# step "Downloading…" "Downloaded" command args: runs the command with a spinner on a terminal,
# then leaves a ✓ (or ✗) line behind
step() {
  doing=$1 did=$2
  shift 2
  if [ ! -t 2 ]; then
    "$@" || return 1
    done_line "$did"
    return 0
  fi
  # The command's own errors wait in a file until the spinner line is cleared
  "$@" 2>"$tmp/step-errors" &
  pid=$!
  while kill -0 "$pid" 2>/dev/null; do
    for frame in ⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏; do
      kill -0 "$pid" 2>/dev/null || break
      printf '\r\033[2K%s%s%s %s' "$dim" "$frame" "$off" "$doing" >&2
      sleep 0.08
    done
  done
  if wait "$pid"; then
    printf '\r\033[2K' >&2
    done_line "$did"
  else
    printf '\r\033[2K%s✗%s %s\n' "$red" "$off" "$doing" >&2
    cat "$tmp/step-errors" >&2
    return 1
  fi
}

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

download() { fetch "$base/$archive" "$tmp/$archive" && fetch "$base/checksums.txt" "$tmp/checksums.txt"; }
step "Downloading TraceX CLI ($VERSION, $os/$arch)…" "Downloaded TraceX CLI ($VERSION, $os/$arch)" download ||
  fail "could not download $base/$archive"

# Never install a binary whose checksum does not match the release
expected="$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)"
[ -n "$expected" ] || fail "no checksum for $archive"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$archive" | cut -d' ' -f1)"
else
  actual="$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)"
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $archive"
done_line "Checksum verified"

tar -xzf "$tmp/$archive" -C "$tmp" tracex
mkdir -p "$INSTALL_DIR"
mv "$tmp/tracex" "$INSTALL_DIR/tracex"
chmod 755 "$INSTALL_DIR/tracex"

done_line "Installed $("$INSTALL_DIR/tracex" version) to $INSTALL_DIR/tracex"
# Put the install folder on PATH for new terminals (once)
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    line="export PATH=\"$INSTALL_DIR:\$PATH\""
    case "$(basename "${SHELL:-}")" in
      zsh) rc="${ZDOTDIR:-$HOME}/.zshrc" ;;
      bash) if [ "$os" = darwin ]; then rc="$HOME/.bash_profile"; else rc="$HOME/.bashrc"; fi ;;
      fish)
        rc="${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish"
        line="fish_add_path $INSTALL_DIR"
        ;;
      *) rc="" ;;
    esac
    say ""
    if [ -n "$rc" ]; then
      mkdir -p "$(dirname "$rc")"
      grep -qsF "$line" "$rc" || printf '\n%s\n' "$line" >>"$rc"
      say "Added $INSTALL_DIR to PATH in $rc. Open a new terminal, or run:"
      say "  $line"
    else
      say "Add $INSTALL_DIR to your PATH:  $line"
    fi
    ;;
esac
say ""
say "Get started:"
say "  tracex login     sign in through your browser"
say "  tracex init      link a repository to its TraceX project"
say "  tracex -c        ask TraceX about your data and code"
