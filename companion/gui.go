package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// ---- palette (family colours; per-program accents live in store.go) ----
var (
	colBg      = color.NRGBA{0x0B, 0x0E, 0x14, 0xFF}
	colPanel   = color.NRGBA{0x14, 0x1A, 0x26, 0xFF}
	colPanel2  = color.NRGBA{0x1A, 0x22, 0x33, 0xFF}
	colEdge    = color.NRGBA{0x2A, 0x34, 0x4E, 0xFF}
	colText    = color.NRGBA{0xE8, 0xEA, 0xF0, 0xFF}
	colDim     = color.NRGBA{0x8A, 0x94, 0xA8, 0xFF}
	colFaint   = color.NRGBA{0x5B, 0x65, 0x78, 0xFF}
	colCyan    = color.NRGBA{0x35, 0xD3, 0xF4, 0xFF}
	colGreen   = color.NRGBA{0x3D, 0xDC, 0x84, 0xFF}
	colAmber   = color.NRGBA{0xFF, 0xB0, 0x20, 0xFF}
	colRed     = color.NRGBA{0xFF, 0x52, 0x52, 0xFF}
	colToastBg = color.NRGBA{0x3A, 0x0D, 0x0D, 0xFF}
	colViolet  = color.NRGBA{0xB3, 0x88, 0xFF, 0xFF}
	colSidebar = color.NRGBA{0x0E, 0x13, 0x1D, 0xFF}
)

// kPhoenix16 is the PHOENIX firmware's own splash mark, drawn here too so
// the desktop wears the family face. C=cyan, W=white-hot face.
var kPhoenix16 = [16]string{
	"................",
	".C............C.",
	"CCC....CC....CCC",
	"CCCC...CCWC.CCCC",
	"CC.CC..CC..CC.CC",
	"CC...C....C...CC",
	"CC.....CC.....CC",
	".CC....CC....CC.",
	".CC..CC..CC..CC.",
	"..CC..C..C..CC..",
	"...CCCC..CCCC...",
	"......C..C......",
	".....CC..CC.....",
	".....CC..CC.....",
	".......CC.......",
	"................",
}

func accentNRGBA(prog string) color.NRGBA {
	h := progAccent(prog)
	var r, g, b uint8
	fmt.Sscanf(h, "#%02x%02x%02x", &r, &g, &b)
	return color.NRGBA{r, g, b, 0xFF}
}

func stateColor(state string) color.NRGBA {
	switch strings.ToLower(state) {
	case "scanning", "scan":
		return colGreen
	case "alert", "alarm":
		return colRed
	case "watching", "sentinel":
		return colCyan
	case "syncing", "uploading":
		return colViolet
	case "transmitting", "tx":
		return colAmber
	}
	return colDim
}

type pageID int

const (
	pgDash pageID = iota
	pgLive
	pgMap
	pgLibrary
	pgReports
	pgLog
	pgSettings
)

var pageTitles = map[pageID]string{
	pgDash:     "Dashboard",
	pgLive:     "Live Monitor",
	pgMap:      "Wardrive Map",
	pgLibrary:  "Library",
	pgReports:  "Reports",
	pgLog:      "Activity Log",
	pgSettings: "Settings",
}

