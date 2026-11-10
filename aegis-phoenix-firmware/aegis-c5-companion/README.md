# AEGIS ESP32-C5 companion — hot-swap 5 GHz coprocessor

This firmware runs on an ESP32-C5 board wired to the Cardputer ADV's
Grove port. Aegis (v0.4.3+) detects it automatically — no key presses:

1. Aegis probes Grove G1/G2 every 2.5 s with `AEGIS_HELLO`.
2. This firmware answers `+AEGIS:C5,aegis-c5-0.1.0,<mac>`.
3. Aegis immediately sends `AT+WIFI5G=1`, `AT+SNIFF5G=1`, `AT+SCAN5G=1`:
   5 GHz promiscuous deauth monitoring (channel-hopping) and periodic
   5 GHz AP scans start on their own.
4. Unplug the C5 and Aegis notices within ~15 s, marks the link
   offline, and resumes probing. Plug it back in and it re-detects.

## Wiring (3.3 V logic!)

| C5 board | Cardputer ADV |
|---|---|
| TX | Grove G1 (ADV RX) |
| RX | Grove G2 (ADV TX) |
| GND | Grove GND |

Power the C5 **separately** — its own USB-C charger or power bank is
fine; in this Option-A setup USB-C is power-only and carries no data.
Never connect 5 V to a GPIO. A Grove-to-jumper ("UART") cable is the
easy way to make this connection.

Set `LINK_RX_PIN`/`LINK_TX_PIN` in `src/main.cpp` to the C5 pins you
wired. On a XIAO ESP32C5, the D6/D7 pads are the usual UART pair —
check your board's pinout and set the GPIO numbers accordingly.

## Build & flash (Arduino core 3.x — required for ESP32-C5)

The sketch is `src/main.cpp`; copy it to `aegis_c5_companion.ino` in a
folder of the same name, or use Arduino IDE with the file renamed.

```bash
arduino-cli core install esp32:esp32
# Generic ESP32-C5 dev board:
arduino-cli compile --fqbn esp32:esp32:esp32c5 ./aegis_c5_companion
arduino-cli upload  --fqbn esp32:esp32:esp32c5 -p /dev/ttyACM0 ./aegis_c5_companion
# XIAO ESP32C5:
arduino-cli compile --fqbn esp32:esp32:XIAO_ESP32C5 ./aegis_c5_companion
```

Verified compiling against Arduino-ESP32 core 3.3.12 for both
`esp32:esp32:esp32c5` and `esp32:esp32:XIAO_ESP32C5`.

A dual-band antenna matters: a 2.4 GHz-only antenna will make 5 GHz
reception poor. Keep the C5's 5 GHz channel list in `kChannels5G`
within your local regulatory domain.

## Notes / honest limits

- Monitoring is receive-only: promiscuous listening + scans. The
  companion never transmits deauth or jamming frames.
- 5 GHz station join exists (`AT+CWJAP5G`) but Aegis does not send
  your Wi-Fi password to the companion automatically; joining is only
  via the optional console command.
- Grove G1 is shared with Aegis' optional IR-receiver learning. While
  C5 hot-swap is enabled in Aegis SETUP, the C5 owns G1/G2; disable
  "C5 hot-swap" in SETUP if you want IR learning back.
