# AEGIS v1.2.14 pre-release test report (Companion 1.8.4 / PHOENIX 0.12.7)

## Brutal pass x3
- Round 1: `go vet` clean; full Companion test suite PASS; firmware hash-chain
  host fuzz (ASan+UBSan) 5,014 cases, 0 crashes.
- Round 2: `go test -race` PASS; five Go fuzzers (protocol parser, name
  sanitizer, discovery, map-file parsers, tile math) 8 s each PASS;
  chain fuzz 7,514 cases, 0 crashes.
- Round 3 (final binaries): full test suite PASS again; PHOENIX PlatformIO
  build SUCCESS (48.9% flash); chain fuzz 10,014 cases, 0 crashes;
  fake-home suite install PASS; installed Companion `--selftest` ALL PASS;
  GUI under Xvfb alive after 20 s with listener/discovery self-checks OK.
- Sibling firmwares all build: SCRIBE, BENCH, HEARTH, HERALD, KEEPER, LANTERN.
  (SCRIBE's first attempt died from compiler OOM during six parallel builds;
  sequential rerun SUCCESS - an environment limit, not a code failure.)

## Location accuracy work
- Firmware: wardrive/atlas/track only accept logging-quality fixes
  (>=4 sats, HDOP <= 8, not a rejected jump); impossible GPS leaps are
  rejected with a persistent-relocation recovery; records/exports now
  carry HDOP + estimated accuracy; live frames report gps_acc/gps_rej.
- Companion: per-network pins combine repeated sightings - strongest fix
  anchors, nearby strong/clean fixes refine, clustered evidence rejects
  a lone strongest-point GPS spike; every pin carries a +/- radius and
  the HTML map draws it as a circle. Track spikes are cleaned; map
  centres use medians. ZIP home lookup reports its area radius;
  Settings can GPS-refine home from 3+ sightings of the home SSID.
- Honest limit: consumer GNSS + RSSI cannot produce survey-grade AP
  positions. More sightings from different sides shrink the radius;
  they do not make it zero.

## Still needs real hardware
First Dashboard-pushed PHOENIX update end-to-end, GNSS cap feel with
0.12.7 gating, cap-on SD behaviour, Windows installer on real Windows.
