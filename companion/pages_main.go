package main

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget/material"
)

// ---------------- Dashboard ----------------
// qFlipper-style home: the headline numbers up top, live devices in the
// middle, then recent reports + recent activity side by side.

func (u *UI) pageDashboard(gtx layout.Context) layout.Dimensions {
	files, findings, bad := u.core.Store.TodayStats()
	devs := u.core.Live.Snapshot()
	online := u.core.Live.OnlineCount()

	problemsCol := colCyan
	if bad > 0 {
		problemsCol = colRed
	}
	cards := []struct {
		title string
		value string
		col   color.NRGBA
	}{
		{"DEVICES LIVE NOW", fmt.Sprint(online), colGreen},
		{"FILES SYNCED TODAY", fmt.Sprint(files), colCyan},
		{"FINDINGS TODAY", fmt.Sprint(findings), colCyan},
		{"CHAIN PROBLEMS", fmt.Sprint(bad), problemsCol},
		{"LIBRARY SIZE", humanBytes(u.core.Store.SizeBytes()), colCyan},
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// stat cards row
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			children := make([]layout.FlexChild, 0, len(cards))
			for i, cd := range cards {
				cd := cd
				last := i == len(cards)-1
				children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					wrap := func(gtx layout.Context) layout.Dimensions {
						return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return u.label(10.5, colFaint, cd.title).Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return u.bold(24, cd.col, cd.value).Layout(gtx)
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
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 16) }),
		// hero: the companion crest + what this hub does
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return bgFill(gtx, colPanel, 10, pad(14, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.artBadge(gtx, "COMPANION", 3.4)
					}),
					layout.Rigid(hSpace(14)),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.bold(16, colText, "AEGIS Companion - the family hub").Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 4) }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(12, colDim, "Every AEGIS device talks to this one program: they find it by themselves on your home network, stream what they are doing while they work, and hand over their logs when they get home. The Cardputers stay lean collectors - this PC verifies, files, maps and explains. Nothing phones home.").Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 4) }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11.5, colCyan, fmt.Sprintf("%d device(s) live now · %d file(s) synced today · link health is checked automatically below", online, files)).Layout(gtx)
							}),
						)
					}),
				)
			}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 14) }),
		// the family: each program's own art; click to jump to it
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.bold(13, colText, "The family - click one to open it").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			inLib := map[string]bool{}
			for _, dp := range u.core.Store.Summaries() {
				inLib[dp.Prog] = true
			}
			children := make([]layout.FlexChild, 0, len(familyOrder))
			for _, prog := range familyOrder {
				prog := prog
				children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					click := u.famRows.get(prog)
					if click.Clicked(gtx) {
						u.gotoProgram(prog)
					}
					status, scol := "not seen yet", colFaint
					if _, live := u.core.Live.Get(prog); live {
						status, scol = "LIVE NOW", colGreen
					} else if inLib[prog] {
						status, scol = "in library", colDim
					}
					if prog == "TALON" {
						status, scol = "never linked", colAmber
					}
					if prog == "KEEPER" {
						status, scol = "radio off", colDim
					}
					return layout.Inset{Right: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return material.Clickable(gtx, click, func(gtx layout.Context) layout.Dimensions {
							return bgFill(gtx, colPanel, 8, pad(8, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return u.artBadge(gtx, prog, 1.55)
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 5) }),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return u.bold(10.5, colText, prog).Layout(gtx)
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return u.label(9.5, scol, status).Layout(gtx)
										})
									}),
								)
							}))
						})
					})
				}))
			}
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, children...)
		}),
		// live now
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.bold(14, colText, "Live now").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if len(devs) == 0 {
				return bgFill(gtx, colPanel, 10, pad(14, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.label(13, colText, "No Cardputer is connected right now.").Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.label(12, colDim, "Turn one on at home (PHOENIX, HEARTH or SCRIBE) and it appears here within seconds - scanning state, battery, access points, everything. On home Wi-Fi they find this computer by themselves and sync their logs automatically.").Layout(gtx)
						}),
					)
				}))
			}
			rows := make([]layout.FlexChild, 0, len(devs))
			for _, d := range devs {
				d := d
				rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return u.liveCard(gtx, d, false)
					})
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 14) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.bold(14, colText, "Link health - checked automatically").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			items := u.core.Health()
			return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
				rows := make([]layout.FlexChild, 0, len(items))
				for _, it := range items {
					it := it
					rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						col := colGreen
						if !it.OK {
							col = colRed
						} else if it.Warn {
							col = colAmber
						}
						return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									paint.FillShape(gtx.Ops, col, clip.RRect{
										Rect: image.Rectangle{Max: image.Pt(gtx.Dp(7), gtx.Dp(7))},
										NW:   4, NE: 4, SE: 4, SW: 4}.Op(gtx.Ops))
									return layout.Dimensions{Size: image.Pt(gtx.Dp(7), gtx.Dp(7))}
								}),
								layout.Rigid(hSpace(8)),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return u.bold(12, colText, it.Label).Layout(gtx)
								}),
								layout.Rigid(hSpace(8)),
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return u.label(12, colDim, it.Detail).Layout(gtx)
								}),
							)
						})
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
			}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 14) }),
		// recent reports + activity
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Flexed(1.2, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return u.dashRecentReports(gtx)
					})
				}),
				layout.Flexed(1, u.dashRecentActivity),
			)
		}),
	)
}

