//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func companionBinaryName() string { return "AegisCompanion.exe" }

func defaultInstallDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(base, "AegisCompanion"), nil
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// runPS runs a PowerShell script; used for shortcuts + uninstall
// registration (no admin rights needed: everything is per-user).
func runPS(script string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func installShortcuts(dir string) error {
	exe := filepath.Join(dir, companionBinaryName())
	ico := filepath.Join(dir, "icon.ico")
	startMenu := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs")
	if err := os.MkdirAll(startMenu, 0o755); err != nil {
		return err
	}
	lnk := filepath.Join(startMenu, "AEGIS Companion.lnk")
	script := fmt.Sprintf(`$w = New-Object -ComObject WScript.Shell; $s = $w.CreateShortcut(%s); $s.TargetPath = %s; $s.WorkingDirectory = %s; $s.IconLocation = %s; $s.Description = 'AEGIS family hub - syncs and explains your Cardputer logs'; $s.Save()`,
		psQuote(lnk), psQuote(exe), psQuote(dir), psQuote(ico))
	if err := runPS(script); err != nil {
		return err
	}
	// Per-user uninstall registration (Settings > Apps).
	uninstaller := filepath.Join(dir, "AegisSuite-Uninstaller.exe")
	reg := fmt.Sprintf(`$k = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\AEGISCompanionSuite'; New-Item -Path $k -Force | Out-Null; Set-ItemProperty -Path $k -Name DisplayName -Value 'AEGIS Companion - Family Suite'; Set-ItemProperty -Path $k -Name DisplayVersion -Value %s; Set-ItemProperty -Path $k -Name Publisher -Value 'AEGIS'; Set-ItemProperty -Path $k -Name InstallLocation -Value %s; Set-ItemProperty -Path $k -Name UninstallString -Value %s; Set-ItemProperty -Path $k -Name NoModify -Value 1 -Type DWord; Set-ItemProperty -Path $k -Name NoRepair -Value 1 -Type DWord`,
		psQuote(suiteVersion), psQuote(dir), psQuote(`"`+uninstaller+`" --uninstall`))
	return runPS(reg)
}

func removeShortcuts(dir string) error {
	lnk := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "AEGIS Companion.lnk")
	_ = os.Remove(lnk)
	_ = runPS(`Remove-Item -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\AEGISCompanionSuite' -Recurse -Force -ErrorAction SilentlyContinue`)
	return nil
}

func preserveSelfForUninstall(dir string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, "AegisSuite-Uninstaller.exe"), raw, 0o755)
}

func removeSelfCopy(dir string) {
	// A running Windows exe cannot delete itself; the RemoveAll in
	// uninstall will leave just this file behind and the user is
	// told to delete the leftover folder. Deliberately ignored here.
}

func libraryBackupDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Aegis-Library-Backup"), nil
}

func uninstallTip() string {
	return "Tip: if a stray AegisSuite-Uninstaller.exe remains inside an otherwise-empty\nfolder, delete that folder by hand - a running program cannot delete itself."
}

// killRunningCompanion terminates any running AegisCompanion.exe so
// it cannot hold files open (or re-create them) during removal.
func killRunningCompanion(dir string) {
	_ = exec.Command("taskkill", "/IM", companionBinaryName(), "/F").Run()
	time.Sleep(400 * time.Millisecond)
}
