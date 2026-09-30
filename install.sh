#!/bin/sh
set -eu

repository="artengin/claude-starred"
bin_dir="${STARRED_BIN_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "Unsupported OS: $(uname -s). Download a release from https://github.com/$repository/releases" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

archive="claude-starred_${os}_${arch}.tar.gz"
base_url="https://github.com/$repository/releases/latest/download"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT

curl -fsSL -o "$temporary/$archive" "$base_url/$archive"
curl -fsSL -o "$temporary/checksums.txt" "$base_url/checksums.txt"

expected="$(grep " $archive\$" "$temporary/checksums.txt" | cut -d ' ' -f 1)"
actual="$( (sha256sum "$temporary/$archive" 2>/dev/null || shasum -a 256 "$temporary/$archive") | cut -d ' ' -f 1)"

if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "Checksum mismatch for $archive" >&2
  exit 1
fi

tar -xzf "$temporary/$archive" -C "$temporary" claude-starred
mkdir -p "$bin_dir"
mv "$temporary/claude-starred" "$bin_dir/claude-starred"
"$bin_dir/claude-starred" install

case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *) echo "Add $bin_dir to your PATH." ;;
esac
