//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// selfUninstall is the built-in fallback when no suite uninstaller
// was staged beside the program (older installs): a detached shell
// script waits for this process to exit, moves the Library to the
// usual backup folder, removes the install dir and the app-menu
// entry. The caller os.Exit(0)s right after a nil return.
func selfUninstall() error {
	dir := exeDir()
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	backup := filepath.Join(home, "Aegis-Library-Backup")
	script := filepath.Join(os.TempDir(), fmt.Sprintf("aegis-uninstall-%d.sh", os.Getpid()))
	body := "#!/bin/sh\n" +
		"sleep 2\n" +
		"if [ -d '" + dir + "/Library' ]; then mv '" + dir + "/Library' '" + backup + "' 2>/dev/null || mv '" + dir + "/Library' '" + backup + "-'\"$(date +%Y%m%d-%H%M%S)\"; fi\n" +
		"rm -rf '" + dir + "'\n" +
		"rm -f '" + filepath.Join(home, ".local", "share", "applications", "aegis-companion.desktop") + "'\n" +
		"rm -f '" + script + "'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("sh", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
