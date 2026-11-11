package main

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// Wardrive Map page. Real street tiles by default: the saved home map
// (100x100 miles, downloaded once) when it covers the drive, live tiles
// otherwise; the schematic plot is the offline fallback. Every row can
// jump straight into Google Maps at that network's best position.

func rssiNRGBA(rssi int) color.NRGBA {
	switch {
	case rssi >= -60:
		return colGreen
	case rssi >= -75:
		return colAmber
	default:
		return colRed
	}
}

func (u *UI) mapEnsure() MapData {
	size := u.core.Store.SizeBytes()
	if u.mapData == nil || size != u.mapSize {
		d := LoadMapData(u.core.Store.Root())
		u.mapData = &d
		u.mapSize = size
	}
	return *u.mapData
}

// mapFallbackCenter is where the map centres when there is no
// sighting data yet (v1.8.5): Settings home first, then the saved
// home-map's centre. ok=false when data exists (no fallback needed)
// or no centre is known at all.
func (u *UI) mapFallbackCenter(d MapData) (float64, float64, bool) {
	if len(d.Points) > 0 || len(d.Track) > 0 {
		return 0, 0, false
	}
	if u.core.Cfg.HomeLat != 0 || u.core.Cfg.HomeLon != 0 {
		return u.core.Cfg.HomeLat, u.core.Cfg.HomeLon, true
	}
	if a, ok := u.offlineArea(u.core.Cfg.Library); ok && (a.HomeLat != 0 || a.HomeLon != 0) {
		return a.HomeLat, a.HomeLon, true
	}
	// Last resort (v1.8.6): no home anywhere yet - still show a real
	// map (wide US view) instead of an empty box. mapCentreIsHome
	// keeps the HOME marker honest; the banner tells you to set home.
	return 39.8, -98.6, true
}

// mapCentreIsHome reports whether the empty-map centre is the
// user's actual home (vs the wide last-resort view).
func (u *UI) mapCentreIsHome(d MapData) bool {
	if len(d.Points) > 0 || len(d.Track) > 0 {
		return false
	}
	if u.core.Cfg.HomeLat != 0 || u.core.Cfg.HomeLon != 0 {
		return true
	}
	if a, ok := u.offlineArea(u.core.Cfg.Library); ok && (a.HomeLat != 0 || a.HomeLon != 0) {
		return true
	}
	return false
}

// mapHalfMiles is the framing half-size for the home map: the
// saved area's own size when one exists, else the chosen size.
func (u *UI) mapHalfMiles() float64 {
	if a, ok := u.offlineArea(u.core.Cfg.Library); ok && a.HalfMiles > 0 {
		return a.HalfMiles
	}
	return u.core.Cfg.homeHalfMiles()
}

// drawHomeMark paints the amber HOME marker at a projected point,
// skipping quietly when home projects off-canvas.
func (u *UI) drawHomeMark(gtx layout.Context, hp f32.Point, w, h int) {
	if hp.X < -24 || hp.Y < -24 || hp.X > float32(w)+24 || hp.Y > float32(h)+24 {
		return
	}
	hr := 7
	paint.FillShape(gtx.Ops, colAmber, clip.Ellipse{
		Min: image.Pt(int(hp.X)-hr, int(hp.Y)-hr),
		Max: image.Pt(int(hp.X)+hr, int(hp.Y)+hr),
	}.Op(gtx.Ops))
	ring := clip.Ellipse{
		Min: image.Pt(int(hp.X)-hr-4, int(hp.Y)-hr-4),
		Max: image.Pt(int(hp.X)+hr+4, int(hp.Y)+hr+4),
	}
	paint.FillShape(gtx.Ops, colAmber, clip.Stroke{Path: ring.Path(gtx.Ops), Width: 1.8}.Op())
	func() {
		defer op.Offset(image.Pt(int(hp.X)+14, int(hp.Y)-8)).Push(gtx.Ops).Pop()
		u.bold(11, colAmber, "HOME").Layout(gtx)
	}()
}

