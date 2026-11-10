# AEGIS family connectivity audit (2026-11-07)

Question asked: can the other suite programs actually get their data
to the desktop, over Wi-Fi and/or Bluetooth where the hardware supports it?

## How the desktop link works (all programs)
- Transport is **Wi-Fi / TCP** to AEGIS Companion (default :8791),
  HMAC-SHA256 challenge-response, files SHA-256-verified on receipt.
- Discovery: device broadcasts `AEGIS_DISCOVER`, Companion answers
  with an HMAC proof; a written `/AEGIS_LINK/desktop.txt` also works.
- **No program links to the desktop over Bluetooth.** BLE in this
  family is a *detection* sensor (nearby advertisers), not a data
  pipe. The Companion has no BLE listener. If a Bluetooth link is
  ever wanted, it is a new feature, not a fix.

## Per program
| Program | Wi-Fi join | Desktop sync | Desktop live | Bluetooth |
|---|---|---|---|---|
| PHOENIX 0.9.9 | yes (auto-join fixed v0.9.9) | yes | yes (device must approve PC once) | BLE scan = detection |
| SCRIBE 0.1.7 | yes | yes | yes | - |
| HEARTH 0.1.6 | yes | yes | yes | BLE scan = detection |
| HERALD 0.1.5 | yes (retry-after-failed-join fixed v0.1.5) | yes | yes | - (ESP32 CYD) |
| BENCH 0.1.3 | yes | **yes (new v0.1.3)** | no (bench tool) | - |
| LANTERN 0.1.3 | yes | **yes (new v0.1.3)** | no | LoRa RX + GNSS instead |
| KEEPER 0.1.3 | no - radio OFF by design | SD hand-off only | no | no |
| TALON | never linked, by design | never | never | never |

## Fixes made in this pass
1. PHOENIX: saved-network auto-join was defeated by the background
   scan restarting every ~20 s mid-join (radio stolen before
   association completed). Joins now hold a 20 s protected window;
   SETUP shows AUTO-JOINING / CONNECTED / why-not, retry every 30 s.
2. HERALD: a failed first join at boot was never retried
   (`if (!g_wifiJoined) return;`). Now retries with backoff.
3. BENCH + LANTERN: previously joined Wi-Fi but sent nothing. Both
   now carry the family desktop link (discovery + auto-sync of
   battlog.csv / track.csv + waypoints.csv).
4. Companion updater (1.7.1): checks the AEGIS-Desktop release feed
   (private repo = 404 anonymously; Settings takes a read-only token).

## Intentionally offline
- KEEPER: radio-off vault. Data moves by SD only, on purpose.
- TALON: never linked to the Companion, by design.
