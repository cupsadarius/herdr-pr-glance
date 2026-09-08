#!/bin/sh
# Herdr build hook: install the prebuilt herdr-pr-glance executable.
#
# Downloads the exact release matching the VERSION file next to the plugin
# manifest, verifies its SHA-256 checksum and installs it atomically as
# bin/herdr-pr-glance under the plugin root. It never edits the manifest,
# never falls back to "latest" and never runs at pane launch.
set -eu

REPO="cupsadarius/herdr-pr-glance"
BINARY="herdr-pr-glance"
MAX_ARCHIVE_BYTES=67108864 # 64 MiB ceiling on the downloaded archive

TMPDIR_WORK=""

cleanup() {
	if [ -n "$TMPDIR_WORK" ] && [ -d "$TMPDIR_WORK" ]; then
		rm -rf "$TMPDIR_WORK"
	fi
}
trap cleanup EXIT HUP INT TERM

fail() {
	echo "install.sh: $*" >&2
	exit 1
}

# The build hook runs with the plugin root as cwd, but resolve it from the
# script's own location so the installer is independent of the caller's cwd.
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd) || fail "cannot resolve script directory"
plugin_root=$(CDPATH='' cd -- "$script_dir/.." && pwd) || fail "cannot resolve plugin root"

version_file="$plugin_root/VERSION"
[ -f "$version_file" ] || fail "missing version file: $version_file"
version=$(tr -d ' \t\r\n' <"$version_file")
[ -n "$version" ] || fail "empty version file: $version_file"

uname_s=$(uname -s)
uname_m=$(uname -m)

case "$uname_s" in
Darwin) goos=darwin ;;
Linux) goos=linux ;;
*) fail "unsupported operating system: $uname_s (supported: Darwin, Linux)" ;;
esac

case "$uname_m" in
x86_64 | amd64) goarch=amd64 ;;
arm64 | aarch64) goarch=arm64 ;;
*) fail "unsupported architecture: $uname_m (supported: x86_64/amd64, arm64/aarch64)" ;;
esac

if command -v sha256sum >/dev/null 2>&1; then
	sha_cmd="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
	sha_cmd="shasum -a 256"
else
	fail "no SHA-256 tool found: install sha256sum or shasum"
fi

command -v gh >/dev/null 2>&1 || fail "gh is required to download release assets"
command -v tar >/dev/null 2>&1 || fail "tar is required to extract release assets"
# GNU tar shells out to gzip for -z; bsdtar decompresses in-process. Require it
# either way so the failure is a clear prerequisite error, not a tar child error.
command -v gzip >/dev/null 2>&1 || fail "gzip is required to extract release assets"

tag="v$version"
archive="${BINARY}_${version}_${goos}_${goarch}.tar.gz"

TMPDIR_WORK=$(mktemp -d) || fail "cannot create temporary directory"
download_dir="$TMPDIR_WORK/download"
extract_dir="$TMPDIR_WORK/extract"
mkdir -p "$download_dir" "$extract_dir" || fail "cannot create temporary directories"

echo "install.sh: downloading $archive from $REPO@$tag"
gh release download "$tag" -R "$REPO" -p "$archive" -p checksums.txt -D "$download_dir" ||
	fail "download of $archive from $REPO@$tag failed"

[ -f "$download_dir/$archive" ] || fail "release $tag is missing asset $archive"
[ -f "$download_dir/checksums.txt" ] || fail "release $tag is missing asset checksums.txt"

size=$(wc -c <"$download_dir/$archive" | tr -d ' ')
[ "$size" -gt 0 ] || fail "downloaded archive $archive is empty"
[ "$size" -le "$MAX_ARCHIVE_BYTES" ] ||
	fail "downloaded archive $archive is larger than $MAX_ARCHIVE_BYTES bytes"

expected=$(awk -v want="$archive" '
	{ name = $2; sub(/^\*/, "", name); if (name == want) { print $1; exit } }
' "$download_dir/checksums.txt")
[ -n "$expected" ] || fail "checksums.txt has no entry for $archive"

actual=$(cd "$download_dir" && $sha_cmd "$archive" | awk '{print $1}')
[ "$actual" = "$expected" ] ||
	fail "checksum mismatch for $archive (expected $expected, got $actual)"

tar -xzf "$download_dir/$archive" -C "$extract_dir" "$BINARY" ||
	fail "cannot extract $BINARY from $archive"
[ -f "$extract_dir/$BINARY" ] || fail "archive $archive does not contain $BINARY"

chmod 0755 "$extract_dir/$BINARY" || fail "cannot make $BINARY executable"

bin_dir="$plugin_root/bin"
mkdir -p "$bin_dir" || fail "cannot create $bin_dir"

# Stage inside the destination directory so the final move is an atomic rename
# on the same filesystem; any earlier failure leaves the old binary untouched.
staged="$bin_dir/.$BINARY.install.$$"
rm -f "$staged"
cp "$extract_dir/$BINARY" "$staged" || { rm -f "$staged"; fail "cannot stage $BINARY in $bin_dir"; }
chmod 0755 "$staged" || { rm -f "$staged"; fail "cannot make staged $BINARY executable"; }
mv -f "$staged" "$bin_dir/$BINARY" || { rm -f "$staged"; fail "cannot install $BINARY into $bin_dir"; }

echo "install.sh: installed $BINARY $version ($goos/$goarch) into $bin_dir"