func (u *UI) pageMap(gtx layout.Context) layout.Dimensions {
	d := u.mapEnsure()
	fbLat, fbLon, fbOK := u.mapFallbackCenter(d)

	if u.btnMapRefresh.Clicked(gtx) {
		u.mapData = nil
		d = u.mapEnsure()
	}
	if u.btnMapMode.Clicked(gtx) {
		u.mapReal = !u.mapReal
	}
	if u.btnZoomIn.Clicked(gtx) && u.mapZoom < 3 {
		u.mapZoom++
	}
	if u.btnZoomOut.Clicked(gtx) && u.mapZoom > -2 {
		u.mapZoom--
	}
	if u.btnMapWeb.Clicked(gtx) && (len(d.Points) > 0 || fbOK) {
		dd := d
		if len(dd.Points) == 0 && fbOK {
			dd.CenterLat, dd.CenterLon = fbLat, fbLon
		}
		if path, err := WriteMapHTML(u.core.Store.Root(), dd); err == nil {
			openPath(path)
			u.core.Log.Infof("wardrive map written: %s", path)
		} else {
			u.core.Log.Errorf("wardrive map: %v", err)
		}
	}
	if u.btnMapGMaps.Clicked(gtx) && (len(d.Points) > 0 || fbOK) {
		clat, clon := d.CenterLat, d.CenterLon
		if len(d.Points) == 0 && fbOK {
			clat, clon = fbLat, fbLon
		}
		openPath(GoogleMapsURL(clat, clon))
	}
	if u.btnMapShare.Clicked(gtx) {
		if len(d.Points) == 0 {
			u.mapMsg = "Nothing to share yet - wardrive first, then export."
		} else if dir, err := WriteShareExports(u.core.Store.Root(), d); err == nil {
			u.mapMsg = "Shared to " + dir + " (GPX + GeoJSON + CSV - open in Meshtastic / MeshCore tools or any map app)."
			openPath(dir)
		} else {
			u.mapMsg = "Share export failed: " + err.Error()
			u.core.Log.Errorf("share export: %v", err)
		}
	}
	if u.btnSavedOnly.Clicked(gtx) {
		u.savedOnly = !u.savedOnly
		u.tiles, u.tilesBusy = nil, ""
		if u.savedOnly {
			if _, ok := u.offlineArea(u.core.Cfg.Library); !ok {
				u.mapMsg = "No saved home map yet - save one below (or in Settings) and this switch will use it."
			} else {
				u.mapMsg = ""
			}
		}
	}
	if u.btnSaveArea.Clicked(gtx) {
		cfg := u.core.Cfg
		if cfg.HomeLat == 0 && cfg.HomeLon == 0 {
			u.mapMsg = "First set your home location in Settings > Wardrive map - the saved map is a " + cfg.areaSizeLabel() + " square around it."
		} else {
			root := cfg.Library
			u.areaProg.StartR(root, cfg.HomeLat, cfg.HomeLon, cfg.Token, cfg.homeHalfMiles(), func() {
				u.tiles, u.tilesBusy = nil, ""
			})
		}
	}
	if u.btnCancelArea.Clicked(gtx) {
		u.areaProg.Cancel()
	}

	// v1.8.5: no data no longer replaces the page with an empty panel.
	// The full map (home area, 100x100 miles) renders underneath and
	// sightings decorate it as they arrive.
	emptyData := len(d.Points) == 0 && len(d.Track) == 0

	stats := []struct {
		title, value string
		col          color.NRGBA
	}{
		{"GPS-TAGGED NETWORKS", fmt.Sprint(len(d.Points)), colGreen},
		{"ROUTE POINTS", fmt.Sprint(len(d.Track)), colCyan},
		{"ROUTE LENGTH", fmt.Sprintf("%.2f km", d.RouteKm), colCyan},
		{"SIGHTINGS WITHOUT GPS", fmt.Sprint(d.NoGPS), colAmber},
		{"WI-FI POSITION EST.", fmt.Sprint(len(d.EstTrack)), colAmber},
		{"MAP FILES READ", fmt.Sprint(d.Files), colCyan},
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.artBadge(gtx, "PHOENIX", 1.3)
				}),
				layout.Rigid(hSpace(10)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.label(12, colDim, "Recorded by PHOENIX wardrives - each pin is where that network's signal was strongest (GPS best-sighting).").Layout(gtx)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 10) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			children := make([]layout.FlexChild, 0, len(stats))
			for i, s := range stats {
				s := s
				last := i == len(stats)-1
				children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					wrap := func(gtx layout.Context) layout.Dimensions {
						return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return u.label(10.5, colFaint, s.title).Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return u.bold(22, s.col, s.value).Layout(gtx)
								}),
							)
						}))
					}
					if last {
						return wrap(gtx)
					}
					return layout.Inset{Right: 10}.Layout(gtx, wrap)
				}))
			}
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, children...)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !emptyData {
				return vSpace(gtx, 12)
			}
			return layout.Inset{Top: 12}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
					msg := "No GPS-tagged sightings yet — below is your full home map (the whole " + u.core.Cfg.areaSizeLabel() + " area). Wardrive with PHOENIX outdoors (GNSS fix, sky view), come home, and pins + your route land on this same map automatically after sync. You can also drop an SD card in via Library → Import."
					if !u.mapCentreIsHome(d) {
						msg = "No GPS-tagged sightings yet, and no home location is set — showing a wide map for now. Set home in Settings → Wardrive map and this page centres on your full " + u.core.Cfg.areaSizeLabel() + " home map even before the first drive."
					}
					return u.label(12, colDim, msg).Layout(gtx)
				}))
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 12) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if u.mapReal {
				return u.mapPlotAuto(gtx, d)
			}
			return u.mapPlot(gtx, d)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 8) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			modeTxt := "View: schematic plot"
			if !u.mapReal {
				modeTxt = "View: real map"
			}
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnMapMode, modeTxt).Layout(gtx)
				}),
				layout.Rigid(hSpace(8)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnZoomOut, "-").Layout(gtx)
				}),
				layout.Rigid(hSpace(4)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnZoomIn, "+").Layout(gtx)
				}),
				layout.Rigid(hSpace(8)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btn(&u.btnMapWeb, "Open interactive map (browser)").Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hSpace(8)(gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnMapGMaps, "Center in Google Maps").Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hSpace(8)(gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnMapShare, "Share (Meshtastic/MeshCore)").Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hSpace(8)(gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnMapRefresh, "Refresh").Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.E.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						note := "Schematic plot - works offline."
						if u.mapReal && u.savedOnly {
							if _, ok := u.offlineArea(u.core.Cfg.Library); !ok {
								note = "Saved-map mode: no home map saved yet - schematic shown."
							} else {
								note = "Saved home map - no internet needed."
								if u.tiles != nil && u.tiles.Partial {
									note = "Part of this drive is outside your saved home map."
								} else if u.tiles != nil && u.tiles.Err {
									note = "Outside your saved home map - schematic shown."
								}
							}
						} else if u.mapReal {
							note = "Real street map (\u00a9 OpenStreetMap contributors)."
							if u.tiles != nil && u.tiles.Err {
								note = "Real map needs internet once - schematic shown meanwhile."
							} else if u.tiles != nil && u.tiles.Loading {
								note = "Loading real street map\u2026"
							}
						}
						return u.label(11, colFaint, note).Layout(gtx)
					})
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 8) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.mapAreaStrip(gtx)
		}),
		sectionTitle(u, fmt.Sprintf("Networks by strongest signal (%d) - click one to open it in Google Maps", len(d.Points))),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return withScrollBar(gtx, &u.mapList, func(gtx layout.Context) layout.Dimensions {
				return material.List(u.th, &u.mapList).Layout(gtx, len(d.Points), func(gtx layout.Context, i int) layout.Dimensions {
					p := d.Points[i]
					click := u.mapRows.get(p.BSSID)
					if click.Clicked(gtx) {
						openPath(GoogleMapsURL(p.Lat, p.Lon))
					}
					return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return material.Clickable(gtx, click, func(gtx layout.Context) layout.Dimensions {
							return bgFill(gtx, colPanel, 8, pad(10, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										sz := gtx.Dp(10)
										paint.FillShape(gtx.Ops, rssiNRGBA(p.RSSI),
											clip.Ellipse{Min: image.Pt(0, 0), Max: image.Pt(sz, sz)}.Op(gtx.Ops))
										return layout.Dimensions{Size: image.Pt(sz, sz)}
									}),
									layout.Rigid(hSpace(10)),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										ssid := p.SSID
										if ssid == "" {
											ssid = "(hidden network)"
										}
										return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return u.bold(13, colText, ssid).Layout(gtx)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return u.label(11, colDim, fmt.Sprintf("%s \u00b7 ch %d \u00b7 seen %dx \u00b7 %s",
													p.BSSID, p.Channel, p.Sightings, p.Day)).Layout(gtx)
											}),
										)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return u.bold(12.5, rssiNRGBA(p.RSSI), fmt.Sprintf("%d dBm", p.RSSI)).Layout(gtx)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												txt := fmt.Sprintf("%.5f, %.5f", p.Lat, p.Lon)
												if p.UncertaintyM > 0 {
													txt += fmt.Sprintf("  ±%.0f m · %d fix", p.UncertaintyM, p.Observations)
													if p.Observations != 1 {
														txt += "es"
													}
												}
												return u.label(11, colDim, txt).Layout(gtx)
											}),
										)
									}),
									layout.Rigid(hSpace(10)),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return u.label(11.5, colCyan, "Google Maps \u2197").Layout(gtx)
									}),
								)
							}))
						})
					})
				})
			})
		}),
	)
}

