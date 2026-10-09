#!/usr/bin/env sh
#
# Installs the zig the build compiles the C sources with.
#
#	install-zig.sh <version> <directory>
#
# The caller passes a directory it caches, so a warm cache makes this a no-op. When it
# does download, the archive is checked against the digest ziglang.org publishes for
# it; the digests are pinned below rather than fetched beside the archive, because a
# checksum that arrives with the thing it checks proves nothing.
#
# A script rather than a third-party action, so that what CI installs can be read here
# and bumping the toolchain is a change in this repository.

set -eu

if [ "$#" -ne 2 ]; then
	echo "usage: install-zig.sh <version> <directory>" >&2
	exit 2
fi
version="$1"
directory="$2"

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=macos ;;
MINGW* | MSYS* | CYGWIN*) os=windows ;;
*)
	echo "no zig build is known for $(uname -s)" >&2
	exit 1
	;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch=x86_64 ;;
aarch64 | arm64) arch=aarch64 ;;
*)
	echo "no zig build is known for $(uname -m)" >&2
	exit 1
	;;
esac

# From https://ziglang.org/download/index.json: version, platform, sha256.
digests="
0.17.0 linux-x86_64 1cbe9df9f27e6b78d14ccbca43b6703a404ef79ef1c463de901d7f088d4e2026
0.17.0 linux-aarch64 9e8d11661d4ae3bd57702a3832781e23ad151dde5798e16a5ccd503f65234ff8
0.17.0 macos-x86_64 4f9a1c5269aa17ebda5e6d3c2b89d6cbf36f7d2b22a0306e9ab98f25f95529c6
0.17.0 macos-aarch64 b607e9b9234790a008116ae5bdb71c6243b84b9fb42a53a9e70fde41c06c536a
0.17.0 windows-x86_64 b5663f69581dcf391293fbf16c06cb80d81d806545ce618b4d0bab7f0eb8c428
"

expected=$(printf '%s\n' "$digests" | awk -v key="$version $os-$arch" '$1" "$2 == key { print $3 }')
if [ -z "$expected" ]; then
	echo "no digest is pinned for zig $version on $os-$arch; add one to this script" >&2
	exit 1
fi

binary="$directory/zig"
extension=".tar.xz"
if [ "$os" = windows ]; then
	binary="$binary.exe"
	extension=".zip"
fi

if [ -x "$binary" ] && [ "$("$binary" version)" = "$version" ]; then
	echo "zig $version is already in $directory"
	exit 0
fi

# zig names its archives "<arch>-<os>" while the digests below are keyed "<os>-<arch>",
# because that is how the table reads.
name="zig-$arch-$os-$version"
archive="$name$extension"
url="https://ziglang.org/download/$version/$archive"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

echo "downloading $url"
curl -fsSL "$url" -o "$work/$archive"

if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$work/$archive" | awk '{ print $1 }')
else
	actual=$(shasum -a 256 "$work/$archive" | awk '{ print $1 }')
fi
if [ "$actual" != "$expected" ]; then
	echo "$archive hashes to $actual, not the pinned $expected" >&2
	exit 1
fi

mkdir -p "$work/unpacked" "$(dirname "$directory")"
if [ "$extension" = .zip ]; then
	unzip -q "$work/$archive" -d "$work/unpacked"
else
	tar -xf "$work/$archive" -C "$work/unpacked"
fi
rm -rf "$directory"
mv "$work/unpacked/$name" "$directory"

echo "installed zig $("$binary" version) in $directory"
