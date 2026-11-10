# AEGIS Suite v1.2.20 — test report (2026-11-07)

Companion **1.8.9** (unchanged) · PHOENIX **0.12.13** · suite **1.2.20**

## The "stuck at 8.3" report — root cause found
The Cardputer was never failing to update. A legacy console-link
label, `AGENT_VERSION "aegis-0.8.3"`, had not been bumped in many
releases while the real firmware version marched on separately
(`FW_VER_NUM`). The 0.8.3 string shows in the serial banner and
console telemetry, so successful flashes looked like nothing changed.

Fix: `AGENT_VERSION` is now **derived** from the real version
(`"aegis-" FW_VER_NUM`) so the two can never drift apart again.
Verified in the compiled binary: both `AEGIS-PHX-v0.12.13-ADV` and
`aegis-0.12.13` are present. Firmware SHA-256
`1e1ae3d3191f19f7d936399b149e5b8e12f02a5902e0b7522bade5535eb2783e`.
To confirm any flash landed, the device's SETUP / About page
("Firmware: PHOENIX v0.12.13") is the source of truth.

## Also reported: Companion live data frozen
No live-path code changed in 1.8.8/1.8.9, so this is environmental
until proven otherwise. The 2-minute checklist is in the release
message: sidebar Live now + Listening boxes, the amber reject notice
on the Dashboard, SETUP's WiFi/passive rows on the device, and the
one-click SD link rewrite (Settings) cover every known cause
(token drift after reinstall, passive-offline, link approval waiting
on the device, an old tray instance holding the port).

## Verification (sandbox)
- `pio run -e cardputer-adv` SUCCESS (251 s cold); strings verified.
- Suite installers rebuilt around the unchanged, previously verified
  Companion 1.8.9 payload; suite bundle SHA-256
  `91c14dca4dfe12f47a51fc4302d0c1e413a3693dc9dcdbfc3fe7218dc8c12bc3`.