func (u *UI) tileKeyFor(d MapData) string {
	fbLat, fbLon, fbOK := u.mapFallbackCenter(d)
	minLat, minLon, maxLat, maxLon := viewBoundsR(d, fbLat, fbLon, fbOK, u.mapHalfMiles())
	return fmt.Sprintf("%.4f,%.4f,%.4f,%.4f,z%d", minLat, minLon, maxLat, maxLon, u.mapZoom)
}

// mapPlotAuto shows real tiles when available and falls back to the
// schematic while they load or when offline.
func (u *UI) mapPlotAuto(gtx layout.Context, d MapData) layout.Dimensions {
	key := u.tileKeyFor(d)
	offline := false
	var area OfflineArea
	if u.savedOnly {
		if a, ok := u.offlineArea(u.core.Store.Root()); ok {
			offline, area = true, a
			key += "|off"
		}
	}
	if u.tiles == nil || u.tiles.Key != key {
		if u.tilesBusy != key {
			u.tilesBusy = key
			u.tiles = &tileSet{Key: key, Loading: true}
			root := u.core.Store.Root()
			dd, zd := d, u.mapZoom
			fLat, fLon, fOK := u.mapFallbackCenter(dd)
			go func() {
				var ts *tileSet
				if offline {
					ts = buildTileSetCachedFBR(root, dd, zd, area, fLat, fLon, fOK, u.mapHalfMiles())
				} else {
					ts = buildTileSetFBR(root, dd, zd, fLat, fLon, fOK, u.mapHalfMiles())
				}
				ts.Key = key
				u.tiles = ts
				if u.tilesBusy == key {
					u.tilesBusy = ""
				}
			}()
		}
	}
	ts := u.tiles
	if ts == nil || ts.Loading || ts.Err || ts.Img == nil {
		return u.mapPlot(gtx, d)
	}
	return u.mapPlotReal(gtx, d, ts)
}

