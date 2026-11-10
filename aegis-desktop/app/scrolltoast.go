package main

import (
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
)

// Scroll bars + top toast banner (v1.7.2, user request).
//
// Gio's lists scroll but show no persistent scroll bar, so long
// pages (Settings especially) looked unscrollable. withScrollBar
// wraps any list and paints a slim always-visible bar whenever the
// content is taller than the viewport: thumb size = visible
// fraction, thumb position = scroll progress.
//
// The toast is the "something is broken" banner: any page message
// that reports a failure (button errors, rejected input, failed
// downloads) is ALSO raised as a red banner across the top of the
// window for ~9 seconds, and red dashboard health items (listener
// down, library not writable, chain problem) raise it too - so a
// failure can never hide quietly at the bottom of a page.

func fillRect(gtx layout.Context, r image.Rectangle, col color.NRGBA) {
	if r.Empty() {
		return
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, col)
}

func withScrollBar(gtx layout.Context, l *widget.List, w layout.Widget) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(w),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			size := gtx.Constraints.Max
			pos := l.Position
			if size.Y <= 0 || pos.Length <= size.Y {
				return layout.Dimensions{Size: size}
			}
			trackX := size.X - 7
			// track
			fillRect(gtx, image.Rect(trackX, 2, trackX+4, size.Y-2), color.NRGBA{0x2A, 0x3D, 0x4A, 0xFF})
			thumbH := size.Y * size.Y / pos.Length
			if thumbH < 28 {
				thumbH = 28
			}
			maxOff := pos.Length - size.Y
			scrolled := maxOff + pos.OffsetLast // 0 at top, maxOff at bottom
			if scrolled < 0 {
				scrolled = 0
			}
			if scrolled > maxOff {
				scrolled = maxOff
			}
			y := 0
			if maxOff > 0 {
				y = scrolled * (size.Y - thumbH) / maxOff
			}
			fillRect(gtx, image.Rect(trackX, y+2, trackX+4, y+thumbH-2), color.NRGBA{0x2E, 0xC4, 0xE6, 0xFF})
			return layout.Dimensions{Size: size}
		}),
	)
}

// toastState lives on UI (fields declared in gui.go).
func (u *UI) toast(text string, seconds int) {
	u.toastText = text
	u.toastUntil = time.Now().Add(time.Duration(seconds) * time.Second)
}

func looksLikeFailure(msg string) bool {
	m := strings.ToLower(msg)
	for _, k := range []string{
		"fail", "could not", "cannot", "can't", "rejected", "error",
		"must be", "must match", "type the", "set the", "needs a",
		"unreachable", "not found", "no staged", "no update staged",
		"nothing to", "mismatch", "refused", "down -", "not writable",
		"problem", "invalid", "is down", "no desktop", "no sd",
	} {
		if strings.Contains(m, k) {
			return true
		}
	}
	return false
}

// watchToastSources runs every frame: new failure messages from
// any page become a top banner; red health items re-raise it at
// most once a minute so a persistent break stays visible.
func (u *UI) watchToastSources() {
	for _, cand := range []struct {
		now  string
		seen *string
	}{
		{u.settingsMsg, &u.seenSettingsMsg},
		{u.mapMsg, &u.seenMapMsg},
	} {
		if cand.now != "" && cand.now != *cand.seen {
			*cand.seen = cand.now
			if looksLikeFailure(cand.now) {
				u.toast("⚠ "+cand.now, 9)
			}
		} else if cand.now != *cand.seen {
			*cand.seen = cand.now
		}
	}
	if time.Since(u.healthToastAt) < 5*time.Second {
		return
	}
	u.healthToastAt = time.Now()
	for _, it := range u.core.Health() {
		if !it.OK && !it.Warn {
			key := it.Label + ": " + it.Detail
			if key != u.lastHealthToast || time.Since(u.healthToastShown) > time.Minute {
				if time.Now().After(u.toastUntil) {
					u.toast("⚠ "+key, 8)
				}
				u.lastHealthToast, u.healthToastShown = key, time.Now()
			}
			break
		}
	}
}

func (u *UI) toastBanner(gtx layout.Context) layout.Dimensions {
	if u.toastText == "" || time.Now().After(u.toastUntil) {
		return layout.Dimensions{}
	}
	return layout.Inset{Top: 10, Left: 22, Right: 22}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return bgFill(gtx, colToastBg, 8, pad(10, func(gtx layout.Context) layout.Dimensions {
			return u.label(12.5, colRed, u.toastText).Layout(gtx)
		}))
	})
}
