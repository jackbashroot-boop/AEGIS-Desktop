# AEGIS Desktop — family suite

One PC hub (**AEGIS Companion**) + lean Cardputer/CYD device programs
that find it on the home Wi-Fi, prove themselves with an HMAC
handshake (approved on the device screen), sync their logs, stream
live status, and file plain-English, self-diagnosed reports.

| Part | Where | What |
|---|---|---|
| `companion/` | PC (Go + Gio) | Sync server, live dashboard, wardrive maps (offline packs), reports, updater/uninstaller |
| `installer/` | PC (Go, stdlib) | Self-verifying suite installer → `Aegis-Suite-Setup-*` |
| `firmware/phoenix` | Cardputer ADV | Blue-team Sentinel (wardrive, passive scans, get-home sync) |
| `firmware/scribe` | Cardputer ADV | Hash-chained field notes |
| `firmware/hearth` | Cardputer ADV | Home Sentinel |
| `firmware/herald` | CYD ESP32-2432S028 | Desk display Sentinel (custom 3 MB app partition) |
| `firmware/keeper` | Cardputer ADV | Radio-off vault display |
| `firmware/bench` | Cardputer ADV | Wired bench tool |
| `firmware/lantern` | Cardputer ADV | LoRa/GNSS companion |
| `tools/` | PC | Host ASan/UBSan fuzz harness for the firmware hash-chain core |

**Not here, by design:** AEGIS TALON (red team) is a separate private
program — never bundled, linked, synced, or uploaded. No secrets live
in this repo: device tokens are generated per-install on your own PC.

## Install (users)
Download the latest **Release**: `Aegis-Suite-Setup-Linux` or
`Aegis-Suite-Setup-Windows.exe`, run it. It verifies its own payload,
installs per-user (no admin), adds a menu shortcut, and bundles every
device package under `Suite/`. Uninstall from the Companion's
Settings → *Updates & removal* (Library is backed up, never deleted).

## Build
- Companion: `cd companion && go build ./...` (Go 1.24+; `go mod vendor` for offline)
- Suite installers: `python3 build_installers.py` (needs the release zips in `../DOWNLOADS` layout — see script header)
- Firmware: PlatformIO, per-folder `platformio.ini` (`pio run`); PHOENIX wants `-j 1` on small machines
- Flashing: see `docs/FLASHING-GUIDE.txt`

## Updates
Releases carry the suite installers + `latest.json`
(`{"version": "x.y.z", "file": "Aegis-Suite-Setup-...", "notes": "..."}`),
which is exactly what the Companion's *Check for updates* consumes.
