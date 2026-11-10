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
	// Updates panel buttons (v1.8.2): poll here, act before layout.
	if u.btnFwCheck.Clicked(gtx) {
		startUpdateCheck(u.core.Cfg)
		startFwCheck(u.core.Cfg, u.core.Log)
		u.toastGood("Checking: Companion feed + PHOENIX firmware feed...", 4)
	}
	if u.btnFwFetch.Clicked(gtx) {
		startFwDownload(u.core.Cfg, u.core.Log)
		u.toastGood("Downloading the latest PHOENIX here (SHA-256 verified) - then Send update on the device card.", 6)
	}
	if u.btnSyncAll.Clicked(gtx) {
		u.syncEverything()
	}
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

	children := []layout.FlexChild{
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
			return u.bold(14, colText, "Home presence - this PC's Wi-Fi radio").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			pv := presenceSnapshot()
			return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
				rows := []layout.FlexChild{}
				if u.core.Cfg.HomeSSID == "" {
					rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(12, colDim, "Off for now: type your home Wi-Fi name in Settings (Presence section) and this PC starts sensing. Your Cardputers already appear under who's-home.").Layout(gtx)
					}))
				} else {
					state, scol := "HOME QUIET", colGreen
					if pv.Motion {
						state, scol = "MOTION AT HOME", colAmber
					}
					if !pv.Ready {
						state, scol = "LEARNING THE QUIET...", colCyan
					}
					rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return stateBadge(gtx, u, state, true)
							}),
							layout.Rigid(hSpace(10)),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								txt := fmt.Sprintf("signal ripple %.0f (quiet baseline %.0f) - %d access points in earshot", pv.Score, pv.Calm, pv.APs)
								if pv.LastErr != "" {
									txt = "scan problem: " + pv.LastErr
								}
								return u.label(12, colDim, txt).Layout(gtx)
							}),
							layout.Rigid(hSpace(12)),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								pts := make([]float32, len(pv.Points))
								for i, pt := range pv.Points {
									pts[i] = float32(pt.Score)
								}
								if len(pts) >= 2 {
									return spark(gtx, 150, 26, pts, scol)
								}
								return layout.Dimensions{}
							}),
						)
					}))
					rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }))
				}
				if len(pv.People) > 0 {
					rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(12, colDim, "Who's home:").Layout(gtx)
					}))
					for _, person := range pv.People {
						person := person
						rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							st, c := "away", colFaint
							if person.Home {
								st, c = "home ("+person.Via+")", colGreen
							}
							return u.label(12, c, "  "+person.Name+" - "+st).Layout(gtx)
						}))
					}
				} else if u.core.Cfg.HomeSSID != "" {
					rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(12, colDim, "Who's home: name your phone/TV in Settings (Name=MAC lines); your Cardputers show up by themselves.").Layout(gtx)
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
			}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 14) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.bold(14, colText, "Updates - Companion + devices in sync").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			uv := updateSnapshot()
			fw := fwSnapshot()
			compLine := "Companion " + appVersion + " (this PC)"
			switch {
			case uv.haveRemote:
				compLine += " -> " + uv.latest.Version + " available"
			case uv.checked:
				compLine += " - up to date"
			default:
				compLine += " - not checked yet"
			}
			if uv.busy {
				compLine += " (" + uv.msg + ")"
			}
			fwLine := "PHOENIX firmware: "
			switch {
			case fw.FeedVer != "" && fw.StagedVer == fw.FeedVer:
				fwLine += fw.FeedVer + " downloaded here, verified, ready to offer"
			case fw.FeedVer != "":
				fwLine += "latest " + fw.FeedVer + " on the feed"
				if fw.StagedVer != "" {
					fwLine += " (this PC has " + fw.StagedVer + " ready)"
				}
			case fw.StagedVer != "":
				fwLine += fw.StagedVer + " downloaded here"
			default:
				fwLine += "not checked yet"
			}
			if fw.Msg != "" {
				fwLine += " - " + fw.Msg
			}
			if fw.Busy && fw.Pct >= 0 {
				fwLine += fmt.Sprintf(" (%d%%)", fw.Pct)
			}
			return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(12.5, colText, compLine).Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 4) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(12.5, colText, fwLine).Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 8) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.btn(&u.btnFwCheck, "Check for updates").Layout(gtx)
							}),
							layout.Rigid(hSpace(10)),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.btnGhost(&u.btnFwFetch, "Get latest PHOENIX").Layout(gtx)
							}),
							layout.Rigid(hSpace(10)),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.btn(&u.btnSyncAll, "Update everything").Layout(gtx)
							}),
						)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(11.5, colDim, "Update everything: this PC downloads and SHA-256-verifies the newest PHOENIX, then offers it to each live Cardputer - it only flashes after ENTER is pressed on the device itself. The Companion's own update still finishes in Settings (it restarts the app).").Layout(gtx)
					}),
				)
			}))
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
		// found on the network (active ping, v1.7.9)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.bold(13, colText, "On your network right now (this PC pings every 15 s)").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			found := u.core.Disco.Found()
			if len(found) == 0 {
				if n, ip, _ := u.core.Disco.Rejects(); n > 0 {
					return u.label(11.5, colAmber, fmt.Sprintf("Heard %d announcement(s) from %s but the security proof did not match - the token on that SD card differs from this PC's. Fix: Settings > Connection > re-write AEGIS_LINK to the card (or regenerate the token there). This notice is why nothing is listed.", n, ip)).Layout(gtx)
				}
				return u.label(11.5, colDim, "None yet. A Cardputer appears here when it is on your Wi-Fi with the SD link file inserted (PHOENIX 0.12.2+ names itself by answering the ping; the others appear when they come looking for this PC). Finding is not linking - you still approve on the device.").Layout(gtx)
			}
			rows := make([]layout.FlexChild, 0, len(found))
			for _, fd := range found {
				fd := fd
				rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					name, detail, col := fd.Prog, "", colCyan
					if name == "" {
						name = "AEGIS device"
						detail = fd.IP + " - it is looking for this computer; watch its screen"
					} else if _, live := u.core.Live.Get(fd.Prog); live {
						detail = fd.IP + " - LIVE NOW"
						col = colGreen
					} else {
						detail = fd.IP + " - fw " + fd.Ver + " - on the device, press ENTER to approve the link"
					}
					age := time.Since(fd.LastSeen).Round(time.Second)
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if fd.Prog == "" {
								return u.artBadge(gtx, "COMPANION", 1.1) // unnamed yet: neutral tile
							}
							return u.artBadge(gtx, fd.Prog, 1.1)
						}),
						layout.Rigid(hSpace(10)),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.bold(12, colText, name).Layout(gtx)
						}),
						layout.Rigid(hSpace(8)),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return u.label(11.5, col, detail+" · seen "+age.String()+" ago").Layout(gtx)
						}),
					)
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 14) }),
		// recent reports + activity
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Flexed(1.2, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return u.dashRecentReports(gtx)
					})
				}),
				layout.Flexed(1, u.dashRecentActivity),
			)
		}),
	}
	const chunk = 2
	nChunks := (len(children) + chunk - 1) / chunk
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return withScrollBar(gtx, &u.dashList, func(gtx layout.Context) layout.Dimensions {
				return material.List(u.th, &u.dashList).Layout(gtx, nChunks, func(gtx layout.Context, i int) layout.Dimensions {
					lo := i * chunk
					hi := lo + chunk
					if hi > len(children) {
						hi = len(children)
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children[lo:hi]...)
				})
			})
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
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			fw := fwSnapshot()
			fwTxt := "firmware " + d.Ver
			if d.Ver == "" {
				fwTxt = "firmware ?"
			}
			if fw.StagedVer != "" {
				fwTxt += "  ·  this PC has " + fw.StagedVer + " verified"
			}
			canOffer := d.Prog == "PHOENIX" && fw.StagedVer != "" && d.Ver != "" && fwNewer(fw.StagedVer, d.Ver)
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.label(11.5, colDim, fwTxt).Layout(gtx)
				}),
				layout.Rigid(hSpace(10)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if canOffer && online {
						click := u.fwRows.get("fwupd:" + d.Prog)
						if click.Clicked(gtx) {
							u.offerFirmware(d.Prog)
						}
						return u.btn(click, "Send update").Layout(gtx)
					}
					if canOffer {
						return u.label(11, colAmber, "newer firmware ready - device offline").Layout(gtx)
					}
					if d.Prog == "PHOENIX" && fw.StagedVer != "" && d.Ver != "" && !fwNewer(fw.StagedVer, d.Ver) {
						return u.label(11, colGreen, "firmware current").Layout(gtx)
					}
					return layout.Dimensions{}
				}),
			)
		}),
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
					if d.HasGPSAcc {
						txt := fmt.Sprintf("GPS ±%d m", d.GPSAcc)
						if d.HasGPSRej && d.GPSRej > 0 {
							txt += fmt.Sprintf(" · %d jump(s) rejected", d.GPSRej)
						}
						return u.label(12, colGreen, txt).Layout(gtx)
					}
					return layout.Dimensions{}
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
			return withScrollBar(gtx, &u.liveList, func(gtx layout.Context) layout.Dimensions {
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
					kv("GPS estimate", func() string {
						if !d.HasGPSAcc {
							return "not reported"
						}
						txt := fmt.Sprintf("±%d m", d.GPSAcc)
						if d.HasGPSRej {
							txt += fmt.Sprintf(" · %d jump(s) rejected", d.GPSRej)
						}
						return txt
					}()),
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
