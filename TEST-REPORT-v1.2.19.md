# AEGIS Suite v1.2.19 — test report (2026-11-07)

Companion **1.8.9** · PHOENIX **0.12.12** · suite **1.2.19**

## Answers to this round's questions
- **HEARTH art**: the flame is gone — new emblem (your pick of two):
  house + protective shield + Wi-Fi arcs, regenerated as an animated
  GIF. All other art unchanged.
- **Wi-Fi waves as the sensor (no mmWave hardware needed)**: yes —
  this is Wi-Fi CSI, and it was already in the suite; this round it
  became visible end-to-end. PHOENIX samples the ESP32's per-subcarrier
  channel state (how your router's signal ripples through the room)
  and now streams the live score to the Companion: the device tab
  shows "Wi-Fi sensing (CSI): MOTION nearby · score n/100" in real
  time. The PC does a coarser version itself (router RSSI ripple on
  the Dashboard's Home presence card). CSI needs no extra hardware —
  just the Wi-Fi radio — and gives presence/motion, honestly not
  imaging. The LD2410, when it arrives, adds still-person + distance
  precision on top; it is an upgrade, not a prerequisite.
- **C++ or C# rewrite**: not recommended. The Companion is one Go
  codebase producing the Linux and Windows apps from the same source,
  static binaries, stdlib-only system info, and the workload (scans,
  JSON, maps) is nowhere near Go's ceiling. C# would centre Windows
  (.NET/WinUI) and force a second UI for Linux; C++ (Qt) would double
  the UI work for no user-visible gain. The genuinely
  performance-critical code — the firmware — is already C++.

## What changed
- **System page** (new nav entry): This PC (host, OS, CPU model +
  threads, GPU, disk, uptime, library path), Live usage (CPU % and
  memory bars refreshed every few seconds, temperatures from hwmon
  on Linux; Windows temps honestly "not exposed"), Backups &
  failover, and Repo & recovery commands with Copy buttons.
- **Backups/failover**: settings saves are atomic (temp + rename —
  a power cut can't corrupt them) with 5 rotating snapshots beside
  the program; network census persists atomically too; one-click
  backup zip (settings + census + system summary) to
  `Library/Backups`. Second copy of logs remains the devices' SDs.
- **Update prompt**: a staged, verified Companion update now shows
  "Restart now to update / Later" — nothing restarts uninvited;
  Later snoozes until next launch.
- **Settings**: the eight-card family section is compacted to a
  one-line-per-program map (same info lives on Dashboard tabs).
- **PHOENIX 0.12.12**: live frames carry the CSI score + motion flag.

## Verification (sandbox)
- `gofmt` clean · `go vet` clean · `go test` PASS · selftest ALL PASS
  (direct + fake-HOME installed copy, 7 PASS lines)
- PHOENIX `pio run` SUCCESS, 0.12.12 verified via `strings` on the
  binary; firmware SHA-256
  `707c0242a6f115452efa8386be9039a5ec7681fb3802265cb92136c102b020ed`
- Suite setups: Linux `c03644865d94f245…`, Windows `6aa4cb3ad4bb9c0c…`

## Hardware-pending (carried)
CSI tab readout on the real Cardputer; System page on your Windows
box (GPU/RAM via CIM); update prompt flow on a real staged update;
LD2410/VL53L1X when ordered; previous rounds' LoRa/tray/HERALD list.
