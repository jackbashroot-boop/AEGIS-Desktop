//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// installShortcut creates a Start Menu shortcut via the Windows shell
// (WScript.Shell COM), driven by PowerShell - no third-party code, and no
// console window flashes because the child process is hidden.
func installShortcut() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exe)
	icon := filepath.Join(dir, "icon.ico")
	script := fmt.Sprintf(
		`$s=(New-Object -ComObject WScript.Shell).CreateShortcut([IO.Path]::Combine($env:APPDATA,'Microsoft\Windows\Start Menu\Programs','AEGIS Companion.lnk'));`+
			`$s.TargetPath='%s';$s.WorkingDirectory='%s';$s.IconLocation='%s,0';`+
			`$s.Description='AEGIS Companion - Cardputer dashboard and sync';$s.Save()`,
		psQuote(exe), psQuote(dir), psQuote(icon))
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("PowerShell shortcut failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return "Start Menu shortcut installed - press the Windows key and type 'AEGIS Companion'. You can right-click it there to pin it to Start or the taskbar.", nil
}

func psQuote(s string) string {
	return strings.ReplaceAll(s, `'`, `''`)
}

// removeShortcut deletes the Start Menu shortcut installShortcut made.
func removeShortcut() error {
	lnk := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "AEGIS Companion.lnk")
	if err := os.Remove(lnk); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
