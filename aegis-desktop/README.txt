====================================================================
  AEGIS COMPANION v1.7.7 - desktop program for Linux + Windows
  The full-UI rewrite (native, no Python needed). Protocol AEGIS/1.
====================================================================

WHAT'S NEW IN 1.7.7
  * SETTINGS, FIXED PROPERLY (user report: "wardriving setup
    below the screen again" + "very long boxes go horizontal"):
    every Settings element is now its own list item, so the
    scroll length is exact and the Wardrive-map section at the
    bottom is always reachable (guarded by a headless layout
    regression test that fails on the old layout). Input boxes
    (feed URL, GitHub token, SD path, token display) are capped
    at a minimal 480dp instead of stretching across the window.

WHAT'S NEW IN 1.7.6
  * SETTINGS LISTS LAY OUT VERTICALLY (user report: "settings are
    side by side"): the Settings list axis was never set to
    vertical, so the new list chunks arranged horizontally. All
    Settings sections now stack in one uniform left-aligned column.
  * Ships with PHOENIX v0.11.0, which can update ITSELF from this
    repo over Wi-Fi (SETUP > Firmware update) once the release
    carrying phoenix-latest.json is published - see PUSH-ME.txt.

WHAT'S NEW IN 1.7.4
  * SETTINGS SCROLLS TO THE BOTTOM (user screenshot): Settings was
    one giant list item and the scroll range ended early, hiding
    the whole Wardrive map section (Save home location, Save home
    map, Export/Import pack). Settings and Dashboard are now real
    multi-item lists - every button is reachable, scroll bar honest.

WHAT'S NEW IN 1.7.3
  * CLOSE MEANS CLOSE: closing the window now exits immediately.
    Before, shutdown waited for background report rebuilds and a
    slow one left an invisible copy running - it looked minimized
    forever, and that zombie then fought the uninstaller.
  * ONE COPY ONLY: a second launch now refuses with a clear
    message instead of opening a duplicate window.
  * UNINSTALL ACTUALLY UNINSTALLS (suite v1.2.2): the uninstaller
    first stops any running Companion from the install folder,
    then removes everything with retries - a live copy can no
    longer re-create the folder mid-removal. (Kill any pre-1.7.3
    zombie once by hand: pkill -f AegisCompanion)

WHAT'S NEW IN 1.7.2
  * SCROLL BARS everywhere a list can overflow (Settings first -
    it looked unscrollable): a slim bar on the right edge shows
    where you are in Settings, Library, Reports, Findings, Log,
    Live and Map lists.
  * FAILURE BANNER: when a button or connection fails, a red
    warning now appears across the TOP of the window for ~9
    seconds ("Update download failed...", "Listener is DOWN...")
    instead of hiding as small text at the bottom of a page. Red
    dashboard health problems raise it too. All 30 buttons were
    audited - every one is wired; failures now say so loudly.

WHAT'S NEW IN 1.7.1
  * THE UPDATER NOW CHECKS THE REPO (fixes "updater not working on
    the dashboard"): once per run, and any time from Settings >
    "Updates & removal", Companion fetches the tiny latest.json from
    the AEGIS-Desktop GitHub release feed and the Dashboard health
    list shows the result (up to date / update available / not
    checked). The installer itself downloads only when YOU click
    "Install update", is SHA-256-verified against latest.json, and
    then runs the normal in-place suite upgrade - Library kept.
  * If the repo is PRIVATE, GitHub answers 404 anonymously and the
    check says exactly that: either make AEGIS-Desktop public, or
    paste a read-only GitHub token into Settings (stored only in
    your local 0600 config) - feed + token are editable there.
  * Pairs with PHOENIX v0.9.9 (saved Wi-Fi now auto-joins when in
    range), BENCH/LANTERN v0.1.3 (they sync to this hub too) and
    HERALD v0.1.6 (retries Wi-Fi after a failed first join).

WHAT'S NEW IN 1.7.0
  * UPDATES & REMOVAL in Settings > "Updates & removal": "Check for
    updates" reads an Updates folder beside the program (a newer
    suite installer + latest.json) - that is the plug point for the
    future family repo; nothing is ever downloaded silently.
    "Install update" runs the staged installer in place.
    "Uninstall AEGIS Companion" (two clicks) runs the suite
    uninstaller: program + shortcut removed, Library MOVED to
    Aegis-Library-Backup, never deleted.
  * Device self-diagnosis (with PHOENIX v0.9.8): the Cardputer now
    syncs a diagnostics file; its self-checks (SD, vault, Wi-Fi, PC
    link, memory, loop, keyboard) appear in Reports in plain English.
  * Pairs with PHOENIX v0.9.8: visible SSID typing, wrong-password /
    not-found reasons with instant retry, PC-link confirmation on
    the device, boot progress in the top header bar.

WHAT'S NEW IN 1.6.0
  * SIGNED MAP PACKS: exported .aegismap packs now carry a SHA-256
    tree hash of every tile + an HMAC signature made with your token.
    Import verifies first: tampered or foreign-signed packs are
    refused; unsigned older packs still import by hand (and say so).
  * SELF-REFRESHING MAP LIBRARY: after a saved-home-map download the
    pack Library/maps/packs/AEGIS-MAP-home.aegismap refreshes itself,
    and at startup any NEW signature-verified pack in that folder is
    auto-imported - copy one file to another PC, maps work offline.
  * Golden-image regression tests for the family artwork + tiles.
  * Sync report rebuilds now drain cleanly at shutdown.
  * Siblings: SCRIBE v0.1.7 (fuzz-proven chain refactor), HERALD
    v0.1.4 (3 MB app partition - flash its partitions.bin once).

WHAT IT DOES
  Get home from a wardrive, and PHOENIX / SCRIBE / HEARTH on your
  home Wi-Fi talk to this program: it downloads their logs, verifies
  every file (SHA-256) and every hash chain, files them by date, and
  writes plain-English reports (technical paths folded underneath).
  While a Cardputer is on and scanning or transmitting, it also
  streams live status - the Live Monitor page shows it dashboard-
  style: device, state, activity detail, battery, progress, APs in
  view, events and signal history. TALON is never linked, by design.

INSTALL (nothing to install, really)
  1. Extract this ZIP anywhere you like and KEEP the folder: the
     shortcut points at the program file where it sits.
  2. Run it:  AegisCompanion        (Linux)
              AegisCompanion.exe    (Windows)
     First run creates aegis-companion.json (port + secret token)
     and a Library/ folder next to the program.
  3. In the program: Settings > "Install Start Menu / app-menu
     shortcut". That is it - it now appears in your Start Menu
     (Windows) or applications menu (Linux) with the PHOENIX icon.

LINK YOUR CARDPUTERS (once per SD card)
  Settings page > "Put AEGIS_LINK on a card": insert a Cardputer SD
  card, type its folder, click "Write AEGIS_LINK to SD card".
  Or from a terminal:  AegisCompanion --init
  Then set the home Wi-Fi once in PHOENIX (SETUP > Home WiFi
  setup) - SCRIBE/HEARTH read the same shared profile.
  Firmware needed: PHOENIX v0.9.3+, SCRIBE v0.1.4+, HEARTH v0.1.4+
  (live streaming); older firmware still syncs files as before.

PAGES
  Dashboard   - today's take: files synced, devices seen, findings,
                chain health, per-program headlines.
  Live Monitor- live device cards + detail pane with AP and battery
                history graphs while a device streams.
  Library     - everything filed by date/program; raw files are
                kept byte-identical, exactly as received.
  Reports     - plain-English write-ups (open the full HTML report).
  Activity Log- this session's log; full logs are kept under logs/.
  Settings    - port + token, AEGIS_LINK writer, SD-card import
                (for cards you would rather not network), shortcut.

COMMAND LINE (optional)
  (no args)        the window (this is the normal way)
  --headless       listener only, no window
  --selftest       in-process end-to-end test; prints ALL PASS
  --import <dir>   file an SD-card folder into the Library
  --init           write an AEGIS_LINK folder template
  --version

SECURITY NOTES
  Devices prove they know the shared token (HMAC challenge-
  response) before anything is accepted; the token itself never
  crosses the network. Files are size-capped, SHA-256 verified,
  and stored atomically. Nothing phones home: the only listener
  is the device port (default 8791) on your home network.

HONEST LIMITS
  The Windows build is cross-compiled and protocol-tested, but has
  not yet been run on physical Windows hardware - first launch
  feedback welcome. The Linux build needs the usual desktop
  libraries (X11/Wayland + EGL) that ship with any modern distro.

WHAT'S NEW IN v1.1.0 - AUTOMATIC, DYNAMIC INTERCONNECTION
- Auto-discovery: devices now find THIS computer by themselves (broadcast
  handshake proven with the pairing token - safe on a shared network).
  No more typing host addresses, and a changed PC address (DHCP) stops
  being a problem: devices re-discover and remember the new address.
- Self-testing: the Dashboard's "Link health" card checks the listener,
  the discovery responder, library write access, hash-chain integrity and
  the live link automatically, about once a minute. Problems show in plain
  English with the fix (e.g. "another program is using :8791").
- Live link watchdog: a device that drops silently is marked offline on
  its own; devices reconnect with backoff by themselves.
- Companion stays the family hub: AEGIS TALON is still never linked, by
  design, and AEGIS KEEPER's radio stays off.

WHAT'S NEW IN v1.2.0 - WARDRIVE MAP
- New "Wardrive Map" page: every GPS-tagged network from your synced
  library, plotted where its signal was strongest, with your route
  drawn through it. Works offline (schematic plot); one click opens an
  interactive street map in your browser, and every network row (and
  every map pin) links straight into Google Maps at that spot - no
  Google API key needed.
- Needs PHOENIX v0.9.5+, which now syncs the map files (wifi_map
  .geojson/.csv, WiGLE CSV, KML, GPS track, wardrive log) automatically
  alongside the event index, and refreshes them whenever wardrive is
  switched off.
- The page also counts sightings that had no GPS fix, so you can tell
  "no data" apart from "no sky view".

WHAT'S NEW IN v1.3.0 - FAMILY ARTWORK + REAL MAPS + FULLER INFO
- Every AEGIS program now has its own detailed artwork (phoenix,
  scribe's book & quill, hearth's house, herald's flag, bench wrench,
  lantern, keeper's key, talon's claw). The Dashboard wears the big
  Companion crest, with a clickable family strip underneath - click a
  member to jump straight to it. Device cards, detail panes, Library,
  Reports and Settings all show the right program's art and a plain-
  English note of what it does, how it connects, and what it sends.
- Wardrive Map now shows REAL street maps by default, inside the app
  (OpenStreetMap tiles, cached on disk; Settings shows the cache size
  and can clear it). The offline schematic plot is one click away and
  still appears automatically when there is no internet.
- Settings gained a family guide: every program, its hardware, how it
  connects (or why it never does - TALON, KEEPER), and what it sends.

WHAT'S NEW IN v1.4.0 - SAVED HOME MAP + HD ART
- Saved home map: set your home location once (Settings > Wardrive
  map), then "Save 100x100-mile home map" downloads that square of
  street map ONCE (zoom levels 10-14, about 150 MB, polite 3-at-a-time
  fetching, stop/resume any time - already-saved squares are skipped).
  With "Saved map only" ON (it turns itself on once a map is saved),
  the Wardrive Map draws real streets for any drive inside that square
  straight from disk: no internet, no web call, your drives stay
  browsable offline. Drives outside the square fall back to the
  schematic with a note saying why. Progress shows on the Wardrive
  Map page; Settings shows the saved area and can forget it.
- HD family art: every program glyph (companion crest, phoenix,
  scribe, hearth, herald, bench, lantern, keeper, talon) re-authored
  at 48x48 with bevel shading and fine detail - pupils, quill barbs,
  window crosses, chimney smoke, grip bands - same sizes on screen,
  much crisper. Regenerate any time with artgen_hd.py.

WHAT'S NEW IN v1.5.0 - HARDENING + STANDARD MAP LIBRARY
- Standard map library: export your saved map squares as ONE portable
  .aegismap pack (Settings > Wardrive map > Export map library pack,
  lands in Library/maps/packs). Import it on any computer running the
  companion and maps work with NO internet at all - a fresh install
  plus a pack never makes a web call. Packs are validated on import
  (strict tile paths, size caps - a hostile pack cannot write outside
  the map folder; this is fuzz-tested).
- Hardening found by fuzzing this build: device-supplied live numbers
  (progress %, battery, counts) are now clamped before they reach the
  dashboard; device filenames containing ".." or ending in a dot are
  refused outright (Windows aliasing hazard). Tile builders refuse
  non-finite map bounds instead of trusting float luck.
- Performance: program art is rendered once to cached bitmaps (was
  2,304 paint operations per badge per frame), and the Settings page
  no longer walks the whole 10k-file tile cache on every frame.
- The Go source now vendors its dependencies (app/vendor), so the
  companion itself builds with no network.
