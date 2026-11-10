// AEGIS Family Suite installer - one self-installing file per OS.
//
// Double-click (Windows) or run (Linux): it verifies its built-in
// payload, installs the AEGIS Companion for the current user (no
// admin needed), adds a Start Menu / app-menu shortcut, and lays the
// whole device-program suite (PHOENIX, SCRIBE, HEARTH, HERALD,
// KEEPER, BENCH, LANTERN) beside it under Suite/, ready to copy to
// SD cards. TALON is deliberately NOT part of the suite: it is never
// bundled, linked or installed by anything.
//
// Re-running upgrades in place and never touches the Companion's
// Library/ (synced logs, saved maps). --uninstall removes the
// program + shortcut and moves any Library to a clearly named
// backup folder first rather than deleting your data.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	suiteVersion     = "1.2.11"
	companionVersion = "1.8.2"
	productName      = "AEGIS Companion"
)

// embedded payload per-OS: payload-linux.zip / payload-windows.zip
// (see embed_linux.go / embed_windows.go)

type suiteFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type suiteManifest struct {
	Suite     string      `json:"suite"`
	Companion string      `json:"companion"`
	Programs  []string    `json:"programs"`
	Files     []suiteFile `json:"files"`
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func loadManifest(zr *zip.Reader) (*suiteManifest, map[string]*zip.File, error) {
	byName := map[string]*zip.File{}
	for _, f := range zr.File {
		byName[f.Name] = f
	}
	mf, ok := byName["suite-manifest.json"]
	if !ok {
		return nil, nil, fmt.Errorf("payload is missing its manifest (corrupt installer?)")
	}
	rc, err := mf.Open()
	if err != nil {
		return nil, nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(rc, 1<<20))
	_ = rc.Close()
	if err != nil {
		return nil, nil, err
	}
	var man suiteManifest
	if err := json.Unmarshal(raw, &man); err != nil {
		return nil, nil, fmt.Errorf("payload manifest unreadable: %w", err)
	}
	return &man, byName, nil
}

// readVerified pulls one payload member and checks it against the
// manifest - nothing is written to disk unverified.
func readVerified(byName map[string]*zip.File, man *suiteManifest, name string) ([]byte, error) {
	var want *suiteFile
	for i := range man.Files {
		if man.Files[i].Path == name {
			want = &man.Files[i]
			break
		}
	}
	if want == nil {
		return nil, fmt.Errorf("payload entry %s is not in the manifest", name)
	}
	f, ok := byName[name]
	if !ok {
		return nil, fmt.Errorf("payload is missing %s", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, want.Size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != want.Size || sha256Hex(data) != want.SHA256 {
		return nil, fmt.Errorf("payload entry %s failed its SHA-256 check - installer file is damaged, re-download it", name)
	}
	return data, nil
}

func writeFile(dst string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func install(dir string, quiet bool) error {
	fmt.Printf("%s - family suite installer v%s\n", productName, suiteVersion)
	fmt.Printf("This installs the desktop Companion v%s and the device programs:\n  %s\n(TALON is not part of the suite and is never installed by this program.)\n\n", companionVersion, "PHOENIX, SCRIBE, HEARTH, HERALD, KEEPER, BENCH, LANTERN")
	zr, err := zip.NewReader(bytes.NewReader(payloadZip), int64(len(payloadZip)))
	if err != nil {
		return fmt.Errorf("built-in payload unreadable: %w", err)
	}
	man, byName, err := loadManifest(zr)
	if err != nil {
		return err
	}
	fmt.Printf("Checking the installer's built-in files... ")
	for _, f := range man.Files {
		if _, err := readVerified(byName, man, f.Path); err != nil {
			fmt.Println("FAILED")
			return err
		}
	}
	fmt.Printf("all %d files OK.\n", len(man.Files))

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fmt.Printf("Installing into %s\n", dir)
	for _, f := range man.Files {
		if f.Path == "suite-manifest.json" {
			continue
		}
		// The other OS's companion binary is never in this payload,
		// so every remaining file belongs here.
		data, err := readVerified(byName, man, f.Path)
		if err != nil {
			return err
		}
		perm := os.FileMode(0o644)
		if f.Path == companionBinaryName() {
			perm = 0o755
		}
		if err := writeFile(filepath.Join(dir, filepath.FromSlash(f.Path)), data, perm); err != nil {
			return fmt.Errorf("writing %s: %w", f.Path, err)
		}
	}
	meta := map[string]string{
		"suite": suiteVersion, "companion": companionVersion,
		"installed": time.Now().Format("2006-01-02 15:04"), "os": runtime.GOOS,
	}
	if raw, err := json.MarshalIndent(meta, "", "  "); err == nil {
		_ = writeFile(filepath.Join(dir, "installed-version.json"), raw, 0o644)
	}
	if err := installShortcuts(dir); err != nil {
		return fmt.Errorf("creating the menu shortcut: %w", err)
	}
	if err := preserveSelfForUninstall(dir); err != nil {
		fmt.Printf("Note: could not stage the uninstaller copy (%v); uninstall later by re-running this file with --uninstall.\n", err)
	}
	fmt.Println()
	fmt.Println("Done. What you got:")
	fmt.Printf("  * %s is in your applications / Start Menu - launch it there.\n", productName)
	fmt.Printf("  * Program folder: %s\n", dir)
	fmt.Println("  * Device software: the Suite subfolder holds one ready-to-copy")
	fmt.Println("    package per AEGIS program (open Suite/SUITE-README.txt).")
	fmt.Println("  * Your synced logs and maps will live in the Library subfolder")
	fmt.Println("    and are never touched by reinstalls.")
	if !quiet {
		pause()
	}
	return nil
}

func uninstall(dir string, quiet bool) error {
	fmt.Printf("Removing %s (suite v%s)\n", productName, suiteVersion)
	// Data first: never delete a user's synced Library.
	lib := filepath.Join(dir, "Library")
	if st, err := os.Stat(lib); err == nil && st.IsDir() {
		backup, err := libraryBackupDir()
		if err != nil {
			return err
		}
		if _, err := os.Stat(backup); err == nil {
			backup = backup + "-" + time.Now().Format("20060102-150405")
		}
		fmt.Printf("Moving your Library (logs/maps) to %s so nothing is lost...\n", backup)
		if err := os.Rename(lib, backup); err != nil {
			return fmt.Errorf("could not move the Library aside (%w) - nothing was removed", err)
		}
	}
	// Stop any live Companion from this folder first (v1.2.2 fix):
	// a zombie copy left over from a window that never fully exited
	// used to re-create the Library/config while we removed them,
	// so uninstall looked like it "did nothing".
	killRunningCompanion(dir)
	_ = removeShortcuts(dir)
	removeSelfCopy(dir) // best effort; on Windows a running copy survives, note it
	var rmErr error
	for i := 0; i < 8; i++ {
		if rmErr = os.RemoveAll(dir); rmErr == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if rmErr != nil {
		return fmt.Errorf("removing %s: %w", dir, rmErr)
	}
	fmt.Println("Removed. Your Library backup (if any) is where the line above says.")
	if tip := uninstallTip(); tip != "" {
		fmt.Println(tip)
	}
	if !quiet {
		pause()
	}
	return nil
}

func pause() {
	fmt.Print("\nPress Enter to close...")
	_, _ = fmt.Scanln()
}

func main() {
	dirFlag := flag.String("dir", "", "install folder (default: per-user app folder)")
	unFlag := flag.Bool("uninstall", false, "remove the suite (Library is moved to a backup first)")
	quietFlag := flag.Bool("quiet", false, "no pause at the end (for scripts/tests)")
	flag.Parse()
	dir := *dirFlag
	if dir == "" {
		var err error
		dir, err = defaultInstallDir()
		if err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	}
	var err error
	if *unFlag {
		err = uninstall(dir, *quietFlag)
	} else {
		err = install(dir, *quietFlag)
	}
	if err != nil {
		fmt.Println("\nInstall did not finish:", err)
		if !*quietFlag {
			pause()
		}
		os.Exit(1)
	}
}

// sortedProgramList is only used for messages/tests.
func sortedProgramList() []string {
	p := []string{"PHOENIX", "SCRIBE", "HEARTH", "HERALD", "KEEPER", "BENCH", "LANTERN"}
	sort.Strings(p)
	return p
}

var _ = strings.TrimSpace
