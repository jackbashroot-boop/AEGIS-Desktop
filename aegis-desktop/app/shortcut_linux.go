//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// installShortcut writes a freedesktop app-menu entry pointing at the
// running executable - the Linux equivalent of a Start Menu shortcut.
func installShortcut() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	icon := filepath.Join(exeDir(), "icon.png")
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=AEGIS Companion
GenericName=Cardputer dashboard and sync
Comment=Downloads, organises and explains AEGIS Cardputer logs; live device dashboard
Exec="%s"
Icon=%s
Terminal=false
Categories=Utility;Network;
StartupNotify=true
`, strings.ReplaceAll(exe, `"`, `\"`), icon)
	path := filepath.Join(dir, "aegis-companion.desktop")
	if err := os.WriteFile(path, []byte(entry), 0o755); err != nil {
		return "", err
	}
	return "App-menu entry installed: " + path + " (look for 'AEGIS Companion' in your applications menu; you can pin it from there)", nil
}

// removeShortcut deletes the app-menu entry installShortcut wrote.
func removeShortcut() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	p := filepath.Join(home, ".local", "share", "applications", "aegis-companion.desktop")
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	link := filepath.Join(home, ".local", "bin", "aegis-companion")
	_ = os.Remove(link)
	return nil
}