// mapPlotReal draws the composited street tiles with the same route +
// best-sighting pins as the schematic.
func (u *UI) mapPlotReal(gtx layout.Context, d MapData, ts *tileSet) layout.Dimensions {
	h := gtx.Dp(unit.Dp(300))
	w := gtx.Constraints.Max.X
	if w <= 0 {
		w = gtx.Dp(unit.Dp(600))
	}
	sz := image.Pt(w, h)
	paint.FillShape(gtx.Ops, colPanel, clip.RRect{
		Rect: image.Rectangle{Max: sz}, NW: 10, NE: 10, SE: 10, SW: 10,
	}.Op(gtx.Ops))

	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	iw, ih := float64(ts.Img.Bounds().Dx()), float64(ts.Img.Bounds().Dy())
	gtxImg := gtx
	gtxImg.Constraints.Min = sz
	gtxImg.Constraints.Max = sz
	widget.Image{Src: paint.NewImageOp(ts.Img), Fit: widget.Cover}.Layout(gtxImg)
	// gentle dark wash so pins and route read on bright tiles
	paint.FillShape(gtx.Ops, color.NRGBA{0x0B, 0x0E, 0x14, 0x46},
		clip.Rect{Max: sz}.Op())

	s := math.Max(float64(w)/iw, float64(h)/ih)
	proj := func(lat, lon float64) f32.Point {
		ix := lonToWorldX(lon, ts.Zoom) - ts.OriginX
		iy := latToWorldY(lat, ts.Zoom) - ts.OriginY
		return f32.Pt(float32((ix-iw/2)*s+float64(w)/2), float32((iy-ih/2)*s+float64(h)/2))
	}
	// Empty drive, full map (v1.8.5): mark HOME at the centre of the
	// home view so the page reads as a place, not a void.
	if fbLat, fbLon, fbOK := u.mapFallbackCenter(d); fbOK {
		u.drawHomeMark(gtx, proj(fbLat, fbLon), w, h)
	}
	// With sightings on the map, home still gets its marker when it
	// is set (v1.8.12 - it used to vanish the moment data existed,
	// which read as "the map lost my location").
	if (len(d.Points) > 0 || len(d.Track) > 0) && (u.core.Cfg.HomeLat != 0 || u.core.Cfg.HomeLon != 0) {
		u.drawHomeMark(gtx, proj(u.core.Cfg.HomeLat, u.core.Cfg.HomeLon), w, h)
	}
	if len(d.Track) > 1 {
		var path clip.Path
		path.Begin(gtx.Ops)
		trackBreaks := segBreaks(d.TrackSeg)
		var prevTp [2]float64
		havePrev := false
		for i, tp := range d.Track {
			if !validLatLon(tp[0], tp[1]) {
				continue
			}
			// Lift the pen at file/day breaks and >3 km jumps
			// (v1.8.12): drives are segments, not one glued line.
			if !havePrev || trackBreaks[i] || distanceM(prevTp[0], prevTp[1], tp[0], tp[1]) > 3000 {
				path.MoveTo(proj(tp[0], tp[1]))
			} else {
				path.LineTo(proj(tp[0], tp[1]))
			}
			prevTp, havePrev = tp, true
		}
		paint.FillShape(gtx.Ops, colCyan, clip.Stroke{Path: path.End(), Width: 2.4}.Op())
	}
	// Wi-Fi position estimates: amber (v1.8.7).
	if len(d.EstTrack) > 1 {
		var path clip.Path
		path.Begin(gtx.Ops)
		estBreaks := segBreaks(d.EstSeg)
		var prevEt [2]float64
		havePrevEt := false
		for i, tp := range d.EstTrack {
			if !validLatLon(tp[0], tp[1]) {
				continue
			}
			if !havePrevEt || estBreaks[i] || distanceM(prevEt[0], prevEt[1], tp[0], tp[1]) > 3000 {
				path.MoveTo(proj(tp[0], tp[1]))
			} else {
				path.LineTo(proj(tp[0], tp[1]))
			}
			prevEt, havePrevEt = tp, true
		}
		paint.FillShape(gtx.Ops, colAmber, clip.Stroke{Path: path.End(), Width: 1.8}.Op())
	}
	for i := len(d.Points) - 1; i >= 0; i-- {
		p := d.Points[i]
		cp := proj(p.Lat, p.Lon)
		r := 6
		if i == 0 {
			r = 8
		}
		rect := clip.Ellipse{
			Min: image.Pt(int(cp.X)-r, int(cp.Y)-r),
			Max: image.Pt(int(cp.X)+r, int(cp.Y)+r),
		}
		paint.FillShape(gtx.Ops, rssiNRGBA(p.RSSI), rect.Op(gtx.Ops))
		if i == 0 {
			ring := clip.Ellipse{
				Min: image.Pt(int(cp.X)-r-3, int(cp.Y)-r-3),
				Max: image.Pt(int(cp.X)+r+3, int(cp.Y)+r+3),
			}
			paint.FillShape(gtx.Ops, colText, clip.Stroke{Path: ring.Path(gtx.Ops), Width: 1.8}.Op())
		}
	}
	// attribution strip
	paint.FillShape(gtx.Ops, color.NRGBA{0x0B, 0x0E, 0x14, 0xC8},
		clip.Rect{Min: image.Pt(0, h-18), Max: image.Pt(w, h)}.Op())
	func() {
		defer op.Offset(image.Pt(8, h-16)).Push(gtx.Ops).Pop()
		u.label(9.5, colText, "\u00a9 OpenStreetMap contributors \u00b7 pins: GPS best-sighting \u00b7 line: your route").Layout(gtx)
	}()
	return layout.Dimensions{Size: sz}
}

