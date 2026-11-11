#!/usr/bin/env python3
"""Driver updater/validator for AEGIS PHOENIX (offline, stdlib only).

  python3 tools/update_drivers.py           # validate manifest vs board_config.h + platformio.ini
  python3 tools/update_drivers.py --bump bblanchon/ArduinoJson=^7.4.0
      # record a new library version in platformio.ini AND the manifest,
      # so the next build picks it up. Nothing is downloaded by this tool;
      # run tools/make_offline_bundle.sh afterwards (online) to vendor it.
"""
import json, re, sys
from pathlib import Path
ROOT = Path(__file__).resolve().parent.parent
man = json.loads((ROOT / "drivers/manifest.json").read_text())
board = (ROOT / "src/board_config.h").read_text()
ini = (ROOT / "platformio.ini").read_text()
problems = []
for d in man["drivers"]:
    f = ROOT / d["file"]
    if not f.exists():
        problems.append(f"{d['id']}: driver file missing {d['file']}")
    if "/" in d["lib"] and d["lib"].split()[0] not in ini and "Arduino" not in d["lib"] and "companion" not in d["lib"]:
        problems.append(f"{d['id']}: library {d['lib']} not in platformio.ini")
print(f"drivers: {len(man['drivers'])} registered, {len(man.get('planned', []))} planned")
for d in man["drivers"]:
    print(f"  {d['id']:<20} {d['status']:<18} {d['lib']} {d['version']}")
for p in problems:
    print("PROBLEM:", p)
if "--bump" in sys.argv:
    spec = sys.argv[sys.argv.index("--bump") + 1]
    lib, ver = spec.split("=", 1)
    ini2 = re.sub(rf"{re.escape(lib)}\s*@\s*\S+", f"{lib} @ {ver}", ini)
    if ini2 == ini:
        print("no platformio.ini entry matched", lib); sys.exit(1)
    (ROOT / "platformio.ini").write_text(ini2)
    for d in man["drivers"]:
        if d["lib"].startswith(lib): d["version"] = ver
    (ROOT / "drivers/manifest.json").write_text(json.dumps(man, indent=2) + "\n")
    print(f"bumped {lib} -> {ver} in platformio.ini + manifest; re-run make_offline_bundle.sh (online) to vendor it")
sys.exit(1 if problems else 0)
