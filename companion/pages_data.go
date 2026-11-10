package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func (u *UI) btn(click *widget.Clickable, txt string) material.ButtonStyle {
	b := material.Button(u.th, click, txt)
	b.Background = colCyan
	b.Color = u.th.Palette.ContrastFg
	b.TextSize = 12.5
	b.Inset = layout.Inset{Top: 7, Bottom: 7, Left: 12, Right: 12}
	b.CornerRadius = 8
	return b
}

func (u *UI) btnGhost(click *widget.Clickable, txt string) material.ButtonStyle {
	b := u.btn(click, txt)
	b.Background = colPanel2
	b.Color = colText
	return b
}

func (u *UI) field(ed *widget.Editor, hint string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return bgFill(gtx, colPanel2, 8, pad(8, func(gtx layout.Context) layout.Dimensions {
			e := material.Editor(u.th, ed, hint)
			e.Color = colText
			e.HintColor = colFaint
			e.TextSize = 13
			return e.Layout(gtx)
		}))
	}
}

func sectionTitle(u *UI, txt string) layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 14, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return u.bold(13.5, colText, txt).Layout(gtx)
		})
	})
}

func caption(u *UI, txt string) layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return u.label(11.5, colDim, txt).Layout(gtx)
	})
}

// ---------------- Library ----------------

func (u *UI) pageLibrary(gtx layout.Context) layout.Dimensions {
	sums := u.core.Store.Summaries()
	// unique days in order
	var days []string
	seen := map[string]bool{}
	progsByDay := map[string][]DayProg{}
	for _, dp := range sums {
		progsByDay[dp.Day] = append(progsByDay[dp.Day], dp)
		if !seen[dp.Day] {
			seen[dp.Day] = true
			days = append(days, dp.Day)
		}
	}
	if len(days) == 0 {
		return bgFill(gtx, colPanel, 10, pad(16, func(gtx layout.Context) layout.Dimensions {
			return u.label(12.5, colDim, "The library is empty. Sync from a Cardputer, or import an SD card in Settings, and dated folders appear here: Library/<date>/<program>/raw/ with a plain-English report.html beside the raw files.").Layout(gtx)
		}))
	}
	if u.libDay == "" || !seen[u.libDay] {
		u.libDay = days[0]
	}
	progs := progsByDay[u.libDay]
	if u.libProg == "" {
		// Default to the program with the most to show (usually the hub).
		best := progs[0]
		for _, p := range progs {
			if len(p.Findings) > len(best.Findings) {
				best = p
			}
		}
		u.libProg = best.Prog
	}
	var sel *DayProg
	for i := range progs {
		if progs[i].Prog == u.libProg {
			sel = &progs[i]
		}
	}
	if sel == nil {
		sel = &progs[0]
		u.libProg = sel.Prog
	}

	colList := func(title string, items []string, selItem string, set *clickSet, onPick func(string)) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.bold(12, colDim, title).Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					rows := make([]layout.FlexChild, 0, len(items))
					for _, it := range items {
						it := it
						rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							click := set.get(it)
							if click.Clicked(gtx) {
								onPick(it)
							}
							return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return material.Clickable(gtx, click, func(gtx layout.Context) layout.Dimensions {
									bg := colPanel
									c := colDim
									if it == selItem {
										bg = colPanel2
										c = colText
									}
									return bgFill(gtx, bg, 8, pad(8, func(gtx layout.Context) layout.Dimensions {
										if title == "PROGRAM" {
											return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return u.artBadge(gtx, it, 0.85)
												}),
												layout.Rigid(hSpace(8)),
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return u.label(12.5, c, it).Layout(gtx)
												}),
											)
										}
										return u.label(12.5, c, it).Layout(gtx)
									}))
								})
							})
						}))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
				}),
			)
		}
	}

	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(150)
			gtx.Constraints.Max.X = gtx.Dp(150)
			return colList("DATE", days, u.libDay, u.libDays, func(s string) { u.libDay = s; u.libProg = "" })(gtx)
		}),
		layout.Rigid(hSpace(14)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(170)
			gtx.Constraints.Max.X = gtx.Dp(170)
			names := make([]string, 0, len(progs))
			for _, p := range progs {
				names = append(names, p.Prog)
			}
			return colList("PROGRAM", names, u.libProg, u.libProgs, func(s string) { u.libProg = s })(gtx)
		}),
		layout.Rigid(hSpace(14)),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return u.libraryFiles(gtx, sel)
		}),
	)
}

