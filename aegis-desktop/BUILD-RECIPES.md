# AEGIS build recipes — restoring the toolchains from scratch

Everything below is retrievable from the public internet + this repo;
no vendored toolchain zips are kept in the workspace (they were the
bulk of its size). Verified 2026-11 with Companion 1.8.8 / PHOENIX
0.12.11 / suite 1.2.18.

## Companion (Go 1.24 + Gio, CGO)

```sh
# Go toolchain into the workspace (not system-wide)
mkdir -p /home/user/.goroot
curl -sL https://go.dev/dl/go1.24.0.linux-amd64.tar.gz | tar -xz -C /home/user/.goroot --strip-components=1
export PATH=/home/user/.goroot/bin:$PATH
export GOPATH=/home/user/.buildtmp/gopath GOCACHE=/home/user/.buildtmp/gocache

# X11/Wayland headers without root: download debs, extract a sysroot
mkdir -p /home/user/.buildtmp/debs /home/user/.buildtmp/sysroot
cd /home/user/.buildtmp/debs
apt-get download libx11-dev libxkbcommon-dev libwayland-dev libegl-dev libgles-dev libxcursor-dev libxfixes-dev libxinerama-dev libxxf86vm-dev libvulkan-dev mesa-common-dev libgl-dev libxkbcommon-x11-dev libxcb1-dev libx11-xcb-dev libxcb-xkb-dev libegl1 libwayland-client0 libwayland-cursor0 libwayland-egl1 libxkbcommon0 libxkbcommon-x11-0 libx11-6 libx11-xcb1 libxcb1 libxcursor1 libxfixes3 libxinerama1 libxxf86vm1 libgles2 libvulkan1 libxcb-xkb1
for d in *.deb; do dpkg -x "$d" /home/user/.buildtmp/sysroot; done
export PKG_CONFIG_SYSROOT_DIR=/home/user/.buildtmp/sysroot
export PKG_CONFIG_PATH=/home/user/.buildtmp/sysroot/usr/lib/x86_64-linux-gnu/pkgconfig:/home/user/.buildtmp/sysroot/usr/share/pkgconfig
export CGO_CFLAGS="-I/home/user/.buildtmp/sysroot/usr/include"
export CGO_LDFLAGS="-L/home/user/.buildtmp/sysroot/usr/lib/x86_64-linux-gnu -lxcb-xkb"
export LD_LIBRARY_PATH=/home/user/.buildtmp/sysroot/usr/lib/x86_64-linux-gnu  # for running the built binary here

cd /home/user/aegis-desktop/app
go mod download
gofmt -l .
go vet ./...
go test ./
CGO_ENABLED=1 go build -o aegis-companion .
./aegis-companion --version
./aegis-companion --selftest
```

Notes: the sandbox /tmp is a small tmpfs — keep GOPATH/GOCACHE under
/home/user. The Go module cache lands read-only; `chmod -R u+w`
before deleting it.

## PHOENIX firmware (PlatformIO, ESP32-S3)

```sh
python3 -m venv /tmp/pioenv
/tmp/pioenv/bin/pip install -q platformio
export PLATFORMIO_CORE_DIR=/home/user/.piocore
cd /home/user/aegis-phoenix-firmware
/tmp/pioenv/bin/pio run -e cardputer-adv      # sequential, not parallel
# artifact: .pio/build/cardputer-adv/firmware.bin
```

lib_deps are pinned in platformio.ini (M5Cardputer, ArduinoJson,
NimBLE-Arduino, RadioLib) and fetched by PlatformIO itself.

## Packaging

Suite setups: `python3 /home/user/aegis-desktop/build_installers.py`
(bump its version constants first). PHOENIX SD zip + Companion zip:
see the current TEST-REPORT-<version>.md for the exact layout.

Cleanup after a release: remove `.goroot`, `.piocore`, `.buildtmp`,
`.pio` — all re-creatable from this file in ~10 minutes.
