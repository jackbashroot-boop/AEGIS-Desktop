# AEGIS PHOENIX — Cardputer ADV Firmware (real build)

The first actually-compiled `aegis-phoenix-adv.bin`. Evolved from the OLD
PHOENIX agent (`agent_core.h`, `rules.h`, drivers) with the fictional parts
removed or made honest, plus a native AES-256-GCM forensic vault.

## Status - v0.7.0 (PCAP evidence, WiGLE export, new detectors, driver registry)

New in v0.7.0: PCAP capture to SD (`AEGIS PCAP ON`, Wireshark-readable, 8 MB cap), WiGLE wardrive CSV export, beacon-spam + Karma-suspect detectors, BLE skimmer fingerprinting, per-channel density (`AEGIS CHAN`), hash-chained event index, GNSS trusted-time stamps, `drivers/manifest.json` + `tools/update_drivers.py` driver registry/updater.

## Status - v0.6.0 (freeze hardening, IR auto-find, map export)

- **Function-key freeze hardening:** LAN scan is asynchronous and cancellable;
  the LoRa channel sweep advances one step at a time; stale queued keys are
  flushed after the synchronous integrity sweep.
- **Rule Manager:** MODULES -> Rule manager can enable/disable armed rules;
  disabled codes persist in `/AEGIS/rules/disabled.txt`.
- **IR auto-find:** built-in Samsung, LG, Sony, and Philips code sets cycle
  automatically in Remote; Enter saves the responding brand. `C` cycles
  brands manually.
- **Wardrive mapping:** Atlas stores GPS positions when available; pressing
  `M` in Atlas exports GeoJSON, KML, CSV, and GPX track files under
  `/AEGIS/data/`.
- **USB serial export:** `AEGIS HELP`, `AEGIS STATUS`, `AEGIS EVENTS`,
  `AEGIS RULES`, and `AEGIS EXPORT MAP` are available over USB CDC.
- **LoRa classification:** received traffic is labelled LoRaWAN-shaped,
  `LoRa/Meshtastic?`, or raw; encrypted payloads are not falsely decoded.
- **Optional RFID2:** an M5 RFID Unit 2 (WS1850S) at I2C `0x28` is polled
  read-only for ISO14443-A UIDs and remains inert when absent.
- **Release state:** v0.6.0 has passed the recorded build, host-test, and
  static-analysis gates, but the key-mash and peripheral checks still require
  confirmation on real Cardputer ADV hardware.
- Emulator/red-team handoff material is preserved separately in
  `/home/user/aegis-preserved/` when that directory is present.

## Status - v0.5.0 (rule packs, Event Center, Atlas explorer, QoL)

- **SD rule packs:** every `/AEGIS/rules/*.json` file is parsed at boot;
  only rules with a code, a kind, and an expression that passes the
  fuzz-hardened parser are armed (bad rules are counted, never loaded).
  `sdcard/AEGIS/rules/default_rules.json` is a starter pack. Reload any
  time from SETUP -> "Reload rule packs" (no reboot).
- **Event Center (key 3):** newest 64 events, summaries persisted to
  `/AEGIS/data/event_index.jsonl` so they survive reboot (details stay
  in the encrypted vault). `F` filters ALL/WIFI/BLE/LORA-GNSS/
  UNREVIEWED; Enter expands; Enter again marks reviewed (persisted).
  Events carry real NTP timestamps when Wi-Fi was online.
- **Log viewer (key 0):** tail of boot/integrity/net-scan/C5 logs,
  arrows scroll, `F` switches file. Logs rotate at ~512 KB (`.1` kept).
- **Atlas explorer (NET -> Atlas explorer):** browse every remembered
  network - channels seen, best RSSI, sightings, GPS of best sighting -
  with wardrive kept-record count. Enter expands a network.
- **QoL:** SETUP controls brightness (20-100%) and optional screen
  sleep (never/30/60/120 s; detectors keep running while dark, first
  keypress only wakes). High-confidence events raise a 3 s red banner
  plus an optional beep (SETUP, key `B` toggles the beep). Settings
  persist in NVS.
- **Splash:** animated vector phoenix - feather scallops, crest and
  tail flames; embers rise yellow->orange->red, cool to ice blue, and
  snowflakes drift down around the bird. Any key skips.

## Status — Phase 0/1 (previous build)

Working / compiled-in:
- M5Cardputer ADV display + keyboard (`s` = scan now, `q` = quarantine toggle)
- 802.11 promiscuous **deauth/disassoc detector** (with short-frame guard)
- **Evil-twin / rogue-AP scan** with a silent first-scan baseline (no longer
  poisons its own baseline), BLE random-address persistence tracker
- **Rule engine** (`rules.h`) — console-compatible grammar, now rejects
  truncated/invalid expressions at load (`Rule::parses` is fuzz-hardened)
- **AES-256-GCM forensic vault**: events are encrypted (PBKDF2-HMAC-SHA256,
  60k iterations, `AES256GCMv1:<salt>:<nonce>:<ct||tag>`) before being written
  to `/AEGIS/logs/forensic_vault.log`; decryptable on read/export with the
  passphrase from config. Plaintext fallback is explicitly marked
  `[VAULT-UNAVAILABLE]`, never silent.