func (u *UI) libraryFiles(gtx layout.Context, dp *DayProg) layout.Dimensions {
	rawdir := filepath.Join(dp.Dir, "raw")
	if u.btnOpenRaw.Clicked(gtx) {
		openPath(rawdir)
	}
	if u.btnOpenIndex.Clicked(gtx) {
		openPath(u.core.libraryIndexURL())
	}
	if u.btnRebuild.Clicked(gtx) {
		u.core.Store.Rebuild()
		u.core.Log.Infof("reports rebuilt by hand")
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.label(12, colDim, dp.Day+" · "+dp.Headline).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.label(11, colFaint, infoFor(dp.Prog).Title+" sends: "+infoFor(dp.Prog).Sends).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.artBadge(gtx, dp.Prog, 1.6)
				}),
				layout.Rigid(hSpace(10)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return chip(gtx, u, dp.Prog, accentNRGBA(dp.Prog))
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnRebuild, "Rebuild reports").Layout(gtx)
				}),
				layout.Rigid(hSpace(8)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnOpenIndex, "Open index in browser").Layout(gtx)
				}),
				layout.Rigid(hSpace(8)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btn(&u.btnOpenRaw, "Open raw folder").Layout(gtx)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 10) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// table header
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return u.label(10.5, colFaint, "FILE (untouched, as received)").Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.label(10.5, colFaint, "SIZE").Layout(gtx)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 4) }),
		layout.Rigid(hDivider),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(u.th, &u.libList).Layout(gtx, len(dp.Files), func(gtx layout.Context, i int) layout.Dimensions {
				f := dp.Files[i]
				return layout.Inset{Top: 5, Bottom: 5}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return u.label(12.5, colText, f.Name).Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.label(12, colDim, humanBytes(f.Size)).Layout(gtx)
						}),
					)
				})
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.label(11, colFaint, "Stored at: "+rawdir).Layout(gtx)
		}),
	)
}

// ---------------- Reports ----------------

func (u *UI) pageReports(gtx layout.Context) layout.Dimensions {
	sums := u.core.Store.Summaries()
	if len(sums) == 0 {
		return bgFill(gtx, colPanel, 10, pad(16, func(gtx layout.Context) layout.Dimensions {
			return u.label(12.5, colDim, "No reports yet. They are written automatically after every sync and every SD-card import - plain English first, with the raw record folded underneath each finding.").Layout(gtx)
		}))
	}
	if u.selReport == "" {
		best := sums[0]
		for _, s := range sums {
			if len(s.Findings) > len(best.Findings) {
				best = s
			}
		}
		u.selReport = best.Day + "\x00" + best.Prog
	}
	var sel *DayProg
	for i := range sums {
		if sums[i].Day+"\x00"+sums[i].Prog == u.selReport {
			sel = &sums[i]
		}
	}
	if sel == nil {
		sel = &sums[0]
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(330)
			gtx.Constraints.Max.X = gtx.Dp(330)
			return material.List(u.th, &u.repList).Layout(gtx, len(sums), func(gtx layout.Context, i int) layout.Dimensions {
				dp := sums[i]
				key := dp.Day + "\x00" + dp.Prog
				click := u.repRows.get(key)
				if click.Clicked(gtx) {
					u.selReport = key
				}
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return material.Clickable(gtx, click, func(gtx layout.Context) layout.Dimensions {
						bg := colPanel
						if key == u.selReport {
							bg = colPanel2
						}
						return bgFill(gtx, bg, 10, pad(10, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return u.artBadge(gtx, dp.Prog, 1.3)
										}),
										layout.Rigid(hSpace(8)),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return chip(gtx, u, dp.Prog, accentNRGBA(dp.Prog))
										}),
										layout.Rigid(hSpace(8)),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return u.label(11.5, colDim, dp.Day).Layout(gtx)
										}),
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if dp.Bad > 0 {
												return u.label(11, colRed, fmt.Sprintf("⚠ %d chain bad", dp.Bad)).Layout(gtx)
											}
											return u.label(11, colGreen, "chain ok").Layout(gtx)
										}),
									)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 4) }),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return u.label(12.5, colText, dp.Headline).Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return u.label(11, colFaint, fmt.Sprintf("%d item(s)", len(dp.Findings))).Layout(gtx)
								}),
							)
						}))
					})
				})
			})
		}),
		layout.Rigid(hSpace(14)),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return u.reportDetail(gtx, sel)
		}),
	)
}

