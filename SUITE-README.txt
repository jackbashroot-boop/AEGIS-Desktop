====================================================================
  AEGIS FAMILY SUITE - what's inside, and what goes where
  Suite v1.2.4 - bundles Companion v1.7.5
====================================================================

THE PC PROGRAM
  AegisCompanion (.exe on Windows) - installed into your apps /
  Start Menu by the suite installer. Everything syncs to it: get
  home, your Cardputers find it on the home Wi-Fi by themselves,
  hand over their logs (every file SHA-256-verified), and stream
  live status while they work. It files, verifies hash chains,
  maps wardrives (online or fully offline with saved map packs)
  and writes plain-English reports.

THE DEVICE PROGRAMS (Suite/ folder - one package per program)
  PHOENIX v0.10.0  - Cardputer ADV. Blue-team Sentinel: wardriving,
                    passive scans, get-home sync. The collector.
  SCRIBE  v0.1.7  - Cardputer ADV. Field notebook; hash-chained,
                    tamper-evident notes; syncs at home.
  HEARTH  v0.1.6  - Cardputer ADV. Home sentinel; watches your own
                    network, syncs at home.
  HERALD  v0.1.5  - CYD (ESP32-2432S028) desk display. Shows what
                    the family is doing. v0.1.5 note: flash its
                    partitions.bin AND the app (see its README).
  KEEPER  v0.1.3  - Cardputer ADV. Radio-off vault display.
  BENCH   v0.1.3  - Cardputer ADV. Wired bench tool (UART/I2C).
  LANTERN v0.1.3  - Cardputer ADV. LoRa/GNSS companion (Mesh kit).

  Each package unzips to an SD-card folder; each has its own
  README_SD.txt with flash steps. Copy to SD, flash from card or
  cable, done. The Cardputers stay lean collectors; the PC does
  the heavy lifting.

NOT INCLUDED, ON PURPOSE
  AEGIS TALON (red team) is never bundled, linked, installed or
  synced by this suite. It stays a separate, private program.

UNINSTALL
  Windows: Settings > Apps > "AEGIS Companion - Family Suite",
  or run AegisSuite-Uninstaller.exe in the install folder.
  Linux: run the setup file again with --uninstall.
  Either way your Library (synced logs + saved maps) is MOVED to
  an "Aegis-Library-Backup" folder in your home directory -
  uninstall never deletes your data.
