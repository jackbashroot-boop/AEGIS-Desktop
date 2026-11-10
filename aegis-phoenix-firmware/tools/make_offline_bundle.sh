#!/bin/sh
# One-time ONLINE fetch: vendor every PlatformIO package + library this
# project needs, so later builds run fully offline. Re-run to update.
set -eu
cd "$(dirname "$0")/.."
export PLATFORMIO_CORE_DIR="$PWD/vendor/platformio"
pio run -e cardputer-adv
pio pkg list -e cardputer-adv > offline/resolved-versions.txt || true
echo "Offline bundle ready. Build later with:"
echo "  PLATFORMIO_CORE_DIR=\$PWD/vendor/platformio pio run"
