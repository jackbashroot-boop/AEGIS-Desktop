//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func companionBinaryName() string { return "AegisCompanion" }

func defaultInstallDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "AegisCompanion"), nil
}

func desktopFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "applications", "aegis-companion.desktop"), nil
}

func installShortcuts(dir string) error {
	bin := filepath.Join(dir, companionBinaryName())
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=AEGIS Companion
GenericName=AEGIS family hub
Comment=Syncs, maps and explains your AEGIS Cardputer logs
Exec=%s
Icon=%s
Terminal=false
Categories=Utility;
StartupNotify=true
`, bin, filepath.Join(dir, "icon.png"))
	df, err := desktopFilePath()
	if err != nil {
		return err
	}
	if err := writeFile(df, []byte(entry), 0o755); err != nil {
		return err
	}
	// Terminal convenience + menu refresh (best effort).
	home, _ := os.UserHomeDir()
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err == nil {
		link := filepath.Join(binDir, "aegis-companion")
		_ = os.Remove(link)
		_ = os.Symlink(bin, link)
	}
	if p, err := exec.LookPath("update-desktop-database"); err == nil {
		_ = exec.Command(p, filepath.Dir(df)).Run()
	}
	return nil
}

func removeShortcuts(dir string) error {
	if df, err := desktopFilePath(); err == nil {
		_ = os.Remove(df)
	}
	home, _ := os.UserHomeDir()
	_ = os.Remove(filepath.Join(home, ".local", "bin", "aegis-companion"))
	return nil
}

// The installer copy kept in the install dir so the OS uninstall
// entry / a future --uninstall run works even if the downloaded
// file is deleted.
func preserveSelfForUninstall(dir string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, "AegisSuite-Uninstaller"), raw, 0o755)
}

func removeSelfCopy(dir string) {
	_ = os.Remove(filepath.Join(dir, "AegisSuite-Uninstaller"))
}

func libraryBackupDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Aegis-Library-Backup"), nil
}

func uninstallTip() string { return "" }

// killRunningCompanion terminates any AegisCompanion process whose
// executable lives in the install dir (graceful TERM, removal
// retries in uninstall() handle stragglers).
func killRunningCompanion(dir string) {
	target := filepath.Join(dir, companionBinaryName())
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	killed := false
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err == nil && exe == target {
			if proc, err := os.FindProcess(pid); err == nil {
				_ = proc.Signal(syscall.SIGTERM)
				killed = true
			}
		}
	}
	if killed {
		fmt.Println("Stopped a running AEGIS Companion copy so it cannot re-create files.")
		time.Sleep(500 * time.Millisecond)
	}
}
