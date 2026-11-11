#!/usr/bin/env python3
"""HD family art generator for the AEGIS Companion (v1.4.0+).

Re-authors the proven 24x24 family glyphs at 48x48: each base pixel
becomes a 2x2 block, then a bevel pass adds light/shade variants and a
per-glyph detail pass adds pupils, glints, text lines, window crosses,
smoke, grip bands and other fine features. Emits app/art_data.go and
PNG previews under art-preview/. Rerun after any tweak:
    python3 artgen_hd.py
"""
import os
from PIL import Image

BASE24 = {'COMPANION': (['........................', '.CC.C..............C.C..', '.CCC................CC..', 'CCCC......CCC......CCCCC', 'CCCCCCC...CCCWWC.CCCCCCC', 'CCCCCC....CCCWWC..CCCCCC', 'CCC.CCC...CCC...CCC..CCC', 'CCC....CC......C.....CCC', 'CCC....CC......C.....CCC', 'CCC.......CCC........CCC', '.CCC......CCC......CCC..', '.CCC......CCC......CCC..', '.CCC...CCC...CCC...CCC..', '...CCC...C...CC...CCC...', '...CCC...C...CC...CCC...', '....CCCCCC...CCCCCC.....', '.........C.AACC.........', '.........C.AACC.........', '.......CCC...CCC........', '.......CCC...CCC........', '.......CCC...CCC........', '.........C...CC.........', '.........CCC.CCC........', '.........CCC.CCC........'], {'C': '35D3F4', 'D': '1B8FB4', 'W': 'E8EAF0', 'A': 'FFB020'}), 'PHOENIX': (['........................', '.OO..................O..', '.OO..................O..', 'OOOO......OOO......OOOOO', 'OOOOOO....OOOWWO..OOOOOO', 'OOOOOO....OOOWWO..OOOOOO', 'OOO.OOO...OOO...OOO..OOO', 'AAA....AA......A.....AAA', 'AAA....AA......A.....AAA', 'AAA.......AAA........AAA', '.AAA......AAA......AAA..', '.AAA......AAA......AAA..', '.AAA...AAA...AAA...AAA..', '...AAA...A...AA...AAA...', '...AAA...A...AA...AAA...', '....CCCCCC...CCCCCC.....', '.........C.OOCC.........', '.........C.OOCC.........', '.......CCC...CCC........', '.......CCC...CCC........', '.......CCC...CCC........', '.........C...CC.........', '.........CRC.RCC........', '.........CCRRCCC........'], {'C': '35D3F4', 'O': 'FF7A1A', 'A': 'FFB020', 'R': 'FF5252', 'W': 'E8EAF0'}), 'SCRIBE': (['........................', '........................', '..................A.....', '...................AW...', '...................W....', '..................AW....', '....PPPPPPPDDPPPPAWP....', '...VPPPPPPPDDPPPPAWPV...', '...VPPPPPPPDDPPPAWPPV...', '...VPDDDDDPDDPDDWDDPV...', '...VPPPPPPPDDPPAWPPPV...', '...VPDDDDDPDDPDWDDDPV...', '...VPPPPPPPDDPAWPPPPV...', '...VPDDDDDPDDPWDDDDPV...', '...VPPPPPPPDDAWPPPPPV...', '...VPDDDDDPDDWDDDDDPV...', '...VPPPPPPPDDPPPPPPPV...', '...VPDDDDPPDDPPPPPPPV...', '...VPPPPPPPDDPPPPPPPV...', '...VVVVVVVVVVVVVVVVVV...', '........................', '........................', '........................', '........................'], {'V': 'B388FF', 'P': 'D9CCFF', 'D': '7A5FD0', 'A': 'FFC93D', 'W': 'FFF6D8'}), 'BENCH': (['........................', '........................', '..............CCCCCC....', '.............CCCCCC.....', '............CWC.........', '...........CWC..........', '...........CC.......CC..', '...........CC.......CC..', '...........CC.......CC..', '...........CCC.....CCC..', '............CDCC..CCC...', '............DCCCCCCC....', '...........DCCCCCCC.....', '..........DCCCC.........', '..........DCCC..........', '.........DCCC...........', '........DCCCC...........', '.......DCCCC............', '.....CDCCCC.............', '.....CCCCC..............', '......CCC...............', '........................', '........................', '........................'], {'C': '2DD4BF', 'D': '147D6F', 'W': 'E8EAF0'}), 'LANTERN': (['........................', '..........F..F..........', '.........F....F.........', '.........FFFFFF.........', '.........FFFFFF.........', '...........FF...........', '.......FFFFFFFFFF.......', '.......FAAAAAAAAF.......', '.......FAAWWWWAAF.......', '....A..FAAWWWWAAF..A....', '.......FAAWWWWAAF.......', '...A...FAAWOOWAAF...A...', '.......FAAWWWWAAF.......', '....A..FAAWWWWAAF..A....', '.......FAAWWWWAAF.......', '.......FAAWWWWAAF.......', '.......FAAAAAAAAF.......', '.......FFFFFFFFFF.......', '.......FFFFFFFFFF.......', '.........FFFFFF.........', '........................', '........................', '........................', '........................'], {'F': 'B07E2A', 'A': 'FFD54F', 'W': 'FFF3C4', 'O': 'FF8A3D'}), 'KEEPER': (['........................', '........................', '........................', '.......SSS..............', '.....WSSSSSS............', '....WSS...SSS...........', '....SS.....SS...........', '...SS.......SS..........', '...SS.......SS..........', '...SS.......SS..........', '....SS.....SS...........', '....SSS...SLSS..........', '.....SSSSSSSLSS.........', '.......SSS.SSLSS........', '............SSLSS.......', '.............SSLSS......', '..............SSLSS..SS.', '...............SSLSSSSS.', '................SSLSSS..', '.................SSSSS..', '................SSSSSS..', '................SS.SSS..', '................SSS.....', '.................SS.....'], {'S': '90A4C8', 'L': '5B7BA6', 'W': 'E8EAF0'}), 'HEARTH': (['........................', '........................', '........................', '................BBB.....', '................BBB.....', '.........OOOOOO.BBB.....', '........OOOOOOOOBBB.....', '.......OOOOOOOOOOBB.....', '......OOOOOOOOOOOOB.....', '.....OOOOOOOOOOOOOO.....', '..OOOOOOOOOOOOOOOOOOOO..', '.....LLLLLLLLLLLLLL.....', '.....LLLLLLOOLLLLLL.....', '.....LAWALLLLLLAWAL.....', '.....LAAALDDDDLAAAL.....', '.....LAAALDDDDLAAAL.....', '.....LLLLLDDDDLLLLL.....', '.....LLLLLDDDALLLLL.....', '.....LLLLLDDDDLLLLL.....', '.....LLLLLDDDDLLLLL.....', '.....BBBBBDDDDBBBBB.....', '........................', '........................', '........................'], {'O': 'FF8A3D', 'L': 'C9D4E8', 'B': '8A5A33', 'D': '7A4A21', 'A': 'FFD54F', 'W': 'FFF3C4'}), 'HERALD': (['........................', '........................', '......A.............C...', '......S..............C..', '......SOOOOOOOO...C.....', '......SOAOOOOOOO...C..C.', '......SOOAOOOOOOOC......', '......SOOOOOOOOOO..C..C.', '......SOOOOOOOOO..C.....', '......SOOOOOOOO......C..', '......SOOOOOOO......C...', '......S.................', '......S.................', '......S.................', '......S.................', '......S.................', '......S.................', '......S.................', '......S.................', '......S.................', '......S.................', '......S.................', '...SSSSSSSSSSSS.........', '........................'], {'O': 'FF7A59', 'A': 'FFB020', 'C': '35D3F4', 'S': '8A94A8'}), 'TALON': (['........................', '.............ARR........', '.......ARR...ARR...ARR..', '.......ARR..ARRR...ARR..', '......ARRR..ARRR..ARRR..', '......ARRR..ARR...ARRR..', '......ARR...ARR...ARR...', '......ARR..ARRR...ARR...', '.....ARRR..ARRR..ARRR...', '.....ARRR..ARR...ARRR...', '.....ARR...ARR...ARR....', '.....ARR..ARRR...ARR....', '....ARRR..ARRR..ARRR....', '....ARRR..ARR...ARRR....', '....ARR...ARR...ARR.....', '....ARR..ARRR...ARR.....', '...ARRR..ARRR..ARRR.....', '...ARRR..RRR...ARRR.....', '...RRR...RRR...RRR......', '...RRR.........RRR......', '........................', '........................', '........................', '........................'], {'R': 'FF5252', 'A': 'FFB020'})}

