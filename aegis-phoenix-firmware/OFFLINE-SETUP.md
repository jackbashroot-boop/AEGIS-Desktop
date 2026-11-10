# Offline / native setup — AEGIS PHOENIX v0.6.0

## Install on the Cardputer (no downloads at all)
`release/aegis-phoenix-adv.bin` is prebuilt. Copy it via M5Launcher from SD —
no compiler, no network, no host libraries needed. Verify first:
`sha256sum release/aegis-phoenix-adv.bin`
= `4c198134f8b75845bf863dacd3bd59a2facb2fee9744d632ae6a28cf8eaefa97`

## Build from source
Toolchain: PlatformIO Core with the exact versions locked in
`offline/DEPENDENCY-LOCK.md` (platform espressif32 ^6.6.0, board
m5stack-stamps3, Arduino framework, M5Cardputer ^1.0.3, ArduinoJson ^7.0.4,
NimBLE-Arduino ^2.1.0, RadioLib ^7.0.0 — resolved versions are written into
`offline/resolved-versions.txt` by the fetch script).

One machine with network runs the fetch ONCE:
- Linux/macOS: `bash tools/make_offline_bundle.sh`
- Windows (PowerShell): `powershell -ExecutionPolicy Bypass -File tools\make_offline_bundle.ps1`

It populates `vendor/platformio/` (PlatformIO package cache) and
`.pio/libdeps/` inside this project. Afterwards every build is offline:
`PLATFORMIO_CORE_DIR=$PWD/vendor/platformio pio run` (the scripts print the
exact per-OS command). Copy the whole project folder — including `vendor/` —
to any air-gapped machine with PlatformIO Core installed and it builds with
no network. The C5 companion in `aegis-c5-companion/` builds with arduino-cli
+ Arduino-ESP32 core 3.3.12; fetch it once with
`arduino-cli core install esp32:esp32@3.3.12` on the online machine and copy
`~/.arduino15` (Windows: `%LOCALAPPDATA%\Arduino15`) alongside.

Host-side logic tests need only a C++17 compiler:
`g++ -std=c++17 -I src tools/host_tests/test_key_fsm.cpp -o /tmp/t && /tmp/t`