- **Boot self-tests** on every power-up (visible on serial @115200 and in the
  boot flow): rule-parser valid/invalid cases, AES round-trip, and
  tamper-rejection. A vault fault disables the vault rather than logging
  garbage.
- Optional PHOENIX console link (enrol / signed manifest / verified module
  install / OTA) — activates only if `console_url` is set in config.
  **Detection is fully autonomous without it.**
- SD layout: `/AEGIS/{logs,downloads,rules,art,standalone}` auto-created.

Cap LoRa-1262 (auto-detected; inert and honestly "inactive" without it):
- **LoRa SX1262 — REAL driver** (RadioLib): NSS G5/RST G3/BUSY G6/DIO1 G4 on
  the shared SD SPI bus, TCXO 1.8 V, DIO2 RF switch + PI4IOE P0 antenna-switch
  attempt, continuous interrupt-driven RX with RSSI/SNR capture to
  `/AEGIS/logs/lora_hat.log`, noise-floor/jam indication. RX-only by design;
  915 MHz default (US) — set `AEGIS_LORA_FREQ_MHZ` for EU868.
- **GNSS ATGM336H — REAL NMEA parser**: checksum-validated GGA/RMC,
  lat/lon/alt/HDOP/sats/speed/course/UTC, stale-fix expiry, and an
  impossible-jump spoof detector feeding `gnss_spoof` events. UART1
  RX=G15/TX=G13 @115200 (per M5 pin table; swap if silent).
ESP32-C5 5 GHz hot-swap (v0.4.3, Option A): `AEGIS_ENABLE_C5=1` runs a
non-blocking link manager on Grove G1/G2. It probes every 2.5 s, and
when the companion firmware in `aegis-c5-companion/` answers, it
auto-starts 5 GHz promiscuous deauth monitoring + AP scans and feeds
5 GHz events into the same EVENTS/vault pipeline. Unplug it and the
link drops cleanly; replug and it re-detects. The C5 is powered
separately (USB-C is power-only in this option). SETUP can disable
hot-swap, which frees G1 for the optional IR receiver. A future
Option B (USB-host CDC through a powered hub) is not yet implemented.

Known honest limitations of this phase:
- BLE HID manager *detects* HID advertisers; it does not pair/bond (the old
  "discovered and paired" log line was false and has been corrected).
- The C5 link uses the documented line protocol in `esp32c5_driver.h`;
  only a board running `aegis-c5-companion/` (or speaking the same
  protocol) is ever believed - UART noise cannot fake a detection.
- EAPOL/KRACK heuristic uses fixed frame offsets; treat as experimental.

## Build (CachyOS / fish)

```fish
sudo pacman -S --needed platformio    # or: pipx install platformio
cd aegis-phoenix-firmware
pio run
cp .pio/build/cardputer-adv/firmware.bin release/aegis-phoenix-adv.bin
sha256sum release/aegis-phoenix-adv.bin
```

Expected: build SUCCESS, 0 warnings from `src/`, ~1.41 MB, ~42% flash.

## Install (M5 Launcher — already flashed on the device)

1. Edit `sdcard/AEGIS/config.json`: your **2.4 GHz** SSID/password and a
   strong `vault_passphrase` (min. 8 chars). Leave `console_url` empty for
   autonomous mode.
2. Copy `config.json` to the SD card at `/AEGIS/config.json`, and copy
   `release/aegis-phoenix-adv.bin` to the SD card **root** (Launcher browser).
3. Card into ADV → power on → Launcher → Files → `aegis-phoenix-adv.bin` →
   Install.
4. On boot, watch for (serial 115200 or screen flow):
   `[SELFTEST] rules ...=1 ...=1`, `[SELFTEST] AES-256-GCM roundtrip=1
   tamper-rejected=1`, `[VAULT] AES-256-GCM forensic vault ready`,
   `[APS] baseline learned: N known BSSIDs`.
5. Return to Launcher any time: power cycle, press Enter at the splash.

## Verify the artifact before flashing (fish)

```fish
set BIN ~/aegis-phoenix-firmware/release/aegis-phoenix-adv.bin
sha256sum $BIN
dd if=$BIN bs=1 skip=32768 count=2 status=none | xxd -p   # must NOT be aa50
```

## Roadmap to the full suite

- Phase 2: on-device 240×135 multi-screen UI (MON/HEAL/LOG/CFG/CAP), SD
  rule-pack loading from `/AEGIS/rules/*.json`, forensic vault viewer/export,
  log rotation, FreeRTOS task split (radios vs UI) to remove scan blocking.
- Phase 3: Phoenix Rebirth via ESP32 OTA boot-partition switch (the honest
  version of "golden_recovery rollback"), IR TX (GPIO44), USB-HID auditing.
- Phase C5: ESP32-C5 companion firmware implementing the UART protocol,
  5 GHz + 802.15.4 detectors. Requires the C5 hardware + antenna.
- Phase RF: CC1101 sub-GHz. Requires the module.
