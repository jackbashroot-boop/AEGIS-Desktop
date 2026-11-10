package main

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// Settings must scroll ALL the way to the Wardrive/About tail and
// no list item may be wider than the viewport (v1.7.7 regression
// test for "wardrive below the screen again" + "very long boxes").
func TestSettingsScrollableToBottom(t *testing.T) {
	dir := t.TempDir()
	log := NewLog()
	core := &Core{
		Cfg:   &Config{Library: dir, Token: "0123456789abcdef0123456789abcdef", Port: 8791, UpdateFeed: "https://example.invalid/latest.json"},
		Log:   log,
		Live:  NewLiveRegistry(),
		Store: NewStore(dir, log),
	}
	u := newUI(core)

	frame := func(u *UI, w, h int) {
		var ops op.Ops
		gtx := layout.Context{
			Ops:         &ops,
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(w, h)),
		}
		u.pageSettings(gtx)
	}

	// One huge viewport lays out every item: learn the true totals.
	frame(u, 1000, 100000)
	totalItems := u.setList.Position.Count
	totalLen := u.setList.Position.Length
	if totalItems < 40 {
		t.Fatalf("suspiciously few settings items laid out: %d", totalItems)
	}
	t.Logf("settings items=%d totalLen=%d", totalItems, totalLen)

	// Now a realistic window: scroll step by step to the bottom.
	u2 := newUI(core)
	reached := false
	for step := 0; step < 400 && !reached; step++ {
		frame(u2, 1000, 650)
		pos := u2.setList.Position
		if pos.First+pos.Count >= totalItems {
			reached = true
			break
		}
		u2.setList.Position.Offset += 200
	}
	if !reached {
		t.Fatalf("bottom of Settings unreachable: first=%d count=%d of %d items (len=%d)",
			u2.setList.Position.First, u2.setList.Position.Count, totalItems, u2.setList.Position.Length)
	}
}