type UI struct {
	core *Core
	th   *material.Theme
	page pageID

	navClicks map[pageID]*widget.Clickable

	liveRows *clickSet
	selProg  string
	dashList widget.List
	liveList widget.List

	mapList       widget.List
	mapRows       *clickSet
	famRows       *clickSet
	btnMapMode    widget.Clickable
	btnZoomIn     widget.Clickable
	btnZoomOut    widget.Clickable
	btnClearTiles widget.Clickable
	btnSaveArea   widget.Clickable
	btnCancelArea widget.Clickable
	btnSavedOnly  widget.Clickable
	btnSaveHome   widget.Clickable
	btnDeleteArea widget.Clickable
	btnExportPack widget.Clickable
	btnImportPack widget.Clickable
	homeLatEd     widget.Editor
	homeLonEd     widget.Editor
	savedOnly     bool
	mapMsg        string
	areaProg      *areaProgress
	tileBytesVal  int64
	tileBytesAt   time.Time
	areaVal       OfflineArea
	areaOK        bool
	areaAt        time.Time
	mapReal       bool
	mapZoom       int
	tiles         *tileSet
	tilesBusy     string
	btnMapWeb     widget.Clickable
	btnMapGMaps   widget.Clickable
	btnMapRefresh widget.Clickable
	mapData       *MapData
	mapSize       int64

	libDays      *clickSet
	libProgs     *clickSet
	libDay       string
	libProg      string
	libList      widget.List
	btnOpenRaw   widget.Clickable
	btnOpenIndex widget.Clickable
	btnRebuild   widget.Clickable

	repRows     *clickSet
	repExpand   *clickSet
	expanded    map[string]bool
	selReport   string // "day\x00prog"
	repList     widget.List
	findList    widget.List
	btnOpenHTML widget.Clickable

	logList      widget.List
	btnLogFolder widget.Clickable
	setList      widget.List

	portEd       widget.Editor
	hostEd       widget.Editor
	sdEd         widget.Editor
	showToken    widget.Bool
	btnApplyPort widget.Clickable
	btnRegenTok  widget.Clickable
	btnWriteSD   widget.Clickable
	btnImportSD  widget.Clickable
	btnShortcut  widget.Clickable
	btnOpenCfg   widget.Clickable
	btnOpenLib   widget.Clickable
	settingsMsg  string

	btnCheckUpdate   widget.Clickable
	btnInstallUpdate widget.Clickable
	btnUninstall     widget.Clickable
	btnApplyFeed     widget.Clickable
	feedEd           widget.Editor
	ghTokenEd        widget.Editor
	updateFound      string // human description of a staged update
	updatePath       string // installer to run ("" = none staged)
	updateSeenMsg    string // last async update-check message shown
	toastText        string // top failure banner (v1.7.2)
	toastUntil       time.Time
	seenSettingsMsg  string
	seenMapMsg       string
	lastHealthToast  string
	healthToastAt    time.Time
	healthToastShown time.Time
	uninstallArm     bool // two-click confirm for uninstall
}

// clickSet hands out one stable widget.Clickable per string key.
type clickSet struct{ m map[string]*widget.Clickable }

func newClickSet() *clickSet { return &clickSet{m: map[string]*widget.Clickable{}} }

func (c *clickSet) get(key string) *widget.Clickable {
	if _, ok := c.m[key]; !ok {
		c.m[key] = &widget.Clickable{}
	}
	return c.m[key]
}

func newUI(core *Core) *UI {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	th.Palette.Bg = colBg
	th.Palette.Fg = colText
	th.Palette.ContrastBg = colCyan
	th.Palette.ContrastFg = color.NRGBA{0x06, 0x20, 0x2B, 0xFF}
	u := &UI{
		core: core, th: th, page: pgDash,
		navClicks: map[pageID]*widget.Clickable{},
		liveRows:  newClickSet(), libDays: newClickSet(), libProgs: newClickSet(),
		mapRows: newClickSet(), famRows: newClickSet(), mapReal: true,
		areaProg: newAreaProgress(),
		repRows:  newClickSet(), repExpand: newClickSet(), expanded: map[string]bool{},
	}
	for _, p := range []pageID{pgDash, pgLive, pgMap, pgLibrary, pgReports, pgLog, pgSettings} {
		u.navClicks[p] = &widget.Clickable{}
	}
	u.dashList.List.Axis = layout.Vertical
	u.liveList.List.Axis = layout.Vertical
	u.mapList.List.Axis = layout.Vertical
	u.libList.List.Axis = layout.Vertical
	u.repList.List.Axis = layout.Vertical
	u.findList.List.Axis = layout.Vertical
	u.logList.List.Axis = layout.Vertical
	u.logList.List.ScrollToEnd = true
	u.portEd.SingleLine = true
	u.hostEd.SingleLine = true
	u.sdEd.SingleLine = true
	u.portEd.SetText(fmt.Sprint(core.Cfg.Port))
	u.feedEd.SingleLine = true
	u.ghTokenEd.SingleLine = true
	u.feedEd.SetText(core.Cfg.UpdateFeed)
	u.ghTokenEd.SetText(core.Cfg.GitHubToken)
	u.hostEd.SetText(preferredLANIP())
	u.homeLatEd.SingleLine = true
	u.homeLonEd.SingleLine = true
	if core.Cfg.HomeLat != 0 || core.Cfg.HomeLon != 0 {
		u.homeLatEd.SetText(fmt.Sprintf("%.5f", core.Cfg.HomeLat))
		u.homeLonEd.SetText(fmt.Sprintf("%.5f", core.Cfg.HomeLon))
	}
	if _, ok := LoadOfflineArea(core.Cfg.Library); ok {
		u.savedOnly = true // a saved home map is the primary map when one exists
	}
	u.selReport = ""
	return u
}

