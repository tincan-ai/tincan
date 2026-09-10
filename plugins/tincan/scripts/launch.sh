#!/bin/sh
# Select only an executable shipped with this plugin. Never download or compile.
set -eu
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
plugin_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
case "$(uname -s)" in
    Darwin) system=darwin ;;
    Linux) system=linux ;;
    MINGW*|MSYS*|CYGWIN*) exec "$plugin_dir/bin/tincan.exe" "$@" ;;
    *) printf '%s\n' 'Tincan does not support this operating system.' >&2; exit 126 ;;
esac
case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) printf '%s\n' 'Tincan requires an Intel/AMD 64-bit or ARM64 machine.' >&2; exit 126 ;;
esac
# A translated shell on Apple Silicon should still launch the native binary.
if [ "$system" = darwin ] && [ "$arch" = amd64 ]; then
    if [ "$(/usr/sbin/sysctl -n hw.optional.arm64 2>/dev/null || :)" = 1 ]; then arch=arm64; fi
fi
binary="$plugin_dir/bin/$system-$arch/tincan"
if [ ! -x "$binary" ]; then
    printf '%s\n' "Tincan's bundled executable is missing or not executable ($system-$arch). Reinstall the plugin from the Tincan marketplace." >&2
    exit 126
fi
exec "$binary" "$@"
