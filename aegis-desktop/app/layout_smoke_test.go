package main

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// Headless layout smoke tests (v1.8.12): the System page gained a
// collapsible section and every page gained a centred width cap -
// lay them out at phone and maximised sizes, collapsed and open,
// and fail on any panic.
func TestPageLayoutSmoke(t *testing.T) {
	dir := t.TempDir()
	log := NewLog()
	core := &Core{
		Cfg:   &Config{Library: dir, Token: "0123456789abcdef0123456789abcdef", Port: 8791, UpdateFeed: "https://example.invalid/latest.json"},
		Log:   log,
		Live:  NewLiveRegistry(),
		Store: NewStore(dir, log),
	}
	frame := func(u *UI, w, h int, pg func(layout.Context) layout.Dimensions) {
		var ops op.Ops
		gtx := layout.Context{
			Ops:         &ops,
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(w, h)),
		}
		pg(gtx)
	}
	for _, size := range [][2]int{{800, 600}, {2560, 1440}, {480, 800}} {
		u := newUI(core)
		frame(u, size[0], size[1], u.pageSystem) // repo commands collapsed
		u.showRepo = true
		frame(u, size[0], size[1], u.pageSystem) // ... and open
		u2 := newUI(core)
		frame(u2, size[0], size[1], u2.pageMap) // empty map, no home set
		u2.core.Cfg.HomeLat, u2.core.Cfg.HomeLon = 41.5247, -72.0759
		frame(u2, size[0], size[1], u2.pageMap) // home set, still no data
		_ = u2.dropMenu
	}
}