// tileBytes caches the tile-cache directory walk: after a 100x100-mile
// save that walk touches 10k+ files and must not run per frame.
func (u *UI) tileBytes(root string) int64 {
	if time.Since(u.tileBytesAt) < 3*time.Second {
		return u.tileBytesVal
	}
	u.tileBytesVal = TileCacheBytes(root)
	u.tileBytesAt = time.Now()
	return u.tileBytesVal
}

// offlineArea caches the saved-area receipt read (small file, but it
// was being parsed several times per frame across pages).
func (u *UI) offlineArea(root string) (OfflineArea, bool) {
	if time.Since(u.areaAt) < time.Second {
		return u.areaVal, u.areaOK
	}
	u.areaVal, u.areaOK = LoadOfflineArea(root)
	u.areaAt = time.Now()
	return u.areaVal, u.areaOK
}

func runGUI(core *Core) {
	go func() {
		w := new(app.Window)
		w.Option(app.Title("AEGIS Companion"), app.Size(1200, 780), app.MinSize(940, 620))
		u := newUI(core)
		// One update check per run (v1.7.1): fetches only the tiny
		// latest.json; nothing downloads without a click.
		startUpdateCheck(core.Cfg)
		// Live data changes outside the frame loop; keep the window fresh.
		go func() {
			t := time.NewTicker(600 * time.Millisecond)
			defer t.Stop()
			for range t.C {
				w.Invalidate()
			}
		}()
		var ops op.Ops
		for {
			switch e := w.Event().(type) {
			case app.DestroyEvent:
				if e.Err != nil {
					core.Log.Errorf("window closed with error: %v", e.Err)
				}
				// Exit NOW (v1.7.3): Server.Stop() used to run first
				// and waits for background report rebuilds - a stuck
				// rebuild left the process alive with no window, so
				// the app looked "minimized forever" after closing.
				// os.Exit ends every goroutine anyway.
				core.Log.Infof("window closed - exiting")
				os.Exit(0)
			case app.FrameEvent:
				gtx := app.NewContext(&ops, e)
				u.layout(gtx)
				e.Frame(gtx.Ops)
			}
		}
	}()
	app.Main()
}

// ---- shared building blocks ----

func (u *UI) label(size unit.Sp, col color.NRGBA, txt string) material.LabelStyle {
	l := material.Label(u.th, size, txt)
	l.Color = col
	return l
}

func (u *UI) bold(size unit.Sp, col color.NRGBA, txt string) material.LabelStyle {
	l := u.label(size, col, txt)
	l.Font.Weight = font.Bold
	return l
}

// bgFill paints col behind the child, sized to the child's own height and
// the full available width (for full-width rows/cards).
func bgFill(gtx layout.Context, col color.NRGBA, radius unit.Dp, w layout.Widget) layout.Dimensions {
	cs := gtx.Constraints
	macro := op.Record(gtx.Ops)
	dims := w(gtx)
	call := macro.Stop()
	sz := image.Point{X: cs.Max.X, Y: dims.Size.Y}
	r := gtx.Dp(radius)
	if r > sz.Y/2 {
		r = sz.Y / 2
	}
	if r > sz.X/2 {
		r = sz.X / 2
	}
	rect := clip.RRect{Rect: image.Rectangle{Max: sz}, NW: r, NE: r, SE: r, SW: r}
	paint.FillShape(gtx.Ops, col, rect.Op(gtx.Ops))
	call.Add(gtx.Ops)
	return layout.Dimensions{Size: sz}
}

func pad(v unit.Dp, w layout.Widget) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: v, Bottom: v, Left: v, Right: v}.Layout(gtx, w)
	}
}

func hDivider(gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(1)
	sz := image.Pt(gtx.Constraints.Max.X, h)
	paint.FillShape(gtx.Ops, colEdge, clip.Rect{Max: sz}.Op())
	return layout.Dimensions{Size: sz}
}

func vSpace(gtx layout.Context, h unit.Dp) layout.Dimensions {
	return layout.Dimensions{Size: image.Pt(0, gtx.Dp(h))}
}

// hSpace is a horizontal spacer widget for Flex rows.
func hSpace(w unit.Dp) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(gtx.Dp(w), 0)}
	}
}

func phoenixMark(gtx layout.Context, cell unit.Dp) layout.Dimensions {
	c := float32(gtx.Dp(cell))
	size := image.Pt(int(c*16), int(c*16))
	for y, row := range kPhoenix16 {
		for x, ch := range row {
			var col color.NRGBA
			switch ch {
			case 'C':
				col = colCyan
			case 'W':
				col = colText
			default:
				continue
			}
			rect := clip.Rect{
				Min: image.Pt(int(float32(x)*c), int(float32(y)*c)),
				Max: image.Pt(int(float32(x+1)*c+0.5), int(float32(y+1)*c+0.5)),
			}
			paint.FillShape(gtx.Ops, col, rect.Op())
		}
	}
	return layout.Dimensions{Size: size}
}

