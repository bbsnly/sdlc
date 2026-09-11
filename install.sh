#!/bin/sh
# Install sdlc.
#
#   curl -fsSL https://raw.githubusercontent.com/bbsnly/sdlc/main/install.sh | sh
#
# Downloads the release archive for this machine, checks it against the
# release's checksums.txt, and puts a single static binary on your PATH.
# Nothing else is installed and nothing outside the install directory is
# touched.
#
#   --version X.Y.Z   install that version instead of the latest
#   --dir PATH        install somewhere other than ~/.local/bin
#   --help
#
# The same settings can come from the environment, which is easier when the
# script is piped into sh: SDLC_VERSION and SDLC_INSTALL_DIR.
set -eu

repo=bbsnly/sdlc
version="${SDLC_VERSION:-}"
dir="${SDLC_INSTALL_DIR:-$HOME/.local/bin}"

die() {
  echo "sdlc: $1" >&2
  shift
  for line in "$@"; do echo "  $line" >&2; done
  exit 1
}

# Spelled out rather than read back out of this file: the usual way to run
# this script is to pipe it into sh, where the file is not on disk to read.
usage() {
  cat <<'USAGE'
Install sdlc, the story-driven delivery loop for Claude Code.

  install.sh [--version X.Y.Z] [--dir PATH]

  --version X.Y.Z   install that version instead of the latest
  --dir PATH        install somewhere other than ~/.local/bin
  -h, --help        this

The same settings can come from the environment, which is easier when this
script is piped into sh: SDLC_VERSION and SDLC_INSTALL_DIR. SDLC_DOWNLOAD_BASE
downloads from somewhere other than GitHub.
USAGE
  exit "${1:-0}"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version) [ $# -ge 2 ] || die "--version needs a version, like --version 0.1.0"; version="$2"; shift 2 ;;
    --version=*) version="${1#--version=}"; shift ;;
    --dir) [ $# -ge 2 ] || die "--dir needs a directory"; dir="$2"; shift 2 ;;
    --dir=*) dir="${1#--dir=}"; shift ;;
    -h|--help) usage 0 ;;
    *) echo "sdlc: unknown option $1" >&2; usage 1 ;;
  esac
done

# A leading v is what the tag looks like, and it is the natural thing to
# paste; everything downstream wants it without.
version="${version#v}"

command -v curl >/dev/null 2>&1 || die \
  "curl is not installed, and this script downloads a release with it." \
  "macOS:  curl ships with the system" \
  "Linux:  apt-get install curl   (or: dnf install curl)"

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *)
    die "no prebuilt binary for $(uname -s)." \
      "Build from source instead:" \
      "  go install github.com/$repo/cmd/sdlc@latest"
    ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *)
    die "no prebuilt binary for $(uname -m)." \
      "Build from source instead:" \
      "  go install github.com/$repo/cmd/sdlc@latest"
    ;;
esac

if [ -z "$version" ]; then
  # Resolve "latest" through the redirect rather than the API: the API is rate
  # limited per IP, and a shared network can exhaust it for everyone on it.
  latest=$(curl -fsSL -o /dev/null -w '%{url_effective}' \
    "https://github.com/$repo/releases/latest" 2>/dev/null) || latest=""
  version="${latest##*/tag/v}"
  # The same shape the other two installers require: a version starts with a
  # digit. Without it a redirect to anything but a tag yields a plausible
  # string that goes straight into a URL.
  case "$version" in
    "" | *"/"* | *"releases"* | [!0-9]*)
      die "could not work out the latest version of sdlc." \
        "why  https://github.com/$repo/releases/latest did not redirect to a tag;" \
        "     the usual cause is no network, or no release yet" \
        "fix  pass one: install.sh --version X.Y.Z"
      ;;
  esac
fi