// mapPlot draws the offline schematic: route polyline + one dot per
// network at its best-sighting position, colored by signal strength.
func (u *UI) mapPlot(gtx layout.Context, d MapData) layout.Dimensions {
	h := gtx.Dp(unit.Dp(300))
	w := gtx.Constraints.Max.X
	if w <= 0 {
		w = gtx.Dp(unit.Dp(600))
	}
	sz := image.Pt(w, h)

	// bounds over points + track (robustly trimmed, v1.8.12: one
	// far-flung bad fix no longer stretches the frame)
	minLat, minLon, maxLat, maxLon, hasFrame := frameBounds(d)
	homeView := false
	var homeLat, homeLon float64
	if !hasFrame {
		// No sightings: frame the full 100x100-mile home map instead
		// of an empty box (v1.8.5).
		fbLat, fbLon, fbOK := u.mapFallbackCenter(d)
		if !fbOK {
			paint.FillShape(gtx.Ops, colPanel, clip.RRect{
				Rect: image.Rectangle{Max: sz}, NW: 10, NE: 10, SE: 10, SW: 10,
			}.Op(gtx.Ops))
			return layout.Dimensions{Size: sz}
		}
		minLat, minLon, maxLat, maxLon = areaBounds(fbLat, fbLon, u.mapHalfMiles())
		homeView, homeLat, homeLon = true, fbLat, fbLon
	}
	// pad 10% and guard degenerate (single point) bounds
	padLat := math.Max((maxLat-minLat)*0.1, 0.002)
	padLon := math.Max((maxLon-minLon)*0.1, 0.002)
	minLat, maxLat = minLat-padLat, maxLat+padLat
	minLon, maxLon = minLon-padLon, maxLon+padLon
	proj := func(lat, lon float64) f32.Point {
		x := float32((lon - minLon) / (maxLon - minLon) * float64(w))
		y := float32(float64(h) - (lat-minLat)/(maxLat-minLat)*float64(h))
		return f32.Pt(x, y)
	}

	// panel background + border grid
	paint.FillShape(gtx.Ops, colPanel, clip.RRect{
		Rect: image.Rectangle{Max: sz}, NW: 10, NE: 10, SE: 10, SW: 10,
	}.Op(gtx.Ops))
	for i := 1; i < 4; i++ {
		gx := w * i / 4
		gy := h * i / 4
		paint.FillShape(gtx.Ops, colEdge, clip.Rect{Min: image.Pt(gx, 0), Max: image.Pt(gx+1, h)}.Op())
		paint.FillShape(gtx.Ops, colEdge, clip.Rect{Min: image.Pt(0, gy), Max: image.Pt(w, gy+1)}.Op())
	}
	// route
	if len(d.Track) > 1 {
		var path clip.Path
		path.Begin(gtx.Ops)
		trackBreaks := segBreaks(d.TrackSeg)
		var prevTp [2]float64
		havePrev := false
		for i, tp := range d.Track {
			if !validLatLon(tp[0], tp[1]) {
				continue
			}
			// Lift the pen at file/day breaks and >3 km jumps
			// (v1.8.12): drives are segments, not one glued line.
			if !havePrev || trackBreaks[i] || distanceM(prevTp[0], prevTp[1], tp[0], tp[1]) > 3000 {
				path.MoveTo(proj(tp[0], tp[1]))
			} else {
				path.LineTo(proj(tp[0], tp[1]))
			}
			prevTp, havePrev = tp, true
		}
		paint.FillShape(gtx.Ops, colCyan, clip.Stroke{Path: path.End(), Width: 2}.Op())
	}
	// Wi-Fi position estimates (v1.8.7): amber, under the pins.
	if len(d.EstTrack) > 1 {
		var path clip.Path
		path.Begin(gtx.Ops)
		estBreaks := segBreaks(d.EstSeg)
		var prevEt [2]float64
		havePrevEt := false
		for i, tp := range d.EstTrack {
			if !validLatLon(tp[0], tp[1]) {
				continue
			}
			if !havePrevEt || estBreaks[i] || distanceM(prevEt[0], prevEt[1], tp[0], tp[1]) > 3000 {
				path.MoveTo(proj(tp[0], tp[1]))
			} else {
				path.LineTo(proj(tp[0], tp[1]))
			}
			prevEt, havePrevEt = tp, true
		}
		paint.FillShape(gtx.Ops, colAmber, clip.Stroke{Path: path.End(), Width: 1.6}.Op())
	}
	// points (weakest first so strongest sit on top)
	for i := len(d.Points) - 1; i >= 0; i-- {
		p := d.Points[i]
		c := proj(p.Lat, p.Lon)
		r := 5
		if i == 0 {
			r = 7
		}
		rect := clip.Ellipse{
			Min: image.Pt(int(c.X)-r, int(c.Y)-r),
			Max: image.Pt(int(c.X)+r, int(c.Y)+r),
		}
		paint.FillShape(gtx.Ops, rssiNRGBA(p.RSSI), rect.Op(gtx.Ops))
		if i == 0 {
			ring := clip.Ellipse{
				Min: image.Pt(int(c.X)-r-3, int(c.Y)-r-3),
				Max: image.Pt(int(c.X)+r+3, int(c.Y)+r+3),
			}
			paint.FillShape(gtx.Ops, colText, clip.Stroke{Path: ring.Path(gtx.Ops), Width: 1.6}.Op())
		}
	}
	// HOME marker on the empty full-map view (v1.8.5; only when the
	// centre really is home - never on the wide last-resort view)
	if homeView && u.mapCentreIsHome(d) {
		hp := proj(homeLat, homeLon)
		hr := 7
		paint.FillShape(gtx.Ops, colAmber, clip.Ellipse{
			Min: image.Pt(int(hp.X)-hr, int(hp.Y)-hr),
			Max: image.Pt(int(hp.X)+hr, int(hp.Y)+hr),
		}.Op(gtx.Ops))
		ring := clip.Ellipse{
			Min: image.Pt(int(hp.X)-hr-4, int(hp.Y)-hr-4),
			Max: image.Pt(int(hp.X)+hr+4, int(hp.Y)+hr+4),
		}
		paint.FillShape(gtx.Ops, colAmber, clip.Stroke{Path: ring.Path(gtx.Ops), Width: 1.8}.Op())
		func() {
			defer op.Offset(image.Pt(int(hp.X)+14, int(hp.Y)-8)).Push(gtx.Ops).Pop()
			u.bold(11, colAmber, "HOME").Layout(gtx)
		}()
		func() {
			defer op.Offset(image.Pt(10, h-20)).Push(gtx.Ops).Pop()
			u.label(10.5, colDim, "Full "+u.core.Cfg.areaSizeLabel()+" home map — no sightings here yet").Layout(gtx)
		}()
	}
	if !homeView && (u.core.Cfg.HomeLat != 0 || u.core.Cfg.HomeLon != 0) {
		u.drawHomeMark(gtx, proj(u.core.Cfg.HomeLat, u.core.Cfg.HomeLon), w, h)
	}
	// corner labels
	u.label(10.5, colFaint, fmt.Sprintf("%.4f, %.4f", maxLat, minLon)).Layout(
		gtx)
	return layout.Dimensions{Size: sz}
}