// pill draws a proper rounded badge: background sized by the text, corner
// radius exactly half the height (a 99dp radius on a 20px chip makes the
// rounded-rect path self-intersect into bowties - learned from a screenshot).
func pill(gtx layout.Context, u *UI, txt string, col color.NRGBA) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := gtx.Constraints.Min
			r := sz.Y / 2
			rr := clip.RRect{Rect: image.Rectangle{Max: sz}, NW: r, NE: r, SE: r, SW: r}
			paint.FillShape(gtx.Ops, colPanel2, rr.Op(gtx.Ops))
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 4, Bottom: 4, Left: 8, Right: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return u.label(11, col, "● "+txt).Layout(gtx)
			})
		}),
	)
}

func chip(gtx layout.Context, u *UI, txt string, col color.NRGBA) layout.Dimensions {
	return pill(gtx, u, txt, col)
}

func stateBadge(gtx layout.Context, u *UI, state string, online bool) layout.Dimensions {
	col := colFaint
	txt := "OFFLINE"
	if online {
		col = stateColor(state)
		txt = strings.ToUpper(state)
		if txt == "" {
			txt = "ONLINE"
		}
		// scanning/alert pulse: breathe the dot by wall-clock second
		if (state == "scanning" || state == "alert") && time.Now().UnixMilli()/600%2 == 0 {
			col.A = 0x90
		}
	}
	return pill(gtx, u, txt, col)
}

func batteryBar(gtx layout.Context, u *UI, pct int, charging bool, width unit.Dp) layout.Dimensions {
	w := gtx.Dp(width)
	h := gtx.Dp(10)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	barCol := colGreen
	if pct < 30 {
		barCol = colAmber
	}
	if pct < 15 {
		barCol = colRed
	}
	// track
	paint.FillShape(gtx.Ops, colPanel2, clip.RRect{
		Rect: image.Rectangle{Max: image.Pt(w, h)}, NW: 3, NE: 3, SE: 3, SW: 3}.Op(gtx.Ops))
	// fill
	fw := w * pct / 100
	if fw > 0 {
		paint.FillShape(gtx.Ops, barCol, clip.RRect{
			Rect: image.Rectangle{Max: image.Pt(fw, h)}, NW: 3, NE: 3, SE: 3, SW: 3}.Op(gtx.Ops))
	}
	lbl := fmt.Sprintf("%d%%", pct)
	if charging {
		lbl += " ⚡"
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: image.Pt(w, h)}
		}),
		layout.Rigid(hSpace(6)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.label(11, colDim, lbl).Layout(gtx)
		}),
	)
}

// spark draws a line graph of pts (auto-scaled) at the given size.
func spark(gtx layout.Context, wDp, hDp unit.Dp, pts []float32, col color.NRGBA) layout.Dimensions {
	w := gtx.Dp(wDp)
	h := gtx.Dp(hDp)
	sz := image.Pt(w, h)
	if len(pts) == 0 {
		return layout.Dimensions{Size: sz}
	}
	lo, hi := pts[0], pts[0]
	for _, p := range pts {
		if p < lo {
			lo = p
		}
		if p > hi {
			hi = p
		}
	}
	if hi-lo < 0.001 {
		hi = lo + 1
	}
	// baseline
	paint.FillShape(gtx.Ops, colEdge, clip.Rect{Min: image.Pt(0, h-1), Max: image.Pt(w, h)}.Op())
	var path clip.Path
	path.Begin(gtx.Ops)
	stepX := float32(w) / float32(max(1, len(pts)-1))
	for i, p := range pts {
		x := float32(i) * stepX
		y := float32(h) - 2 - (p-lo)/(hi-lo)*float32(h-4)
		if i == 0 {
			path.MoveTo(f32.Pt(x, y))
		} else {
			path.LineTo(f32.Pt(x, y))
		}
	}
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: path.End(), Width: 1.6}.Op())
	return layout.Dimensions{Size: sz}
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < 2*time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

// openPath opens a file/folder/URL with the OS default handler.
func openPath(target string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	_ = cmd.Start()
}

// ---- shell layout ----

func (u *UI) layout(gtx layout.Context) layout.Dimensions {
	u.watchToastSources()
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			paint.FillShape(gtx.Ops, colBg, clip.Rect{Max: gtx.Constraints.Min}.Op())
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(u.sidebar),
				layout.Flexed(1, u.content),
			)
		}),
	)
}