func (u *UI) reportDetail(gtx layout.Context, dp *DayProg) layout.Dimensions {
	if u.btnOpenHTML.Clicked(gtx) {
		openPath("file://" + filepath.Join(dp.Dir, "report.html"))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return u.bold(15, colText, dp.Prog+" — "+dp.Day).Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btn(&u.btnOpenHTML, "Open full HTML report").Layout(gtx)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 4) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.label(12.5, colCyan, dp.Headline).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 8) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(u.th, &u.findList).Layout(gtx, len(dp.Findings), func(gtx layout.Context, i int) layout.Dimensions {
				f := dp.Findings[i]
				key := fmt.Sprintf("%s|%s|%d", dp.Day, dp.Prog, i)
				toggle := u.repExpand.get(key)
				if toggle.Clicked(gtx) {
					u.expanded[key] = !u.expanded[key]
				}
				sevCol := sevColorN(f.Sev)
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return bgFill(gtx, colPanel, 10, pad(10, func(gtx layout.Context) layout.Dimensions {
						children := []layout.FlexChild{
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return u.label(11, sevCol, "●").Layout(gtx)
									}),
									layout.Rigid(hSpace(6)),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return u.bold(13, colText, f.Simple).Layout(gtx)
									}),
								)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(12, colDim, f.Why).Layout(gtx)
							}),
						}
						if f.Detail != "" {
							children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(12, colCyan, f.Detail).Layout(gtx)
							}))
						}
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return material.Clickable(gtx, toggle, func(gtx layout.Context) layout.Dimensions {
								arrow := "▸"
								if u.expanded[key] {
									arrow = "▾"
								}
								return u.label(11.5, colFaint, arrow+" technical details (file: "+f.Source+", kind: "+f.Kind+")").Layout(gtx)
							})
						}))
						if u.expanded[key] {
							children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return u.label(10.5, colDim, f.Raw).Layout(gtx)
								})
							}))
						}
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
					}))
				})
			})
		}),
	)
}

func sevColorN(sev int) color.NRGBA {
	switch sev {
	case 3:
		return color.NRGBA{0xFF, 0x52, 0x52, 0xFF}
	case 2:
		return color.NRGBA{0xFF, 0xB0, 0x20, 0xFF}
	case 1:
		return color.NRGBA{0x8A, 0x94, 0xA8, 0xFF}
	}
	return color.NRGBA{0x3A, 0x43, 0x58, 0xFF}
}

// ---------------- Activity Log ----------------

func (u *UI) pageLog(gtx layout.Context) layout.Dimensions {
	if u.btnLogFolder.Clicked(gtx) {
		openPath(filepath.Join(exeDir(), "logs"))
	}
	lines := u.core.Log.Snapshot()
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					p := u.core.Log.Path()
					return u.label(12, colDim, "Session log file: "+p).Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnLogFolder, "Open logs folder").Layout(gtx)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 8) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return bgFill(gtx, colPanel, 10, pad(10, func(gtx layout.Context) layout.Dimensions {
				return material.List(u.th, &u.logList).Layout(gtx, len(lines), func(gtx layout.Context, i int) layout.Dimensions {
					ln := lines[i]
					c := colDim
					if strings.Contains(ln, " ERROR ") {
						c = colRed
					} else if strings.Contains(ln, " WARN ") {
						c = colAmber
					} else if strings.Contains(ln, "live:") || strings.Contains(ln, "received ") {
						c = colText
					}
					return u.label(11.5, c, ln).Layout(gtx)
				})
			}))
		}),
	)
}

// ---------------- Settings ----------------