archive="sdlc_${version}_${os}_${arch}.tar.gz"
# SDLC_DOWNLOAD_BASE points this at somewhere other than GitHub. It exists so
# that this script can be tested against a local mirror on every commit,
# rather than only by a real release.
base="${SDLC_DOWNLOAD_BASE:-https://github.com/$repo/releases/download}/v${version}"

tmp=$(mktemp -d)
# incoming is the staging name inside the install directory, set once the
# target is known. Both are cleaned up, and the INT and TERM traps exit: a
# handler that only tidies up lets the script carry on past a Ctrl-C, against
# a temporary directory it has just deleted.
incoming=""
cleanup() {
  rm -rf "$tmp"
  [ -z "$incoming" ] || rm -f "$incoming"
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM

echo "sdlc: downloading $archive"
# --retry-all-errors is deliberately not used here: it needs curl 7.71, and
# RHEL 8, CentOS 7 and Ubuntu 20.04 ship older ones that exit at option
# parsing rather than running. -f already means a 404 is not retried, and
# --retry covers the transient and 5xx cases on its own.
curl -fsSL --retry 3 --connect-timeout 20 \
  -o "$tmp/$archive" "$base/$archive" || die \
  "could not download $archive" \
  "why  $base/$archive did not answer with the file" \
  "fix  check that v$version is released, and that this machine can reach github.com"

curl -fsSL --retry 3 --connect-timeout 20 \
  -o "$tmp/checksums.txt" "$base/checksums.txt" || die \
  "could not download the checksums for v$version" \
  "why  $base/checksums.txt did not answer with the file" \
  "fix  the release is incomplete; report it at https://github.com/$repo/issues"

# Verify before anything is made executable or moved onto a PATH directory.
# A release you cannot check is a release you should not install.
expected=$(tr -d '\r' < "$tmp/checksums.txt" |
  awk -v want="$archive" '$2 == want || $2 == "*" want { print $1 }')
[ -n "$expected" ] || die \
  "checksums.txt does not list $archive" \
  "why  the release is missing the build for this platform" \
  "fix  report it at https://github.com/$repo/issues"

if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
elif command -v openssl >/dev/null 2>&1; then
  actual=$(openssl dgst -sha256 "$tmp/$archive" | awk '{print $NF}')
else
  die "no sha256 tool found (sha256sum, shasum or openssl)." \
    "why  this script refuses to install a binary it cannot verify" \
    "fix  install one of those, or download the archive by hand from" \
    "     https://github.com/$repo/releases/tag/v$version"
fi

if [ "$actual" != "$expected" ]; then
  die "the download does not match its checksum, so it is not being installed." \
    "expected  $expected" \
    "got       $actual" \
    "This is either a corrupted download or something worse. Try again; if it" \
    "happens twice, report it at https://github.com/$repo/issues"
fi

tar -xzf "$tmp/$archive" -C "$tmp" sdlc || die "could not unpack $archive"

mkdir -p "$dir" || die "could not create $dir"
# Install through a temporary name in the same directory and rename: a half
# written binary on PATH is worse than no binary on PATH, and rename is the
# only step that is atomic.
chmod 0755 "$tmp/sdlc"
incoming="$dir/.sdlc.incoming.$$"
mv "$tmp/sdlc" "$incoming" || die "could not write to $dir"
mv "$incoming" "$dir/sdlc" || die "could not install into $dir"
incoming=""

echo "sdlc: installed $("$dir/sdlc" version 2>/dev/null || echo "v$version") in $dir"

case ":$PATH:" in
  *":$dir:"*) ;;
  *)
    echo
    echo "  $dir is not on your PATH. Add it:"
    echo
    echo "    export PATH=\"$dir:\$PATH\""
    echo
    echo "  and put that line in your shell profile (~/.zshrc, ~/.bashrc) to keep it."
    ;;
esac

cat <<'NEXT'

Next, in Claude Code:

  /plugin marketplace add bbsnly/sdlc
  /plugin install sdlc@sdlc

Then run `sdlc doctor` in a project to check the install.
NEXT
