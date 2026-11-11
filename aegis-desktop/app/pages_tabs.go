package main

// Dashboard tabs (v1.8.8): the old Dashboard + Live Monitor merged into
// one page - an Overview tab plus one tab per program. Clicking a
// program's art anywhere opens its tab: live detail while it streams,
// library history and plain-English status when it does not. The
// sidebar also carries a permanent "Live now" box above the listening
// box so connection state is glanceable from every page.

import (
	"fmt"
	"image"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget/material"
)

// tabStrip is the horizontal Overview/program tab bar.
func (u *UI) tabStrip(gtx layout.Context) layout.Dimensions {
	type tab struct{ prog, label string }
	tabs := []tab{{"", "Overview"}}
	for _, p := range familyOrder {
		tabs = append(tabs, tab{p, p})
	}
	return material.List(u.th, &u.tabList).Layout(gtx, len(tabs), func(gtx layout.Context, i int) layout.Dimensions {
		t := tabs[i]
		key := t.label
		click := u.tabRows.get("tab:" + key)
		if click.Clicked(gtx) {
			u.dashTab = t.prog
			if t.prog != "" {
				u.selProg = t.prog
			}
		}
		active := u.dashTab == t.prog
		bg, fg := colPanel, colDim
		if active {
			bg, fg = colPanel2, colCyan
		}
		return layout.Inset{Right: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return material.Clickable(gtx, click, func(gtx layout.Context) layout.Dimensions {
				return bgFill(gtx, bg, 8, pad(8, func(gtx layout.Context) layout.Dimensions {
					if t.prog == "" {
						return u.bold(12, fg, t.label).Layout(gtx)
					}
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.artBadge(gtx, t.prog, 0.85)
						}),
						layout.Rigid(hSpace(6)),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							live := ""
							if _, ok := u.core.Live.Get(t.prog); ok {
								live = " ●"
							}
							return u.bold(12, fg, t.label+live).Layout(gtx)
						}),
					)
				}))
			})
		})
	})
}

// pageProgram is one program's tab.
func (u *UI) pageProgram(gtx layout.Context, prog string) layout.Dimensions {
	if u.btnTabLib.Clicked(gtx) {
		for _, dp := range u.core.Store.Summaries() {
			if strings.EqualFold(dp.Prog, prog) {
				u.libDay, u.libProg = dp.Day, dp.Prog
				u.page = pgLibrary
				break
			}
		}
	}
	info := progInfo[prog]
	d, live := u.core.Live.Get(prog)
	var hist []DayProg
	for _, dp := range u.core.Store.Summaries() {
		if strings.EqualFold(dp.Prog, prog) {
			hist = append(hist, dp)
		}
	}
	status, scol := "not seen yet", colFaint
	switch {
	case live:
		status, scol = "LIVE NOW", colGreen
	case prog == "TALON":
		status, scol = "never linked, by design", colAmber
	case prog == "KEEPER":
		status, scol = "radio off, by design", colDim
	case len(hist) > 0:
		status, scol = "in library - not live right now", colDim
	}

	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return bgFill(gtx, colPanel, 10, pad(14, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.artBadge(gtx, prog, 2.8)
					}),
					layout.Rigid(hSpace(14)),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.bold(17, colText, info.Title).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 3) }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(12, colCyan, info.Device).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 3) }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(12, colDim, info.Role).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 3) }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11.5, colFaint, info.Connect+"  ·  Sends: "+info.Sends).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 5) }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.bold(12, scol, status).Layout(gtx)
							}),
						)
					}),
				)
			}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 12) }),
	}
	if live {
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.bold(14, colText, "Live right now").Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.liveCard(gtx, d, false)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 8) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.liveDetail(gtx, d)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 14) }),
		)
	} else if prog == "TALON" {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
				return u.label(12, colDim, "TALON is the clean-room red-team build. It is never linked, never installed by the suite and never streams - by design. Its art lives here so the family is complete; there is nothing to connect.").Layout(gtx)
			}))
		}), layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 14) }))
	} else {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
				return u.label(12, colDim, "Not streaming right now. Turn it on at home and this tab fills with live state within seconds - scanning, battery, access points, progress. Below is what it has already handed to the library.").Layout(gtx)
			}))
		}), layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 14) }))
	}
	// library history for this program
	children = append(children,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.bold(14, colText, "In the library").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if len(hist) == 0 {
				return u.label(12, colDim, "Nothing filed yet. When this device syncs, its days appear here and on the Library page.").Layout(gtx)
			}
			rows := make([]layout.FlexChild, 0, len(hist)+1)
			for i, dp := range hist {
				if i >= 8 {
					break
				}
				dp := dp
				rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return u.label(12, colText, fmt.Sprintf("%s · %d file(s) · %d finding(s) · %s", dp.Day, len(dp.Files), len(dp.Findings), dp.Headline)).Layout(gtx)
					})
				}))
			}
			rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.btnGhost(&u.btnTabLib, "Open in Library").Layout(gtx)
			}))
			return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
			}))
		}),
	)
	return withScrollBar(gtx, &u.dashList, func(gtx layout.Context) layout.Dimensions {
		return material.List(u.th, &u.dashList).Layout(gtx, 1, func(gtx layout.Context, i int) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// liveNowBox is the permanent sidebar status box above the listening
// box: green dot when anything is linked, red when nothing is.
func (u *UI) liveNowBox(gtx layout.Context) layout.Dimensions {
	if u.tabRows.get("livenow").Clicked(gtx) {
		u.page = pgDash
		u.dashTab = ""
	}
	devs := u.core.Live.Snapshot()
	now := time.Now()
	var on []DeviceLive
	for _, d := range devs {
		if u.core.Live.Online(d, now) {
			on = append(on, d)
		}
	}
	dot := colRed
	if len(on) > 0 {
		dot = colGreen
	}
	return material.Clickable(gtx, u.tabRows.get("livenow"), func(gtx layout.Context) layout.Dimensions {
		return bgFill(gtx, colPanel, 10, pad(10, func(gtx layout.Context) layout.Dimensions {
			rows := []layout.FlexChild{
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							sz := gtx.Dp(9)
							paint.FillShape(gtx.Ops, dot, clip.Ellipse{Min: image.Pt(0, 0), Max: image.Pt(sz, sz)}.Op(gtx.Ops))
							return layout.Dimensions{Size: image.Pt(sz, sz)}
						}),
						layout.Rigid(hSpace(8)),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.bold(12, colText, "Live now").Layout(gtx)
						}),
						layout.Rigid(hSpace(6)),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.label(11, colDim, fmt.Sprintf("%d linked", len(on))).Layout(gtx)
						}),
					)
				}),
			}
			if len(on) == 0 {
				rows = append(rows,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 4) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(11, colDim, "Nothing linked right now - devices appear here by themselves at home.").Layout(gtx)
					}),
				)
			}
			for i, d := range on {
				if i >= 3 {
					break
				}
				d := d
				rows = append(rows,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 3) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						txt := d.Prog + " · " + d.State
						if d.HasBatt {
							txt += fmt.Sprintf(" · %d%%", d.Batt)
						}
						return u.label(11, colGreen, txt).Layout(gtx)
					}),
				)
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
		}))
	})
}
