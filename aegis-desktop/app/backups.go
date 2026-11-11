package main

// Failover copies (v1.8.9): settings are the one thing that hurts to
// lose in a power cut, so (1) every config save is atomic
// (write-temp-then-rename) and drops a rotating backup beside the
// program, and (2) the System page can take a one-click backup zip
// (config + network census + system summary) into Library/Backups.
// The library itself is append-only logs - the devices still hold
// their SD originals, which is the second copy by design.

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// backupConfigCopy keeps the newest 5 settings snapshots next to the
// program, at most one per 10 minutes (saves are frequent).
func backupConfigCopy() {
	dir := filepath.Join(exeDir(), "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	ents, _ := filepath.Glob(filepath.Join(dir, "aegis-config-*.json"))
	sort.Strings(ents)
	if len(ents) > 0 {
		if st, err := os.Stat(ents[len(ents)-1]); err == nil && time.Since(st.ModTime()) < 10*time.Minute {
			return
		}
	}
	data, err := os.ReadFile(configPath())
	if err != nil {
		return
	}
	name := "aegis-config-" + time.Now().Format("20060102-150405") + ".json"
	_ = os.WriteFile(filepath.Join(dir, name), data, 0o600)
	ents, _ = filepath.Glob(filepath.Join(dir, "aegis-config-*.json"))
	sort.Strings(ents)
	for len(ents) > 5 {
		_ = os.Remove(ents[0])
		ents = ents[1:]
	}
}

// lastBackupInfo describes the newest backup zip + config snapshots.
func lastBackupInfo(libRoot string) string {
	var parts []string
	if ents, _ := filepath.Glob(filepath.Join(libRoot, "Backups", "aegis-backup-*.zip")); len(ents) > 0 {
		sort.Strings(ents)
		if st, err := os.Stat(ents[len(ents)-1]); err == nil {
			parts = append(parts, "backup zip "+st.ModTime().Format("2 Jan 15:04"))
		}
	}
	if ents, _ := filepath.Glob(filepath.Join(exeDir(), "backups", "aegis-config-*.json")); len(ents) > 0 {
		parts = append(parts, fmt.Sprintf("%d settings snapshot(s) beside the program", len(ents)))
	}
	if len(parts) == 0 {
		return "no backups yet - settings snapshots start with your next saved change"
	}
	return strings.Join(parts, " · ")
}

// makeBackupZip writes Library/Backups/aegis-backup-<ts>.zip.
func makeBackupZip(core *Core) (string, error) {
	dir := filepath.Join(core.Store.Root(), "Backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "aegis-backup-"+time.Now().Format("20060102-150405")+".zip")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	z := zip.NewWriter(f)
	add := func(name string, data []byte) {
		w, err := z.Create(name)
		if err == nil {
			_, _ = w.Write(data)
		}
	}
	if data, err := os.ReadFile(configPath()); err == nil {
		add("aegis-companion.json", data)
	}
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(core.Store.Root()), "netcensus.json")); err == nil {
		add("netcensus.json", data)
	}
	s := sysSnapshotNow()
	summary := fmt.Sprintf("AEGIS Companion %s backup %s\nPC: %s (%s)\nCPU: %s x%d, load %.0f%%\nRAM: %s / %s\nGPU: %s\nTemps: %s\n",
		appVersion, time.Now().Format(time.RFC3339), s.Host, s.OS, s.CPUModel, s.Cores, s.CPUPct,
		humanBytes(int64(s.RAMUsed)), humanBytes(int64(s.RAMTotal)), s.GPU, strings.Join(s.Temps, "; "))
	add("system.txt", []byte(summary))
	if err := z.Close(); err != nil {
		return "", err
	}
	return path, nil
}
