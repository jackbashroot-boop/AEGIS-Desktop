# AEGIS Suite v1.2.17 — test report (Companion 1.8.7 / PHOENIX 0.12.10)

User-requested feature round: passive/startup Wi-Fi clarity, Wi-Fi
location scans, SD-card library handoff, Meshtastic/MeshCore
sharing, firmware streamlining. (One pages_map.go file took
edit-tool damage mid-round; it was restored from the v1.2.16 repo
zip and the changes re-applied singly with gofmt checks.)

## What shipped
- **Passive/startup (PHOENIX):** boot logs + SD boot log now state
  `ssid=set/EMPTY passive=ON/off live=… autoSync=…`; if passive
  offline is ON, the status line says so in plain English
  ("WiFi will not join (SETUP > Passive offline)"). Passive
  persisted ON across a shutdown previously looked exactly like a
  Wi-Fi fault. SD template does NOT set it (checked).
- **Wi-Fi location scans (Companion):** no-GPS sighting groups
  (~20 s windows) are positioned from GPS-mapped APs via RSSI
  path-loss-weighted centroid; groups need ≥2 known APs, scattered
  groups rejected; honest ± radius (25–900 m clamps). Shown as
  amber estimates on schematic/real/browser maps (dashed in the
  browser), stat card "WI-FI POSITION EST.", readiness line.
  Works on existing wardrive.jsonl (ts-grouped) — no re-drive
  needed. Estimates never contaminate AP positions.
- **SD library (both):** PHOENIX writes /AEGIS/data/library.json
  (prog, fw, MAC, every sync file's byte size) before each sync
  and syncs it first; Companion readiness report summarises
  manifests found in the Library. Directories auto-created.
- **Meshtastic/MeshCore sharing (Companion):** Map page "Share"
  button → Library/exports/{wardrive-waypoints.gpx,
  wardrive.geojson (points + GPS route + Wi-Fi estimates),
  wardrive-aps.csv, README-share.txt}. Honest note included and
  in code: LoRa protocols/sync words differ, files are the
  bridge; no fake on-air interop.
- **Streamlining (PHOENIX):** CPU already 80 MHz; added idle
  1 ms loop yield when no key is pressed (hands time to the
  Wi-Fi/BT stacks; less heat/battery, same responsiveness).
  Everything else already census-gated. Dependencies: firmware
  lib_deps pinned by major in platformio.ini; Companion is Go
  modules — nothing else to install natively.

## Verification
- go vet clean, gofmt clean, full `go test ./` PASS (new
  TestEstimateWiFiPositions, TestShareExports).
- PHOENIX `pio run -e cardputer-adv` SUCCESS (0.12.10,
  fw SHA-256 4dec538875ce5644…).
- Fake-HOME suite install PASS; installed 1.8.7 `--selftest`
  ALL PASS.

## Still needs real hardware
- Boot with passive ON → status line/DIAG call it out; join test.
- Wardrive somewhere with patchy GPS → amber Wi-Fi estimates
  appear for the no-fix stretches after sync.
- library.json lands in Library after sync; readiness shows it.
- Share export opens in Meshtastic/MeshCore tooling.
- (Carried) two-device LoRa beacon RSSI; wardrive-ON auto-join.