func (u *UI) dashRecentReports(gtx layout.Context) layout.Dimensions {
	sums := u.core.Store.Summaries()
	return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.bold(13, colText, "Recent reports").Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		}
		if len(sums) == 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.label(12, colDim, "Nothing filed yet. After the first sync (or an SD-card import in Settings) the plain-English write-ups land here.").Layout(gtx)
			}))
		}
		for i, dp := range sums {
			if i >= 5 {
				break
			}
			dp := dp
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				click := u.repRows.get(dp.Day + "\x00" + dp.Prog)
				if click.Clicked(gtx) {
					u.selReport = dp.Day + "\x00" + dp.Prog
					u.page = pgReports
				}
				return material.Clickable(gtx, click, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: 4, Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return chip(gtx, u, dp.Prog, accentNRGBA(dp.Prog))
							}),
							layout.Rigid(hSpace(8)),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return u.label(12.5, colText, dp.Headline).Layout(gtx)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return u.label(11, colFaint, fmt.Sprintf("%s · %d item(s) · %d chain problem(s)",
											dp.Day, len(dp.Findings), dp.Bad)).Layout(gtx)
									}),
								)
							}),
						)
					})
				})
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}))
}

func (u *UI) dashRecentActivity(gtx layout.Context) layout.Dimensions {
	lines := u.core.Log.Snapshot()
	if len(lines) > 9 {
		lines = lines[len(lines)-9:]
	}
	return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.bold(13, colText, "Recent activity").Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		}
		for _, ln := range lines {
			ln := ln
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.label(11, colDim, trimLine(ln, 64)).Layout(gtx)
			}))
		}
		if len(lines) == 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.label(12, colDim, "Quiet so far.").Layout(gtx)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}))
}

func trimLine(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// ---------------- Live Monitor ----------------
// inSSIDer pattern: a table of what's live, a detail pane for the
// selection, and activity-over-time graphs instead of a static snapshot.

func (u *UI) liveCard(gtx layout.Context, d DeviceLive, detail bool) layout.Dimensions {
	online := u.core.Live.Online(d, time.Now())
	accent := accentNRGBA(d.Prog)
	return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.artBadge(gtx, d.Prog, 2.0)
			}),
			layout.Rigid(hSpace(12)),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return u.liveCardBody(gtx, d, online, accent)
			}),
		)
	}))
}

func (u *UI) liveCardBody(gtx layout.Context, d DeviceLive, online bool, accent color.NRGBA) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return chip(gtx, u, d.Prog, accent)
				}),
				layout.Rigid(hSpace(8)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return stateBadge(gtx, u, d.State, online)
				}),
				layout.Rigid(hSpace(8)),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return u.label(12.5, colText, d.Detail).Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					last := "never"
					if !d.LastSeen.IsZero() {
						last = ago(d.LastSeen)
					}
					return u.label(11, colFaint, "seen "+last).Layout(gtx)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 8) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if d.HasBatt {
						return batteryBar(gtx, u, d.Batt, d.Charging, 90)
					}
					return u.label(11, colFaint, "battery -").Layout(gtx)
				}),
				layout.Rigid(hSpace(16)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					ap := "-"
					if d.HasAPs {
						ap = fmt.Sprint(d.APs)
					}
					return u.label(12, colDim, "APs in view: "+ap).Layout(gtx)
				}),
				layout.Rigid(hSpace(16)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if d.Pct >= 0 && online {
						return u.label(12, colCyan, fmt.Sprintf("progress %d%%", d.Pct)).Layout(gtx)
					}
					return layout.Dimensions{}
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					pts := apHistory(d)
					if len(pts) < 2 {
						return u.label(11, colFaint, "activity graph warming up…").Layout(gtx)
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.label(9.5, colFaint, "APs over time").Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return spark(gtx, 150, 30, pts, accent)
						}),
					)
				}),
			)
		}),
	)
}