EXTRA_PAL = {
    "COMPANION": {"K": "14202E"},
    "PHOENIX": {"K": "14202E"},
    "HEARTH": {"S": "8A94A8"},
}

# detail ops in 48x48 space: (op, ...) with op in px/rect/hline/vline
DETAILS = {
    "COMPANION": [
        ("px", 23, 33, "c"), ("px", 24, 33, "c"),   # hub light dots
        ("px", 3, 3, "c"), ("px", 44, 3, "c"),      # crest tip lights
        ("px", 23, 36, "W"), ("px", 24, 36, "W"),   # hub core
    ],
    "PHOENIX": [
        ("rect", 2, 2, 5, 3, "A"), ("rect", 42, 2, 45, 3, "A"),  # flame tips
        ("px", 23, 20, "R"), ("px", 24, 21, "R"), ("px", 25, 20, "A"),  # breast flame
        ("px", 23, 44, "R"), ("px", 24, 44, "R"),   # tail ember
    ],
    "SCRIBE": [
        ("hline", 10, 21, 19, "D"), ("hline", 28, 21, 37, "D"),  # extra text lines
        ("hline", 10, 29, 19, "D"), ("hline", 28, 29, 37, "D"),
        ("hline", 10, 37, 19, "D"), ("hline", 28, 37, 37, "D"),
        ("px", 38, 8, "W"), ("px", 37, 11, "W"), ("px", 36, 14, "W"),  # quill barbs
        ("px", 8, 14, "W"), ("px", 39, 14, "W"),     # page corner light
    ],
    "HEARTH": [
        ("hline", 12, 27, 17, "B"), ("vline", 14, 26, 29, "B"),  # window crosses
        ("hline", 30, 27, 35, "B"), ("vline", 32, 26, 29, "B"),
        ("px", 26, 33, "A"),                    # door knob
        ("px", 36, 4, "S"), ("px", 38, 2, "S"), ("px", 34, 2, "S"),  # chimney smoke
        ("hline", 10, 41, 37, "B"),             # foundation shadow
    ],
    "HERALD": [
        ("px", 12, 6, "A"), ("px", 13, 6, "A"), ("px", 13, 4, "A"),  # finial
        ("rect", 15, 14, 31, 15, "o"),          # flag wave shading
        ("hline", 6, 45, 29, "S"),              # ground shadow
    ],
    "BENCH": [
        ("rect", 12, 30, 15, 31, "D"), ("rect", 15, 34, 18, 35, "D"),  # grip bands
        ("rect", 27, 22, 28, 23, "W"),          # pivot bolt
        ("px", 30, 6, "W"), ("px", 33, 9, "W"),      # jaw highlights
    ],
    "LANTERN": [
        ("rect", 23, 24, 24, 29, "W"),          # bright flame core
        ("px", 23, 2, "A"), ("px", 24, 2, "A"),      # handle light
        ("hline", 14, 37, 33, "F"),             # base plate shade row (base char, bevel darkens)
    ],
    "KEEPER": [
        ("px", 40, 35, "W"), ("px", 42, 39, "W"),    # tooth glints
        ("px", 10, 8, "W"),                     # bow rim light
    ],
    "TALON": [
        ("px", 13, 10, "r"), ("px", 13, 18, "r"),    # claw shine
        ("px", 25, 8, "r"), ("px", 25, 16, "r"),
        ("px", 37, 10, "r"), ("px", 37, 18, "r"),
    ],
}


