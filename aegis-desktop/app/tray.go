package main

// System-tray mode (v1.8.1, user request): with the Settings toggle
// on, closing the window tucks the Companion into the system tray
// instead of quitting - the device server, discovery and live links
// (all in Core, independent of the window) keep running. The tray
// icon's right-click menu offers "Open Dashboard" (recreates the
// window) and "Quit Aegis Companion" (the real exit). With the
// toggle off, closing behaves exactly as before: immediate exit.
//
// Implementation: fyne.io/systray (pure Go; DBus StatusNotifier on
// Linux, Shell_NotifyIcon on Windows, so the CGO-free Windows build
// still works). Run lives on its own goroutine; the Gio window loop
// consults tray state on DestroyEvent.

import (
	_ "embed"
	"os"
	"runtime"
	"sync/atomic"

	"fyne.io/systray"
)

//go:embed assets/icon-256.png
var trayIconPNG []byte

//go:embed assets/icon.ico
var trayIconICO []byte

type trayManager struct {
	openCh   chan struct{}
	started  atomic.Bool
	ready    atomic.Bool
	quitting atomic.Bool
}

func newTrayManager() *trayManager {
	return &trayManager{openCh: make(chan struct{}, 1)}
}

func (t *trayManager) start() {
	if !t.started.CompareAndSwap(false, true) {
		return
	}
	go systray.Run(t.onReady, func() {
		t.ready.Store(false)
		t.started.Store(false)
	})
}

func (t *trayManager) stop() {
	if t.started.Load() {
		systray.Quit()
	}
}

// active reports the tray icon really is up - only then may a window
// close hide into the tray instead of exiting (fail-safe: a desktop
// with no working tray keeps the old close=quit behaviour).
func (t *trayManager) active() bool {
	return t != nil && t.ready.Load() && !t.quitting.Load()
}

func (t *trayManager) onReady() {
	if runtime.GOOS == "windows" {
		systray.SetIcon(trayIconICO)
	} else {
		systray.SetIcon(trayIconPNG)
	}
	systray.SetTitle("AEGIS Companion")
	systray.SetTooltip("Aegis Companion - device link running")
	mOpen := systray.AddMenuItem("Open Dashboard", "Bring the Aegis Companion window back")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit Aegis Companion", "Stop the device link and exit completely")
	t.ready.Store(true)
	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				select {
				case t.openCh <- struct{}{}:
				default: // a reopen is already pending
				}
			case <-mQuit.ClickedCh:
				t.quitting.Store(true)
				os.Exit(0) // same exit-now policy as the window close path
			}
		}
	}()
}
