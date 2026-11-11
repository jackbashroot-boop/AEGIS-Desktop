# TEST REPORT — Suite v1.2.24 (Companion 1.8.12 / PHOENIX 0.12.15)

Built 2026-11-07. Companion-only round driven by a screenshot of
the System page; the firmware is unchanged from v1.2.23.

## Layout discipline (all pages)
- Page content is now centred at a 1180 dp maximum width — cards,
  bars and buttons no longer stretch edge-to-edge on a maximised
  window. Long text still gets the full capped width when it needs it.
- Buttons size to their text (with comfortable padding) instead of
  filling the row — the full-width "Back up now" bar is gone.
  Progress/usage bars are capped at 560 dp.
- System → "Repo & recovery commands" is now a collapsed drop-down
  (click to open); its commands were also made version-agnostic.
- Settings sections are clearly named area banners ("Connection —
  listener", "Presence — who is home", "Home location & offline
  map", "Updates — this program", "About & help", ...), still one
  list item per row so scrolling behaviour is unchanged (the
  scroll-to-bottom regression test still passes).

## Map fixes ("the lat and long is affecting it")
- Records with latitude/longitude written swapped are repaired
  when their mirror lands by the rest of the data (unambiguous
  test), instead of dragging the frame to another ocean.
- The map frame now trims far-flung outliers: one bad fix hundreds
  of km away no longer shrinks the real drive to a dot. NaN/Inf
  (genuinely corrupt) coordinates are still refused outright, as
  before.
- Routes are per-file segments: the pen lifts between drives/days
  and over >3 km jumps, and Route km no longer counts the jumps.
- HOME is marked on the map even when sightings exist (it used to
  vanish once data arrived).

## Artwork
- The animated program GIFs are embedded for all eight programs
  but only PHOENIX and TALON were ever decoded — the rest silently
  fell back to static art. All eight now load.

## Who's home picker
- Settings → Presence now lists the devices this PC has seen on
  the network (live + known census), each with name, MAC, IP,
  vendor and last-seen. Ticking one writes the same Name=MAC lines
  as before (saved format unchanged); saved-but-unseen people stay
  listed and removable.

## Home location consent + map size
- When home is unset, Settings asks once: "Use approximate
  location" (public IP lookup, city-level, labelled approximate),
  "Type ZIP / coordinates instead", or "No thanks" (never asks
  again). On Allow, the result is saved and the offline map starts
  downloading around it automatically.
- Offline map size is a drop-down: 20×20, 50×50 or 100×100 miles,
  with a live square-count/size estimate. The downloader, saved-map
  framing and all labels follow the choice (historic default
  100×100 preserved).

## System page names
- Raw probe strings are cleaned into the names the parts are sold
  under, locally: "Advanced Micro Devices, Inc. [AMD/ATI] Navi 31
  [Radeon RX 7900 XTX/…]" → "AMD Radeon RX 7900 XTX · Navi 31";
  "12th Gen Intel(R) Core(TM) i5-12600K" → "Intel Core i5-12600K
  (12th Gen)". Added Memory and Machine (DMI) rows and the real OS
  name (os-release). Nothing about the PC is sent anywhere — the
  naming rules are baked in.

## Verification
- gofmt clean, go vet clean, full `go test` PASS (new tests:
  friendly names, IP-location parsing, track segments/route,
  swapped-coordinate repair, frame trimming, headless layout smoke
  for System/Map at three window sizes; the settings
  scroll-to-bottom and bad-bounds refusal tests still pass).
- Companion 1.8.12 `--version` + `--selftest` ALL PASS, including
  from a fake-HOME suite install. Suite setups built: Linux SHA
  `c220868ae243ec60f9613df9a25aec77a1ee2aaf54da211ea20a8ecdb18ffa34`,
  Windows `aee5c6889731a52eca0f7fd420df7612b047882c6f99be69d241b4d1b49013a0`.
- Known flake: TestOversizeStreamKilled (network timing) failed
  once under full-suite load, passes in isolation and on re-run.
- PHOENIX 0.12.15 binary unchanged (SHA `300f26b6…`).

## Hardware / eyes-on confirmation still needed
- Maximised window: System + Settings should sit centred, buttons
  text-sized, repo commands tucked away until clicked.
- Wardrive Map with real data: frame hugs the drive, HOME visible,
  no cross-town route lines.
- Settings → Presence picker lists the home's devices with MACs.
