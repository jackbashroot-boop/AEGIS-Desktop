package main

import "testing"

func TestProgArtComplete(t *testing.T) {
	progs := append([]string{"COMPANION"}, familyOrder...)
	for _, p := range progs {
		def, ok := progArt[p]
		if !ok {
			t.Fatalf("no art for %s", p)
		}
		if len(def.grid) != 48 {
			t.Fatalf("%s: %d rows, want 48 (HD)", p, len(def.grid))
		}
		painted := 0
		for y, row := range def.grid {
			if len(row) != 48 {
				t.Fatalf("%s row %d: len %d, want 48 (HD)", p, y, len(row))
			}
			for x := 0; x < 48; x++ {
				ch := row[x]
				if ch == '.' {
					continue
				}
				if _, ok := def.pal[ch]; !ok {
					t.Fatalf("%s (%d,%d): char %q not in palette", p, x, y, ch)
				}
				painted++
			}
		}
		if painted < 150 {
			t.Fatalf("%s: only %d painted cells - glyph too sparse for HD", p, painted)
		}
		seen := map[byte]bool{}
		for _, row := range def.grid {
			for x := 0; x < len(row); x++ {
				if row[x] != '.' {
					seen[row[x]] = true
				}
			}
		}
		if len(seen) < 5 {
			t.Fatalf("%s: only %d distinct shades - HD art should carry bevel/detail shades", p, len(seen))
		}
	}
}

func TestProgInfoComplete(t *testing.T) {
	for _, p := range familyOrder {
		d, ok := progInfo[p]
		if !ok {
			t.Fatalf("no progInfo for %s", p)
		}
		if d.Title == "" || d.Device == "" || d.Role == "" || d.Connect == "" || d.Sends == "" {
			t.Fatalf("progInfo %s incomplete: %+v", p, d)
		}
	}
	// standing design rules, in writing
	if got := progInfo["TALON"].Connect; got == "" || got[:5] != "NEVER" {
		t.Fatalf("TALON connect text must lead with NEVER: %q", got)
	}
}

func TestTileMath(t *testing.T) {
	if x := lonToWorldX(-180, 10); x != 0 {
		t.Fatalf("lon -180 -> %v", x)
	}
	if x := lonToWorldX(180, 10); x != 256*1024 {
		t.Fatalf("lon 180 -> %v", x)
	}
	if y := latToWorldY(0, 10); y != 128*1024 {
		t.Fatalf("lat 0 -> %v", y)
	}
	// Norwich, CT at z13 lives in tile (2455, 3055)
	tx := int(lonToWorldX(-72.0759, 13)) / 256
	ty := int(latToWorldY(41.5247, 13)) / 256
	if tx != 2455 || ty != 3055 {
		t.Fatalf("Norwich tile = (%d,%d), want (2455,3055)", tx, ty)
	}
	z := pickZoom(41.51, -72.09, 41.54, -72.06, 1150, 380)
	if z < 12 || z > 16 {
		t.Fatalf("pickZoom = %d, want 12..16", z)
	}
}