func (u *UI) sidebar(gtx layout.Context) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Dp(216)
	gtx.Constraints.Max.X = gtx.Dp(216)
	return bgFill(gtx, colSidebar, 0, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: 18, Bottom: 6, Left: 18}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return phoenixMark(gtx, 3.4)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: 18}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return u.bold(21, colText, "AEGIS").Layout(gtx)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: 18, Bottom: 16}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return u.label(11, colCyan, "C O M P A N I O N").Layout(gtx)
				})
			}),
			layout.Rigid(hDivider),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 8) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.navList(gtx)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: gtx.Constraints.Min}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: 14, Right: 14, Bottom: 14}.Layout(gtx, u.listenerCard)
			}),
		)
	})
}

func (u *UI) navList(gtx layout.Context) layout.Dimensions {
	items := []struct {
		id    pageID
		label string
		badge func() string
	}{
		{pgDash, "Dashboard", nil},
		{pgLive, "Live Monitor", func() string {
			if n := u.core.Live.OnlineCount(); n > 0 {
				return fmt.Sprint(n)
			}
			return ""
		}},
		{pgMap, "Wardrive Map", func() string {
			if u.mapData != nil && len(u.mapData.Points) > 0 {
				return fmt.Sprint(len(u.mapData.Points))
			}
			return ""
		}},
		{pgLibrary, "Library", nil},
		{pgReports, "Reports", nil},
		{pgLog, "Activity Log", nil},
		{pgSettings, "Settings", nil},
	}
	widgets := make([]layout.FlexChild, 0, len(items))
	for _, it := range items {
		it := it
		widgets = append(widgets, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			click := u.navClicks[it.id]
			if click.Clicked(gtx) {
				u.page = it.id
			}
			active := u.page == it.id
			return material.Clickable(gtx, click, func(gtx layout.Context) layout.Dimensions {
				bg := color.NRGBA{}
				if active {
					bg = colPanel2
				}
				return bgFill(gtx, bg, 8, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: 8, Bottom: 8, Left: 16, Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								dot := colFaint
								if active {
									dot = colCyan
								}
								paint.FillShape(gtx.Ops, dot, clip.RRect{
									Rect: image.Rectangle{Max: image.Pt(gtx.Dp(7), gtx.Dp(7))},
									NW:   4, NE: 4, SE: 4, SW: 4}.Op(gtx.Ops))
								return layout.Dimensions{Size: image.Pt(gtx.Dp(7), gtx.Dp(7))}
							}),
							layout.Rigid(hSpace(10)),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								c := colDim
								if active {
									c = colText
								}
								return u.label(13.5, c, it.label).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if it.badge == nil || it.badge() == "" {
									return layout.Dimensions{}
								}
								return chip(gtx, u, it.badge(), colGreen)
							}),
						)
					})
				})
			})
		}))
	}
	return layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, widgets...)
	})
}

func (u *UI) listenerCard(gtx layout.Context) layout.Dimensions {
	port, listening := u.core.Server.Listening()
	return bgFill(gtx, colPanel, 10, pad(10, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if listening {
					return u.label(12, colGreen, fmt.Sprintf("● Listening on :%d", port)).Layout(gtx)
				}
				return u.label(12, colRed, "● Not listening - see Settings").Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				n := u.core.Live.OnlineCount()
				return u.label(11.5, colDim, fmt.Sprintf("%d device(s) live now", n)).Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.label(11.5, colFaint, "TALON is never linked, by design").Layout(gtx)
			}),
		)
	}))
}

func (u *UI) content(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 16, Bottom: 8, Left: 22, Right: 22}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return u.bold(20, colText, pageTitles[u.page]).Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						files, _, _ := u.core.Store.TodayStats()
						return u.label(12, colDim, fmt.Sprintf("%s · %d file(s) synced today",
							time.Now().Format("Mon 2 Jan 2006"), files)).Layout(gtx)
					}),
				)
			})
		}),
		layout.Rigid(hDivider),
		layout.Rigid(u.toastBanner),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 14, Left: 22, Right: 22, Bottom: 16}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				switch u.page {
				case pgDash:
					return u.pageDashboard(gtx)
				case pgLive:
					return u.pageLive(gtx)
				case pgMap:
					return u.pageMap(gtx)
				case pgLibrary:
					return u.pageLibrary(gtx)
				case pgReports:
					return u.pageReports(gtx)
				case pgLog:
					return u.pageLog(gtx)
				case pgSettings:
					return u.pageSettings(gtx)
				}
				return layout.Dimensions{}
			})
		}),
	)
}
