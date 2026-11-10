//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// selfUninstall is the built-in fallback when no suite uninstaller
// was staged beside the program (older installs): a detached
// PowerShell script waits for this process to exit, moves the
// Library to the usual backup folder, removes the install dir,
// the Start Menu shortcut and the uninstall registry entry. The
// caller os.Exit(0)s right after a nil return.
func selfUninstall() error {
	dir := exeDir()
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	backup := filepath.Join(home, "Aegis-Library-Backup")
	script := filepath.Join(os.TempDir(), fmt.Sprintf("aegis-uninstall-%d.ps1", os.Getpid()))
	body := "Start-Sleep -Seconds 2\n" +
		"if (Test-Path '" + filepath.Join(dir, "Library") + "') { try { Move-Item '" + filepath.Join(dir, "Library") + "' '" + backup + "' -ErrorAction Stop } catch { Move-Item '" + filepath.Join(dir, "Library") + "' ('" + backup + "-' + (Get-Date -Format yyyyMMdd-HHmmss)) } }\n" +
		"Remove-Item '" + dir + "' -Recurse -Force -ErrorAction SilentlyContinue\n" +
		"Remove-Item (Join-Path $env:APPDATA 'Microsoft\\Windows\\Start Menu\\Programs\\AEGIS Companion.lnk') -Force -ErrorAction SilentlyContinue\n" +
		"Remove-Item 'HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\AEGISCompanionSuite' -Recurse -Force -ErrorAction SilentlyContinue\n" +
		"Remove-Item '" + script + "' -Force -ErrorAction SilentlyContinue\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		return err
	}
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", script)
	return cmd.Start()
}
