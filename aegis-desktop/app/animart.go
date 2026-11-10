package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"sync"
	"time"

	"gioui.org/op/paint"
	"gioui.org/widget"
)

// Animated family artwork (v1.7.9, user request): real animated
// GIFs for PHOENIX (flapping bird + embers) and TALON (pulsing
// claw slashes), generated as pixel art and embedded in the
// program. They play inside the family badges wherever those are
// drawn; the Dashboard and Settings ask for a fresh frame every
// ~120 ms while visible. Any other program keeps its static art,
// and a decode failure also falls back to the static art.

//go:embed assets/phoenix.gif
var phoenixGIF []byte

//go:embed assets/talon.gif
var talonGIF []byte

var (
	animOnce   sync.Once
	animFrames = map[string][]paint.ImageOp{}
	animErr    error
)

func loadAnimArt() {
	for prog, raw := range map[string][]byte{"PHOENIX": phoenixGIF, "TALON": talonGIF} {
		g, err := gif.DecodeAll(bytes.NewReader(raw))
		if err != nil {
			animErr = fmt.Errorf("decode %s gif: %w", prog, err)
			continue
		}
		if len(g.Image) == 0 {
			continue
		}
		w, h := g.Config.Width, g.Config.Height
		if w <= 0 || h <= 0 {
			w, h = g.Image[0].Bounds().Dx(), g.Image[0].Bounds().Dy()
		}
		base := image.NewNRGBA(image.Rect(0, 0, w, h))
		ops := make([]paint.ImageOp, 0, len(g.Image))
		for _, fr := range g.Image {
			draw.Draw(base, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
			snap := image.NewNRGBA(base.Bounds())
			copy(snap.Pix, base.Pix)
			ops = append(ops, paint.NewImageOp(snap))
		}
		if len(ops) > 0 {
			animFrames[prog] = ops
		}
	}
}

// animImage returns the current animation frame for prog, or nil
// when the program has no animated art.
func animImage(prog string) *widget.Image {
	animOnce.Do(loadAnimArt)
	ops := animFrames[prog]
	if len(ops) == 0 {
		return nil
	}
	src := ops[int(time.Now().UnixMilli()/115)%len(ops)]
	return &widget.Image{Src: src, Fit: widget.Contain}
}