func (u *UI) pageSettings(gtx layout.Context) layout.Dimensions {
	cfg := u.core.Cfg

	if u.btnApplyPort.Clicked(gtx) {
		p, err := strconv.Atoi(strings.TrimSpace(u.portEd.Text()))
		switch {
		case err != nil || p < 1 || p > 65535:
			u.settingsMsg = "Port must be a number between 1 and 65535."
		default:
			if err := u.core.RestartListener(p); err != nil {
				u.settingsMsg = "Could not listen on :" + fmt.Sprint(p) + " - " + err.Error()
			} else {
				cfg.Port = p
				_ = cfg.Save()
				u.settingsMsg = fmt.Sprintf("Listening on :%d now. Re-write AEGIS_LINK to the SD cards so they use the new port.", p)
				u.core.Log.Infof("listener port changed to %d from Settings", p)
			}
		}
	}
	if u.btnRegenTok.Clicked(gtx) {
		cfg.Token = newToken()
		_ = cfg.Save()
		u.settingsMsg = "New token generated. Devices will be REJECTED until you re-write AEGIS_LINK/desktop.txt to their SD cards (button below)."
		u.core.Log.Warnf("sync token regenerated from Settings - SD cards need the new desktop.txt")
	}
	if u.btnWriteSD.Clicked(gtx) {
		sd := strings.TrimSpace(u.sdEd.Text())
		host := strings.TrimSpace(u.hostEd.Text())
		switch {
		case sd == "":
			u.settingsMsg = "Type the SD card folder first (for example E:\\ on Windows or /media/you/CARD on Linux)."
		case host == "":
			u.settingsMsg = "Type this computer's home-network address first."
		default:
			dir := filepath.Join(sd, "AEGIS_LINK")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				u.settingsMsg = "Could not create " + dir + ": " + err.Error()
			} else if err := os.WriteFile(filepath.Join(dir, "desktop.txt"), []byte(cfg.desktopTxt(host)), 0o644); err != nil {
				u.settingsMsg = "Could not write desktop.txt: " + err.Error()
			} else {
				u.settingsMsg = "Wrote " + filepath.Join(dir, "desktop.txt") + " - put the card back in the Cardputer and it can sync + stream live."
				u.core.Log.Infof("AEGIS_LINK/desktop.txt written to %s (host %s)", dir, host)
			}
		}
	}
	if u.btnImportSD.Clicked(gtx) {
		sd := strings.TrimSpace(u.sdEd.Text())
		if sd == "" {
			u.settingsMsg = "Type the SD card folder first."
		} else if n, err := u.core.Store.ImportFrom(sd); err != nil {
			u.settingsMsg = "Import failed: " + err.Error()
		} else {
			u.settingsMsg = fmt.Sprintf("Imported %d file(s) from the card and rebuilt the reports - see Library / Reports.", n)
			u.core.Log.Infof("SD import from %s: %d file(s)", sd, n)
		}
	}
	if u.btnShortcut.Clicked(gtx) {
		if msg, err := installShortcut(); err != nil {
			u.settingsMsg = "Shortcut failed: " + err.Error()
		} else {
			u.settingsMsg = msg
		}
	}
	if u.btnCheckUpdate.Clicked(gtx) {
		latest, path, err := findUpdate()
		switch {
		case err != nil:
			u.updateFound, u.updatePath = "", ""
			u.settingsMsg = "No update staged. Updates live in the Updates folder next to the program (" + updatesDir() + "): a newer suite installer plus a small latest.json. When the family repo exists, that folder is what it will feed - nothing is downloaded silently."
			u.core.Log.Infof("update check: nothing staged (%v)", err)
		case !versionNewer(latest.Version, appVersion):
			u.updateFound, u.updatePath = "", ""
			u.settingsMsg = fmt.Sprintf("You are up to date - this is v%s, the staged update is v%s.", appVersion, latest.Version)
		default:
			u.updatePath = path
			u.updateFound = fmt.Sprintf("Update v%s is staged and ready", latest.Version)
			msg := u.updateFound + ". Use 'Install update' and the suite installer will upgrade everything in place - your Library is kept."
			if latest.Notes != "" {
				msg += " Notes: " + latest.Notes
			}
			if path == "" {
				msg += " (latest.json names no installer file, so there is nothing to run yet.)"
				u.updatePath = ""
			}
			u.settingsMsg = msg
			u.core.Log.Infof("update check: v%s staged at %s", latest.Version, path)
		}
	}
	if u.btnInstallUpdate.Clicked(gtx) {
		if u.updatePath == "" {
			u.settingsMsg = "Check for updates first - no staged installer to run."
		} else if err := launchInstaller(u.updatePath); err != nil {
			u.settingsMsg = "Could not start the update installer: " + err.Error()
		} else {
			u.core.Log.Infof("update installer launched: %s - closing so it can replace files", u.updatePath)
			os.Exit(0) // the installer upgrades in place; get out of its way
		}
	}
	if u.btnUninstall.Clicked(gtx) {
		switch {
		case !u.uninstallArm:
			u.uninstallArm = true
			u.settingsMsg = "Really remove AEGIS Companion? Click 'Uninstall' once more to confirm. Your Library (logs + saved maps) is MOVED to an Aegis-Library-Backup folder, never deleted. Devices keep working standalone; they just stop syncing here."
		default:
			u.uninstallArm = false
			if suiteUninstallerPath() != "" {
				if err := launchSuiteUninstaller(); err != nil {
					u.settingsMsg = "Could not start the uninstaller: " + err.Error()
				} else {
					u.core.Log.Warnf("uninstaller launched from Settings - closing")
					os.Exit(0)
				}
			} else {
				// Portable copy: no suite uninstaller staged. Remove
				// the menu entry here and give exact manual steps.
				_ = removeShortcut()
				u.settingsMsg = "This copy was not installed by the suite installer, so there is no staged uninstaller. I removed the app-menu/Start shortcut. To finish: close this program and delete its folder (" + exeDir() + "). Your Library inside it is yours to keep or delete - nothing else was installed anywhere."
			}
		}
	}
	if u.btnOpenCfg.Clicked(gtx) {
		openPath(exeDir())
	}
	if u.btnOpenLib.Clicked(gtx) {
		openPath(u.core.Cfg.Library)
	}
	if u.btnClearTiles.Clicked(gtx) {
		if err := ClearTileCache(u.core.Cfg.Library); err != nil {
			u.settingsMsg = "Could not clear the map tile cache: " + err.Error()
		} else {
			_ = DeleteOfflineArea(u.core.Cfg.Library) // the saved area lived in that cache
			u.savedOnly = false
			u.tiles, u.tilesBusy = nil, ""
			u.settingsMsg = "Map tile cache cleared - the real map will re-download its squares next time it opens (schematic works meanwhile)."
			u.core.Log.Infof("map tile cache cleared from Settings")
		}
	}
	if u.btnSaveHome.Clicked(gtx) {
		lat, err1 := strconv.ParseFloat(strings.TrimSpace(u.homeLatEd.Text()), 64)
		lon, err2 := strconv.ParseFloat(strings.TrimSpace(u.homeLonEd.Text()), 64)
		switch {
		case err1 != nil || err2 != nil || lat < -85 || lat > 85 || lon < -180 || lon > 180:
			u.settingsMsg = "Home location needs a latitude (-85 to 85) and a longitude (-180 to 180) - for example 41.5247 and -72.0759."
		default:
			cfg.HomeLat, cfg.HomeLon = lat, lon
			if err := cfg.Save(); err != nil {
				u.settingsMsg = "Could not save the home location: " + err.Error()
			} else {
				u.settingsMsg = fmt.Sprintf("Home set to %.5f, %.5f. Now save the 100x100-mile offline map around it (button below, or on the Wardrive Map page).", lat, lon)
				u.core.Log.Infof("home location set to %.5f, %.5f", lat, lon)
			}
		}
	}
	if u.btnSaveArea.Clicked(gtx) {
		if cfg.HomeLat == 0 && cfg.HomeLon == 0 {
			u.settingsMsg = "Set the home location above first - the saved map is drawn around it."
		} else {
			u.areaProg.Start(cfg.Library, cfg.HomeLat, cfg.HomeLon, cfg.Token, func() {
				u.tiles, u.tilesBusy = nil, ""
			})
			u.settingsMsg = "Saving the home map in the background - progress is on the Wardrive Map page and below. You can keep working; it resumes if stopped."
		}
	}
	if u.btnExportPack.Clicked(gtx) {
		dest := filepath.Join(cfg.Library, "maps", "packs",
			fmt.Sprintf("AEGIS-MAP-%s.aegismap", time.Now().Format("2006-01-02")))
		if n, err := ExportMapPack(cfg.Library, dest, cfg.Token); err != nil {
			u.settingsMsg = "Map pack export failed: " + err.Error()
		} else if n == 0 {
			u.settingsMsg = "Nothing to export yet - the map cache is empty. View a wardrive map or save the home map first."
		} else {
			u.settingsMsg = fmt.Sprintf("Map library pack written: %s (%d squares). Copy that one file anywhere - importing it needs no internet.", dest, n)
			u.core.Log.Infof("map pack exported: %s (%d tiles)", dest, n)
		}
	}
	if u.btnImportPack.Clicked(gtx) {
		packs := ListMapPacks(cfg.Library)
		if len(packs) == 0 {
			u.settingsMsg = "No .aegismap packs in Library/maps/packs - export one here, or copy a pack file into that folder first."
		} else {
			total, skipped, verified := 0, 0, 0
			for _, p := range packs {
				n, sk, state, err := ImportMapPack(cfg.Library, p, cfg.Token)
				if err != nil {
					u.settingsMsg = "Import failed on " + filepath.Base(p) + ": " + err.Error()
					break
				}
				total, skipped = total+n, skipped+sk
				if state == PackVerified {
					verified++
				}
				markPackImported(cfg.Library, p, "manual import", state == PackVerified)
			}
			if u.settingsMsg == "" || total > 0 {
				sigNote := "None of the packs were signed - fine if you made them yourself."
				if verified == len(packs) {
					sigNote = "Signature verified on every pack - made by a companion holding your token."
				} else if verified > 0 {
					sigNote = fmt.Sprintf("%d of %d pack(s) signature-verified; the rest were unsigned.", verified, len(packs))
				}
				u.settingsMsg = fmt.Sprintf("Imported %d map squares from %d pack(s) (%d entries skipped) - no internet used. %s Saved-map mode can use them now.", total, len(packs), skipped, sigNote)
				u.tiles, u.tilesBusy = nil, ""
				u.tileBytesAt = time.Time{}
				u.areaAt = time.Time{}
				u.core.Log.Infof("map packs imported: %d tiles (%d verified)", total, verified)
			}
		}
	}
	if u.btnDeleteArea.Clicked(gtx) {
		if err := DeleteOfflineArea(cfg.Library); err != nil {
			u.settingsMsg = "Could not forget the saved home map: " + err.Error()
		} else {
			u.savedOnly = false
			u.tiles, u.tilesBusy = nil, ""
			u.settingsMsg = "Saved home map forgotten. The downloaded squares stay in the cache (clear them above if you want the disk space back)."
		}
	}

	tokenShown := "••••••••••••••••••••••••" + cfg.Token[max(0, len(cfg.Token)-4):]
	if u.showToken.Value {
		tokenShown = cfg.Token
	}
	ips := localLANIPs()
	ipNote := "No private LAN address found - type this computer's home-network IP manually."
	if len(ips) > 0 {
		ipNote = "This computer's address(es): " + strings.Join(ips, ", ") + " - the first is filled in. Devices must be on the same home network."
	}

	form := []layout.FlexChild{
		sectionTitle(u, "Listener"),
		caption(u, "Cardputers connect here to sync logs and stream live status. Default 8791 - must match desktop.txt on the cards."),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(110)
					gtx.Constraints.Max.X = gtx.Dp(110)
					return u.field(&u.portEd, "port")(gtx)
				}),
				layout.Rigid(hSpace(10)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btn(&u.btnApplyPort, "Apply + restart listener").Layout(gtx)
				}),
			)
		}),
		sectionTitle(u, "Put AEGIS_LINK on a card (do this once per card)"),
		caption(u, "Insert a Cardputer SD card, type its folder below, then write the link file. One file works for SCRIBE, PHOENIX and HEARTH cards."),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.label(12, colDim, "This computer:").Layout(gtx)
				}),
				layout.Rigid(hSpace(8)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(170)
					gtx.Constraints.Max.X = gtx.Dp(170)
					return u.field(&u.hostEd, "192.168.x.x")(gtx)
				}),
				layout.Rigid(hSpace(12)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.label(12, colDim, "SD card folder:").Layout(gtx)
				}),
				layout.Rigid(hSpace(8)),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return u.field(&u.sdEd, `E:\  or  /media/you/CARD`)(gtx)
				}),
			)
		}),
		caption(u, ipNote),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btn(&u.btnWriteSD, "Write AEGIS_LINK to SD card").Layout(gtx)
				}),
				layout.Rigid(hSpace(10)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnImportSD, "Import logs from this SD card instead").Layout(gtx)
				}),
			)
		}),
		sectionTitle(u, "Secret token"),
		caption(u, "Every device proves it knows this token before anything is accepted. Keep it private; it is stored in aegis-companion.json next to the program."),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return bgFill(gtx, colPanel2, 8, pad(8, func(gtx layout.Context) layout.Dimensions {
						return u.label(12.5, colText, tokenShown).Layout(gtx)
					}))
				}),
				layout.Rigid(hSpace(10)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return material.Switch(u.th, &u.showToken, "Show").Layout(gtx)
				}),
				layout.Rigid(hSpace(6)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.label(12, colDim, "Show").Layout(gtx)
				}),
				layout.Rigid(hSpace(12)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnRegenTok, "Regenerate").Layout(gtx)
				}),
			)
		}),
		sectionTitle(u, "Program"),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btn(&u.btnShortcut, "Install Start Menu / app-menu shortcut").Layout(gtx)
				}),
				layout.Rigid(hSpace(10)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnOpenCfg, "Open program folder").Layout(gtx)
				}),
				layout.Rigid(hSpace(10)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnOpenLib, "Open Library folder").Layout(gtx)
				}),
			)
		}),
		caption(u, "The shortcut points at this very program file - keep the folder where it is afterwards. Library: "+cfg.Library),
		sectionTitle(u, "Updates & removal"),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btn(&u.btnCheckUpdate, "Check for updates").Layout(gtx)
				}),
				layout.Rigid(hSpace(10)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnInstallUpdate, "Install update").Layout(gtx)
				}),
				layout.Rigid(hSpace(10)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnUninstall, "Uninstall AEGIS Companion...").Layout(gtx)
				}),
			)
		}),
		caption(u, "Updates are checked in the Updates folder beside the program - a newer suite installer + latest.json (this is where the future repo will plug in; nothing downloads silently). Uninstall uses the suite uninstaller: the program and shortcut go, your Library is moved to Aegis-Library-Backup, never deleted."),
		sectionTitle(u, "The family - who connects to this hub, and how"),
		caption(u, "Each program has its own job and its own artwork. Linked ones find this PC themselves; TALON and KEEPER never link, by design."),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			info := infoFor("PHOENIX")
			return bgFill(gtx, colPanel, 8, pad(10, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.artBadge(gtx, "PHOENIX", 1.9)
					}),
					layout.Rigid(hSpace(12)),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.bold(12.5, colText, info.Title+" · "+info.Device).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11.5, colDim, info.Role).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colCyan, "Connects: "+info.Connect).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colFaint, "Sends here: "+info.Sends).Layout(gtx)
							}),
						)
					}),
				)
			}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			info := infoFor("SCRIBE")
			return bgFill(gtx, colPanel, 8, pad(10, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.artBadge(gtx, "SCRIBE", 1.9)
					}),
					layout.Rigid(hSpace(12)),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.bold(12.5, colText, info.Title+" · "+info.Device).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11.5, colDim, info.Role).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colCyan, "Connects: "+info.Connect).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colFaint, "Sends here: "+info.Sends).Layout(gtx)
							}),
						)
					}),
				)
			}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			info := infoFor("HEARTH")
			return bgFill(gtx, colPanel, 8, pad(10, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.artBadge(gtx, "HEARTH", 1.9)
					}),
					layout.Rigid(hSpace(12)),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.bold(12.5, colText, info.Title+" · "+info.Device).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11.5, colDim, info.Role).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colCyan, "Connects: "+info.Connect).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colFaint, "Sends here: "+info.Sends).Layout(gtx)
							}),
						)
					}),
				)
			}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			info := infoFor("HERALD")
			return bgFill(gtx, colPanel, 8, pad(10, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.artBadge(gtx, "HERALD", 1.9)
					}),
					layout.Rigid(hSpace(12)),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.bold(12.5, colText, info.Title+" · "+info.Device).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11.5, colDim, info.Role).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colCyan, "Connects: "+info.Connect).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colFaint, "Sends here: "+info.Sends).Layout(gtx)
							}),
						)
					}),
				)
			}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			info := infoFor("BENCH")
			return bgFill(gtx, colPanel, 8, pad(10, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.artBadge(gtx, "BENCH", 1.9)
					}),
					layout.Rigid(hSpace(12)),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.bold(12.5, colText, info.Title+" · "+info.Device).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11.5, colDim, info.Role).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colCyan, "Connects: "+info.Connect).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colFaint, "Sends here: "+info.Sends).Layout(gtx)
							}),
						)
					}),
				)
			}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			info := infoFor("LANTERN")
			return bgFill(gtx, colPanel, 8, pad(10, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.artBadge(gtx, "LANTERN", 1.9)
					}),
					layout.Rigid(hSpace(12)),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.bold(12.5, colText, info.Title+" · "+info.Device).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11.5, colDim, info.Role).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colCyan, "Connects: "+info.Connect).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colFaint, "Sends here: "+info.Sends).Layout(gtx)
							}),
						)
					}),
				)
			}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			info := infoFor("KEEPER")
			return bgFill(gtx, colPanel, 8, pad(10, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.artBadge(gtx, "KEEPER", 1.9)
					}),
					layout.Rigid(hSpace(12)),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.bold(12.5, colText, info.Title+" · "+info.Device).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11.5, colDim, info.Role).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colCyan, "Connects: "+info.Connect).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colFaint, "Sends here: "+info.Sends).Layout(gtx)
							}),
						)
					}),
				)
			}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			info := infoFor("TALON")
			return bgFill(gtx, colPanel, 8, pad(10, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.artBadge(gtx, "TALON", 1.9)
					}),
					layout.Rigid(hSpace(12)),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.bold(12.5, colText, info.Title+" · "+info.Device).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11.5, colDim, info.Role).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colCyan, "Connects: "+info.Connect).Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.label(11, colFaint, "Sends here: "+info.Sends).Layout(gtx)
							}),
						)
					}),
				)
			}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		sectionTitle(u, "Wardrive map"),
		caption(u, "The Wardrive Map page shows real street maps by default (OpenStreetMap tiles, downloaded once and cached in the Library - currently "+humanBytes(u.tileBytes(cfg.Library))+"). The schematic plot is the offline fallback. Clearing the cache only costs a re-download."),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.btnGhost(&u.btnClearTiles, "Clear map tile cache").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		caption(u, "Saved home map: set your home location, then save a 100x100-mile square of street map around it (zoom levels 10-14, one polite download, roughly 150 MB). Wardrives inside that square then draw their real street map straight from disk - no internet, no web call - and your drives stay easy to browse offline. Saving resumes if stopped, and re-running fills any gaps."),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.label(12, colDim, "Home latitude:").Layout(gtx)
				}),
				layout.Rigid(hSpace(8)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(120)
					gtx.Constraints.Max.X = gtx.Dp(120)
					return u.field(&u.homeLatEd, "41.5247")(gtx)
				}),
				layout.Rigid(hSpace(12)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.label(12, colDim, "longitude:").Layout(gtx)
				}),
				layout.Rigid(hSpace(8)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(120)
					gtx.Constraints.Max.X = gtx.Dp(120)
					return u.field(&u.homeLonEd, "-72.0759")(gtx)
				}),
				layout.Rigid(hSpace(12)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btn(&u.btnSaveHome, "Save home location").Layout(gtx)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			running, done, total, _, _, _ := u.areaProg.Snapshot()
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btn(&u.btnSaveArea, "Save 100x100-mile home map").Layout(gtx)
				}),
				layout.Rigid(hSpace(8)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnDeleteArea, "Forget saved home map").Layout(gtx)
				}),
				layout.Rigid(hSpace(10)),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					status := "No home map saved yet."
					if area, ok := u.offlineArea(cfg.Library); ok {
						status = fmt.Sprintf("Home map saved %s: %d squares, %s on disk, zooms %d-%d around %.4f, %.4f.",
							area.SavedAt, area.Tiles, humanBytes(area.Bytes), area.MinZoom, area.MaxZoom, area.HomeLat, area.HomeLon)
					} else if cfg.HomeLat != 0 || cfg.HomeLon != 0 {
						n, _ := AreaTilePlan(cfg.HomeLat, cfg.HomeLon)
						status = fmt.Sprintf("Home is set. Saving will fetch about %d squares once (about %s).", n, humanBytes(int64(n)*15000))
					}
					if running {
						status = fmt.Sprintf("Saving home map… %d of %d squares done.", done, total)
					}
					return u.label(11.5, colDim, status).Layout(gtx)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		caption(u, "Standard map library: export your saved squares as ONE portable .aegismap pack (Library/maps/packs). Import it on any computer running this companion - a fresh install with a pack never needs the internet for maps at all."),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			packs := ListMapPacks(cfg.Library)
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btn(&u.btnExportPack, "Export map library pack").Layout(gtx)
				}),
				layout.Rigid(hSpace(8)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.btnGhost(&u.btnImportPack, "Import pack(s) from library folder").Layout(gtx)
				}),
				layout.Rigid(hSpace(10)),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return u.label(11.5, colDim, fmt.Sprintf("%d pack(s) in the library folder.", len(packs))).Layout(gtx)
				}),
			)
		}),
		sectionTitle(u, "About"),
		caption(u, "AEGIS Companion "+appVersion+" · protocol AEGIS/1 · Cardputers stay lean collectors; this program verifies, files and explains. TALON is never linked, by design. Nothing here phones home."),
	}
	if u.settingsMsg != "" {
		form = append(form,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 12) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return bgFill(gtx, colPanel2, 8, pad(10, func(gtx layout.Context) layout.Dimensions {
					return u.label(12.5, colCyan, u.settingsMsg).Layout(gtx)
				}))
			}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(u.th, &u.setList).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, form...)
			})
		}),
	)
}

var _ = unit.Dp(0)
