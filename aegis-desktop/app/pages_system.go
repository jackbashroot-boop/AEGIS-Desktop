package main

// System page (v1.8.9): one concise place for (a) live info about the
// PC this Companion runs on, (b) backups/failover status with a
// one-click backup, and (c) the repo/recovery commands we keep
// re-typing by hand - each with a Copy button.

import (
	"fmt"
	"image"
	"io"
	"strings"

	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/widget/material"
)

type cmdEntry struct{ what, cmd string }

var repoCommands = []cmdEntry{
	{"Get the code (first time)", "git clone https://github.com/jackbashroot-boop/AEGIS-Desktop.git ~/aegis-clean"},
	{"Update your local copy", "cd ~/aegis-clean && git pull"},
	{"Send updated sources up", "cd ~/aegis-clean && git add -A && git commit -m \"Suite sources\" && git push"},
	{"Publish a release (files beside you)", "gh release create vX.Y.Z Aegis-Suite-Setup-Linux Aegis-Suite-Setup-Windows.exe aegis-phoenix-adv.bin latest.json phoenix-latest.json --title \"AEGIS Suite vX.Y.Z\""},
	{"List published releases", "gh release list -R jackbashroot-boop/AEGIS-Desktop"},
	{"Find this round's files in Downloads", "find ~/Downloads -name \"*1.2.19*\""},
	{"Unzip a release bundle", "unzip -o Aegis-Release-v1.2.19-FILES.zip -d release-1.2.19"},
	{"Check the installed Companion", "./AegisCompanion --version && ./AegisCompanion --selftest"},
}

func usageBar(gtx layout.Context, u *UI, label string, frac float32, text string) layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		w := gtx.Constraints.Max.X
		if w <= 0 {
			w = gtx.Dp(520)
		}
		h := gtx.Dp(10)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.label(11.5, colDim, label+": "+text).Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 3) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				fillRect(gtx, image.Rect(0, 0, w, h), colPanel2)
				fw := int(float32(w) * frac)
				if fw > w {
					fw = w
				}
				if fw > 0 {
					fillRect(gtx, image.Rect(0, 0, fw, h), colCyan)
				}
				return layout.Dimensions{Size: image.Pt(w, h)}
			}),
		)
	})
}

func (u *UI) pageSystem(gtx layout.Context) layout.Dimensions {
	if u.btnBackupNow.Clicked(gtx) {
		if path, err := makeBackupZip(u.core); err != nil {
			u.toast("⚠ Backup failed: "+err.Error(), 9)
		} else {
			u.toastGood("✓ Backup saved: "+path, 7)
			u.core.Log.Infof("manual backup zip written: %s", path)
		}
	}
	for i, c := range repoCommands {
		if u.tabRows.get(fmt.Sprintf("cmd:%d", i)).Clicked(gtx) {
			gtx.Execute(clipboard.WriteCmd{Type: "text/plain", Data: io.NopCloser(strings.NewReader(c.cmd))})
			u.toastGood("Copied: "+c.what, 3)
		}
	}
	s := sysSnapshotNow()
	row := func(k, v string) layout.FlexChild {
		return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Dp(110)
						return u.label(11.5, colFaint, strings.ToUpper(k)).Layout(gtx)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return u.label(12, colText, v).Layout(gtx)
					}),
				)
			})
		})
	}
	cpuTxt := "measuring..."
	if s.CPUPct >= 0 {
		cpuTxt = fmt.Sprintf("%.0f%%", s.CPUPct)
	}
	ramTxt, ramFrac := "-", float32(0)
	if s.RAMTotal > 0 {
		ramTxt = humanBytes(int64(s.RAMUsed)) + " of " + humanBytes(int64(s.RAMTotal))
		ramFrac = float32(s.RAMUsed) / float32(s.RAMTotal)
	}
	cpuFrac := float32(0)
	if s.CPUPct > 0 {
		cpuFrac = float32(s.CPUPct) / 100
	}
	temps := "no temperature sensors exposed"
	if len(s.Temps) > 0 {
		temps = strings.Join(s.Temps, "   ·   ")
	}
	disk := "-"
	if s.DiskTotal > 0 {
		disk = humanBytes(int64(s.DiskFree)) + " free of " + humanBytes(int64(s.DiskTotal)) + " (" + s.DiskPath + ")"
	}

	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.bold(14, colText, "This PC").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					row("Computer", s.Host+" · "+s.OS),
					row("Companion", "AEGIS Companion "+appVersion+" (this program)"),
					row("CPU", fmt.Sprintf("%s · %d threads", s.CPUModel, s.Cores)),
					row("GPU", orDash(s.GPU)),
					row("Disk", disk),
					row("Uptime", orDash(s.Uptime)),
					row("Library", u.core.Store.Root()),
				)
			}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 14) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.bold(14, colText, "Live usage").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					usageBar(gtx, u, "CPU", cpuFrac, cpuTxt),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 8) }),
					usageBar(gtx, u, "Memory", ramFrac, ramTxt),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 8) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(11.5, colDim, "Temperatures: "+temps).Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(10.5, colFaint, "Refreshed every few seconds · sampled "+s.At.Format("15:04:05")).Layout(gtx)
					}),
				)
			}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 14) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.bold(14, colText, "Backups & failover").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(12, colText, lastBackupInfo(u.core.Store.Root())).Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 5) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(11.5, colDim, "Settings save atomically (a power cut mid-save can't corrupt them) and keep 5 rotating snapshots beside the program. The backup zip holds your settings, the network census and a system summary, in Library/Backups. Your logs' second copy is the SD card in each device - leave cards in and a dead PC loses nothing.").Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 8) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.btn(&u.btnBackupNow, "Back up now").Layout(gtx)
					}),
				)
			}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 14) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.bold(14, colText, "Repo & recovery commands").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 4) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.label(11.5, colDim, "The commands we use every round, kept here so nobody has to dig through chat. Copy, paste into fish or bash, adjust the version. Chat paste mangles filenames - typing still wins for the long ones.").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			rows := make([]layout.FlexChild, 0, len(repoCommands))
			for i, c := range repoCommands {
				i, c := i, c
				rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return bgFill(gtx, colPanel, 8, pad(9, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return u.bold(11.5, colText, c.what).Layout(gtx)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return u.label(11, colCyan, c.cmd).Layout(gtx)
										}),
									)
								}),
								layout.Rigid(hSpace(10)),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return u.btnGhost(u.tabRows.get(fmt.Sprintf("cmd:%d", i)), "Copy").Layout(gtx)
								}),
							)
						}))
					})
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
		}),
	}
	return withScrollBar(gtx, &u.sysList, func(gtx layout.Context) layout.Dimensions {
		return material.List(u.th, &u.sysList).Layout(gtx, 1, func(gtx layout.Context, i int) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