def shade(hexcol, f):
    r, g, b = (int(hexcol[i:i + 2], 16) for i in (0, 2, 4))
    if f >= 1:  # lighten toward white
        r, g, b = (min(255, int(v + (255 - v) * (f - 1))) for v in (r, g, b))
    else:       # darken
        r, g, b = (int(v * f) for v in (r, g, b))
    return "%02X%02X%02X" % (r, g, b)


def build(name, rows24, pal24):
    pal = dict(pal24)
    pal.update(EXTRA_PAL.get(name, {}))
    used = set("." ) | set(pal)
    variants = {}  # base char -> (light, dark)
    pool = iter("123456789!#$%&*+=?")
    for ch in list(pal):
        light = ch.lower() if ch.isalpha() and ch.lower() not in used else None
        if light:
            used.add(light)
        dark = next(p for p in pool if p not in used)
        used.add(dark)
        variants[ch] = (light, dark)
        if light:
            pal[light] = shade(pal24.get(ch, pal[ch]), 1.35)
        pal[dark] = shade(pal24.get(ch, pal[ch]), 0.6)

    n = 48
    base = [["."] * n for _ in range(n)]
    for y in range(24):
        for x in range(24):
            c = rows24[y][x]
            for dy in (0, 1):
                for dx in (0, 1):
                    base[y * 2 + dy][x * 2 + dx] = c
    out = [row[:] for row in base]
    # bevel: top/left edges lighten, bottom/right darken (per 2x2 block face)
    for y in range(n):
        for x in range(n):
            c = base[y][x]
            if c == ".":
                continue
            light, dark = variants[c]
            up = base[y - 1][x] if y > 0 else "."
            dn = base[y + 1][x] if y < n - 1 else "."
            lf = base[y][x - 1] if x > 0 else "."
            rt = base[y][x + 1] if x < n - 1 else "."
            if y % 2 == 0 and up != c and light:
                out[y][x] = light
            elif y % 2 == 1 and dn != c:
                out[y][x] = dark
            elif x % 2 == 0 and lf != c and light:
                out[y][x] = light
            elif x % 2 == 1 and rt != c:
                out[y][x] = dark
    # authored details
    for op in DETAILS.get(name, []):
        kind = op[0]
        if kind == "px":
            _, x, y, c = op
            out[y][x] = c
        elif kind == "rect":
            _, x0, y0, x1, y1, c = op
            for yy in range(y0, y1 + 1):
                for xx in range(x0, x1 + 1):
                    out[yy][xx] = c
        elif kind == "hline":
            _, x0, y, x1, c = op
            for xx in range(x0, x1 + 1):
                out[y][xx] = c
        elif kind == "vline":
            _, x, y0, y1, c = op
            for yy in range(y0, y1 + 1):
                out[yy][x] = c
    grid = ["".join(r) for r in out]
    return grid, pal


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    os.makedirs(os.path.join(here, "art-preview"), exist_ok=True)
    parts = ["package main\n\nimport \"image/color\"\n\n"
             "// Family pixel art, 48x48 HD (artgen_hd.py). Full-bleed glyphs in\n"
             "// each program's own colors - no shield frames. Light/dark variant\n"
             "// chars are generated bevel shades of the base palette.\n"
             "type progArtDef struct {\n\tgrid []string\n\tpal  map[byte]color.NRGBA\n}\n\n"
             "func artPal(m map[byte]string) map[byte]color.NRGBA {\n\tout := make(map[byte]color.NRGBA, len(m))\n\tfor k, hx := range m {\n\t\tout[k] = color.NRGBA{hx2b(hx[0:2]), hx2b(hx[2:4]), hx2b(hx[4:6]), 0xFF}\n\t}\n\treturn out\n}\n\n"
             "func hx2b(s string) uint8 {\n\tvar v uint8\n\tfor _, ch := range s {\n\t\tv <<= 4\n\t\tswitch {\n\t\tcase ch >= '0' && ch <= '9':\n\t\t\tv |= uint8(ch - '0')\n\t\tcase ch >= 'A' && ch <= 'F':\n\t\t\tv |= uint8(ch - 'A' + 10)\n\t\tcase ch >= 'a' && ch <= 'f':\n\t\t\tv |= uint8(ch - 'a' + 10)\n\t\t}\n\t}\n\treturn v\n}\n\nvar progArt = map[string]progArtDef{\n"]
    previews = {}
    for name, (rows24, pal24) in BASE24.items():
        grid, pal = build(name, rows24, pal24)
        assert len(grid) == 48 and all(len(r) == 48 for r in grid), name
        palgo = ", ".join("'%s': \"%s\"" % (k, v) for k, v in sorted(pal.items()))
        rowsgo = "\n".join('\t\t"%s",' % r for r in grid)
        parts.append('\n\t"%s": {grid: []string{\n%s\n\t}, pal: artPal(map[byte]string{%s})},\n' % (name, rowsgo, palgo))
        # preview PNG (nearest-neighbour x8 on dark backdrop)
        img = Image.new("RGB", (48, 48), (11, 14, 20))
        px = img.load()
        rgb = {k: tuple(int(v[i:i + 2], 16) for i in (0, 2, 4)) for k, v in pal.items()}
        for y in range(48):
            for x in range(48):
                c = grid[y][x]
                if c != ".":
                    px[x, y] = rgb[c]
        previews[name] = img.resize((384, 384), Image.NEAREST)
        previews[name].save(os.path.join(here, "art-preview", name.lower() + ".png"))
    parts.append("}\n")
    with open(os.path.join(here, "app", "art_data.go"), "w") as f:
        f.write("".join(parts))
    # contact sheet
    names = list(BASE24)
    sheet = Image.new("RGB", (384 * 3, 384 * 3), (5, 7, 11))
    for i, nm in enumerate(names):
        sheet.paste(previews[nm], ((i % 3) * 384, (i // 3) * 384))
    sheet.save(os.path.join(here, "art-preview", "contact-sheet.png"))
    print("art_data.go written (48x48 HD), previews in art-preview/")


if __name__ == "__main__":
    main()
