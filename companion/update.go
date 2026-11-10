package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Program updates + removal (v1.7.0).
//
// UPDATE: the "repo later" hook. The updater looks in an Updates
// folder next to the program for a small latest.json:
//
//	{"version": "1.7.0", "file": "Aegis-Suite-Setup-Linux", "notes": "..."}
//
// plus the named installer file beside it. When the family gets its
// public repo, the same latest.json (and file) can be fetched from
// there instead - the version comparison, the "only ever run an
// installer from this folder" rule and the hand-off stay identical.
// Until then, dropping a newer suite installer + latest.json into
// Updates/ is the whole mechanism: no silent downloads, ever.
//
// UNINSTALL: suite-installed copies carry AegisSuite-Uninstaller
// next to the program; the Settings button launches it and quits.
// It moves the Library to Aegis-Library-Backup instead of deleting
// it (see the suite installer). Portable copies get shortcut
// removal + clear instructions instead of a fake uninstall.

type updateLatest struct {
	Version string `json:"version"`
	File    string `json:"file"`
	Notes   string `json:"notes"`
}

func updatesDir() string { return filepath.Join(exeDir(), "Updates") }

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// versionNewer reports whether a > b in x.y.z terms. Unparseable
// input is never "newer" - a hand-edited latest.json must not be
// able to push the user into running something odd.
func versionNewer(a, b string) bool {
	va, okA := parseVersion(a)
	vb, okB := parseVersion(b)
	if !okA || !okB {
		return false
	}
	for i := 0; i < 3; i++ {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

// findUpdate looks for a staged update in Updates/. Returns the
// parsed manifest and the installer path ("" file = info only).
func findUpdate() (updateLatest, string, error) {
	dir := updatesDir()
	raw, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		return updateLatest{}, "", err
	}
	var latest updateLatest
	if err := json.Unmarshal(raw, &latest); err != nil {
		return updateLatest{}, "", fmt.Errorf("latest.json is not readable: %w", err)
	}
	if _, ok := parseVersion(latest.Version); !ok {
		return updateLatest{}, "", fmt.Errorf("latest.json version %q is not x.y.z", latest.Version)
	}
	if latest.File == "" {
		return latest, "", nil
	}
	// The installer must be a plain file name inside Updates/ -
	// never a path that escapes the folder.
	if filepath.Base(latest.File) != latest.File {
		return updateLatest{}, "", fmt.Errorf("latest.json file %q must be a plain file name", latest.File)
	}
	p := filepath.Join(dir, latest.File)
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return latest, "", fmt.Errorf("update file %s not found in Updates", latest.File)
	}
	return latest, p, nil
}

// suiteUninstallerPath is the uninstaller copy the suite installer
// stages next to the program ("" when this copy was not installed
// by the suite).
func suiteUninstallerPath() string {
	name := "AegisSuite-Uninstaller"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(exeDir(), name)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

func launchInstaller(path string) error {
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	return cmd.Start()
}

func launchSuiteUninstaller() error {
	p := suiteUninstallerPath()
	if p == "" {
		return fmt.Errorf("no suite uninstaller beside this program")
	}
	cmd := exec.Command(p, "--uninstall")
	cmd.Dir = filepath.Dir(p)
	return cmd.Start()
}