// mapAreaStrip is the saved-home-map control row under the map: status,
// the 100x100-mile save button with live progress + cancel, and the
// saved-map-only toggle.
func (u *UI) mapAreaStrip(gtx layout.Context) layout.Dimensions {
	running, done, total, fetched, failed, msg := u.areaProg.Snapshot()
	area, hasArea := u.offlineArea(u.core.Cfg.Library)
	return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
		rows := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.bold(12.5, colText, "Saved home map ("+u.core.Cfg.areaSizeLabel()+")").Layout(gtx)
					}),
					layout.Rigid(hSpace(10)),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						status := "Not saved yet - one download and wardrives near home never need the internet."
						if hasArea {
							status = fmt.Sprintf("Saved %s · %d squares · %s on disk · around %.4f, %.4f. Drives inside it render straight from disk.",
								area.SavedAt, area.Tiles, humanBytes(area.Bytes), area.HomeLat, area.HomeLon)
						}
						return u.label(11.5, colDim, status).Layout(gtx)
					}),
				)
			}),
		}
		if running {
			frac := float32(0)
			if total > 0 {
				frac = float32(done) / float32(total)
			}
			rows = append(rows,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 8) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return material.ProgressBar(u.th, frac).Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return u.label(11.5, colCyan, fmt.Sprintf("Saving… %d of %d squares (%d fetched, %d failed) - already-saved squares are skipped, so this resumes.", done, total, fetched, failed)).Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.btnGhost(&u.btnCancelArea, "Stop").Layout(gtx)
						}),
					)
				}),
			)
		} else {
			rows = append(rows,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 8) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							label := "Save home map (" + u.core.Cfg.areaSizeLabel() + ")"
							if hasArea {
								label = "Re-save / fill gaps"
							}
							return u.btn(&u.btnSaveArea, label).Layout(gtx)
						}),
						layout.Rigid(hSpace(8)),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							t := "Saved map only: OFF"
							if u.savedOnly {
								t = "Saved map only: ON"
							}
							return u.btnGhost(&u.btnSavedOnly, t).Layout(gtx)
						}),
						layout.Rigid(hSpace(10)),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							note := msg
							if u.mapMsg != "" {
								note = u.mapMsg
							}
							if note == "" && !hasArea {
								note = "Home is set in Settings > Wardrive map. Zoom levels 10-14, roughly 150 MB once."
							}
							return u.label(11, colFaint, note).Layout(gtx)
						}),
					)
				}),
			)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
	}))
}