func apHistory(d DeviceLive) []float32 {
	var pts []float32
	for _, h := range d.Hist {
		if h.HasAPs {
			pts = append(pts, float32(h.APs))
		}
	}
	return pts
}

func battHistory(d DeviceLive) []float32 {
	var pts []float32
	for _, h := range d.Hist {
		if h.HasBat {
			pts = append(pts, float32(h.Batt))
		}
	}
	return pts
}

func (u *UI) pageLive(gtx layout.Context) layout.Dimensions {
	devs := u.core.Live.Snapshot()
	if len(devs) == 0 {
		return bgFill(gtx, colPanel, 10, pad(16, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.bold(14, colText, "Nothing is streaming right now").Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.label(12.5, colDim, "When a Cardputer running PHOENIX 0.9.3+, HEARTH 0.1.4+ or SCRIBE 0.1.4+ is on your home Wi-Fi with /AEGIS_LINK/desktop.txt on its card, it holds a quiet live link to this program and reports what it is doing every couple of seconds: scanning, watching, alerting, battery, progress. If it is transmitting or busy, you will see it here as it happens. TALON never streams - it is not linked, by design.").Layout(gtx)
				}),
			)
		}))
	}
	// selection default: first online, else first
	if u.selProg == "" {
		u.selProg = devs[0].Prog
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(u.th, &u.liveList).Layout(gtx, len(devs), func(gtx layout.Context, i int) layout.Dimensions {
				d := devs[i]
				click := u.liveRows.get(d.Prog)
				if click.Clicked(gtx) {
					u.selProg = d.Prog
				}
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return material.Clickable(gtx, click, func(gtx layout.Context) layout.Dimensions {
						return u.liveCard(gtx, d, false)
					})
				})
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			sel, ok := u.core.Live.Get(u.selProg)
			if !ok {
				return layout.Dimensions{}
			}
			return u.liveDetail(gtx, sel)
		}),
	)
}

func (u *UI) liveDetail(gtx layout.Context, d DeviceLive) layout.Dimensions {
	online := u.core.Live.Online(d, time.Now())
	return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
		kv := func(k, v string) layout.FlexChild {
			return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Right: 22}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.label(10, colFaint, strings.ToUpper(k)).Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.label(12.5, colText, v).Layout(gtx)
						}),
					)
				})
			})
		}
		ver := d.Ver
		if ver == "" {
			ver = "-"
		}
		lastEvt := d.Last
		if lastEvt == "" {
			lastEvt = "-"
		}
		info := infoFor(d.Prog)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.artBadge(gtx, d.Prog, 2.6)
					}),
					layout.Rigid(hSpace(12)),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.bold(14, colText, info.Title+" - detail").Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11.5, colDim, info.Device+" · "+info.Role).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colFaint, info.Connect).Layout(gtx)
							}),
						)
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					kv("state", d.State),
					kv("firmware", ver),
					kv("events logged", fmt.Sprint(d.Events)),
					kv("last event kind", lastEvt),
					kv("frames received", fmt.Sprint(d.Frames)),
					kv("link", map[bool]string{true: "online", false: "offline (silent >12 s)"}[online]),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 8) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(10, colFaint, "ACCESS POINTS OVER TIME").Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return spark(gtx, 300, 54, apHistory(d), accentNRGBA(d.Prog))
							}),
						)
					}),
					layout.Rigid(hSpace(24)),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(10, colFaint, "BATTERY % OVER TIME").Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return spark(gtx, 300, 54, battHistory(d), colGreen)
							}),
						)
					}),
				)
			}),
		)
	}))
}
