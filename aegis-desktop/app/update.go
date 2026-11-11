package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
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
	// Companion/Suite (v1.8.10): the suite-era feeds (v1.2.15-1.2.21)
	// put the SUITE version (1.2.x) in "version", which always sorts
	// BELOW the Companion's own 1.8.x - every Companion concluded
	// "up to date" forever. Feeds now carry the Companion version in
	// "version" AND "companion", with "suite" for display. effVersion
	// prefers "companion" so both feed eras resolve correctly.
	Companion string `json:"companion"`
	Suite     string `json:"suite"`
	File      string `json:"file"`
	Notes     string `json:"notes"`
	// URL (optional) is the direct installer download; when empty the
	// installer is fetched next to the feed's latest.json. SHA256
	// (required for downloads) is verified before anything runs.
	// FileLinux/FileWindows + SHA256Linux/SHA256Windows let ONE
	// latest.json serve both OSes (a release ships both installers).
	URL           string `json:"url"`
	SHA256        string `json:"sha256"`
	FileLinux     string `json:"file_linux"`
	FileWindows   string `json:"file_windows"`
	SHA256Linux   string `json:"sha256_linux"`
	SHA256Windows string `json:"sha256_windows"`
	// Files is the nested per-OS shape our own release generator
	// emits: {"files":{"linux":{"name","sha256","size"},...}}.
	// (Before v1.7.9 the parser only knew the flat fields above,
	// so a check could SAY an update existed while Install always
	// failed with "names no installer file" - found in the audit.)
	Files map[string]feedEntry `json:"files"`
	// Top-level per-OS objects (another emitted shape, v1.2.15+).
	Linux   feedEntry `json:"linux"`
	Windows feedEntry `json:"windows"`
}

// feedEntry is one OS's installer entry. "name" is the canonical key;
// "file" was emitted by the suite-era generator - accept both.
type feedEntry struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func (e feedEntry) fileName() string {
	if e.Name != "" {
		return e.Name
	}
	return e.File
}

// effVersion is the Companion version this feed offers.
func (l updateLatest) effVersion() string {
	if l.Companion != "" {
		return l.Companion
	}
	return l.Version
}

func (l updateLatest) osEntry() (name, sha string, size int64, ok bool) {
	key := "linux"
	if runtime.GOOS == "windows" {
		key = "windows"
	}
	if l.Files != nil {
		if e, hit := l.Files[key]; hit && e.fileName() != "" {
			return e.fileName(), e.SHA256, e.Size, true
		}
	}
	e := l.Linux
	if runtime.GOOS == "windows" {
		e = l.Windows
	}
	if e.fileName() != "" {
		return e.fileName(), e.SHA256, e.Size, true
	}
	return "", "", 0, false
}

// effectiveFile is the installer file name for this OS.
func (l updateLatest) effectiveFile() string {
	if n, _, _, ok := l.osEntry(); ok {
		return n
	}
	if runtime.GOOS == "windows" && l.FileWindows != "" {
		return l.FileWindows
	}
	if runtime.GOOS != "windows" && l.FileLinux != "" {
		return l.FileLinux
	}
	return l.File
}

// effectiveSHA256 is the expected installer hash for this OS.
func (l updateLatest) effectiveSHA256() string {
	if _, sha, _, ok := l.osEntry(); ok {
		return sha
	}
	if runtime.GOOS == "windows" && l.SHA256Windows != "" {
		return l.SHA256Windows
	}
	if runtime.GOOS != "windows" && l.SHA256Linux != "" {
		return l.SHA256Linux
	}
	return l.SHA256
}

// effectiveSize is the expected installer size in bytes (0 = unknown).
func (l updateLatest) effectiveSize() int64 {
	_, _, size, _ := l.osEntry()
	return size
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
	if _, ok := parseVersion(latest.effVersion()); !ok {
		return updateLatest{}, "", fmt.Errorf("latest.json version %q is not x.y.z", latest.effVersion())
	}
	ef := latest.effectiveFile()
	if ef == "" {
		return latest, "", nil
	}
	// The installer must be a plain file name inside Updates/ -
	// never a path that escapes the folder.
	if filepath.Base(ef) != ef {
		return updateLatest{}, "", fmt.Errorf("latest.json file %q must be a plain file name", ef)
	}
	p := filepath.Join(dir, ef)
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
	// --quiet is load-bearing (v1.7.8): without it the uninstaller
	// ends in an interactive "Press Enter to close..." - and a GUI
	// launch has no terminal, so it looked like nothing happened.
	cmd := exec.Command(p, "--uninstall", "--quiet")
	cmd.Dir = filepath.Dir(p)
	return cmd.Start()
}

// ------------------------------------------------------- repo feed (v1.7.1)
// The Dashboard/Settings updater now checks the AEGIS-Desktop repo
// release feed in addition to the staged Updates/ folder. Checking
// only downloads the tiny latest.json; the installer itself is
// downloaded only when the user clicks "Install update", is
// SHA-256-verified against latest.json, and lands in Updates/ where
// the original staged-install path runs it. If the repo is private,
// GitHub answers 404 to anonymous requests: the check says so, and a
// read-only token pasted into Settings (stored only in the local
// 0600 config) makes it work without making the repo public.

const defaultUpdateFeed = "https://github.com/jackbashroot-boop/AEGIS-Desktop/releases/latest/download/latest.json"

// updateState is shared between the async check/download goroutines
// and the UI (which polls it each frame).
type updateState struct {
	mu         sync.Mutex
	busy       bool
	phase      string // "check" | "download" | ""
	pct        int    // download percent 0-100, -1 = indeterminate
	msg        string
	latest     updateLatest
	haveRemote bool   // a newer release is known at the feed
	stagedPath string // installer ready to run
	checked    bool
}

var gUpdate updateState

// updateView is a lock-free copy of updateState for readers.
type updateView struct {
	busy       bool
	phase      string
	pct        int
	msg        string
	latest     updateLatest
	haveRemote bool
	stagedPath string
	checked    bool
}

func updateSnapshot() updateView {
	gUpdate.mu.Lock()
	defer gUpdate.mu.Unlock()
	return updateView{gUpdate.busy, gUpdate.phase, gUpdate.pct, gUpdate.msg, gUpdate.latest,
		gUpdate.haveRemote, gUpdate.stagedPath, gUpdate.checked}
}

func (st *updateState) set(fn func(*updateState)) {
	st.mu.Lock()
	defer st.mu.Unlock()
	fn(st)
}

func feedClient() *http.Client { return &http.Client{Timeout: 20 * time.Second} }

func feedRequest(cfg *Config, url string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AEGIS-Companion/"+appVersion)
	if cfg != nil && cfg.GitHubToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.GitHubToken)
	}
	return req, nil
}

func feedHint(status int) string {
	if status == http.StatusNotFound {
		return " (the repo feed answered 404: if AEGIS-Desktop is private, anonymous update checks cannot see it - make the repo public, or paste a read-only GitHub token in Settings below)"
	}
	return fmt.Sprintf(" (HTTP %d)", status)
}

// fetchLatest downloads and validates the feed's latest.json.
func fetchLatest(cfg *Config) (updateLatest, error) {
	feed := defaultUpdateFeed
	if cfg != nil && cfg.UpdateFeed != "" {
		feed = cfg.UpdateFeed
	}
	req, err := feedRequest(cfg, feed)
	if err != nil {
		return updateLatest{}, err
	}
	resp, err := feedClient().Do(req)
	if err != nil {
		return updateLatest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return updateLatest{}, fmt.Errorf("update feed unreachable%s", feedHint(resp.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return updateLatest{}, err
	}
	var latest updateLatest
	if err := json.Unmarshal(raw, &latest); err != nil {
		return updateLatest{}, fmt.Errorf("the feed's latest.json is not readable: %w", err)
	}
	if _, ok := parseVersion(latest.effVersion()); !ok {
		return updateLatest{}, fmt.Errorf("the feed's latest.json version %q is not x.y.z", latest.effVersion())
	}
	if ef := latest.effectiveFile(); ef != "" && filepath.Base(ef) != ef {
		return updateLatest{}, fmt.Errorf("the feed's latest.json file %q must be a plain file name", ef)
	}
	return latest, nil
}

// installerURL resolves where the release installer downloads from.
func installerURL(cfg *Config, latest updateLatest) (string, error) {
	if latest.URL != "" {
		if !strings.HasPrefix(latest.URL, "https://") {
			return "", fmt.Errorf("latest.json url must be https")
		}
		return latest.URL, nil
	}
	ef := latest.effectiveFile()
	if ef == "" {
		return "", fmt.Errorf("latest.json names no installer file")
	}
	feed := defaultUpdateFeed
	if cfg != nil && cfg.UpdateFeed != "" {
		feed = cfg.UpdateFeed
	}
	u, err := url.Parse(feed)
	if err != nil {
		return "", err
	}
	u.Path = path.Join(path.Dir(u.Path), ef)
	return u.String(), nil
}

// startUpdateCheck kicks the async check (local staged first, then
// the repo feed). Safe to call repeatedly; it no-ops while busy.
func startUpdateCheck(cfg *Config) {
	if updateSnapshot().busy {
		return
	}
	gUpdate.set(func(st *updateState) {
		st.busy, st.phase, st.pct, st.msg = true, "check", -1, "Checking for updates..."
	})
	go func() {
		finish := func(msg string, fn func(*updateState)) {
			gUpdate.set(func(st *updateState) {
				st.busy, st.phase, st.checked, st.msg = false, "", true, msg
				if fn != nil {
					fn(st)
				}
			})
		}
		if latest, p, err := findUpdate(); err == nil && p != "" && versionNewer(latest.effVersion(), appVersion) {
			finish(fmt.Sprintf("Update v%s is already downloaded and staged in the Updates folder - click Install update to run it.", latest.effVersion()),
				func(st *updateState) { st.stagedPath, st.latest = p, latest })
			return
		}
		latest, err := fetchLatest(cfg)
		if err != nil {
			finish("No update staged locally, and the online check did not complete: "+err.Error(), nil)
			return
		}
		if !versionNewer(latest.effVersion(), appVersion) {
			finish(fmt.Sprintf("You are up to date - this is v%s and the newest release is v%s.", appVersion, latest.effVersion()), nil)
			return
		}
		msg := fmt.Sprintf("Update v%s is available from the AEGIS-Desktop repo. Click Install update to download it (SHA-256 verified) and run the installer - your Library is kept.", latest.effVersion())
		if latest.Notes != "" {
			msg += " Notes: " + latest.Notes
		}
		finish(msg, func(st *updateState) { st.latest, st.haveRemote = latest, true })
	}()
}

// progWriter reports download progress into updateState.
type progWriter struct {
	w     io.Writer
	n     int64
	total int64
	last  int
}

func (p *progWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.n += int64(n)
	if p.total > 0 {
		pct := int(p.n * 100 / p.total)
		if pct != p.last {
			p.last = pct
			gUpdate.set(func(st *updateState) { st.pct = pct })
		}
	}
	return n, err
}

// startUpdateDownload downloads the known-newer release installer
// into Updates/, verifies SHA-256, and stages it for Install.
func startUpdateDownload(cfg *Config) {
	snap := updateSnapshot()
	if snap.busy || !snap.haveRemote {
		return
	}
	latest := snap.latest
	gUpdate.set(func(st *updateState) {
		st.busy, st.phase, st.msg = true, "download", "Downloading update v"+latest.effVersion()+"..."
	})
	go func() {
		fail := func(err error) {
			gUpdate.set(func(st *updateState) {
				st.busy, st.phase = false, ""
				st.msg = "Update download failed: " + err.Error()
			})
		}
		if latest.effectiveSHA256() == "" {
			fail(fmt.Errorf("the release latest.json has no sha256 - refusing an unverified download"))
			return
		}
		dl, err := installerURL(cfg, latest)
		if err != nil {
			fail(err)
			return
		}
		req, err := feedRequest(cfg, dl)
		if err != nil {
			fail(err)
			return
		}
		resp, err := feedClient().Do(req)
		if err != nil {
			fail(err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			fail(fmt.Errorf("installer download unreachable%s", feedHint(resp.StatusCode)))
			return
		}
		dir := updatesDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fail(err)
			return
		}
		tmp := filepath.Join(dir, ".download.part")
		f, err := os.Create(tmp)
		if err != nil {
			fail(err)
			return
		}
		h := sha256.New()
		total := resp.ContentLength
		if sz := latest.effectiveSize(); sz > 0 {
			total = sz
		}
		if total > 0 {
			gUpdate.set(func(st *updateState) { st.pct = 0 })
		}
		pw := &progWriter{w: io.MultiWriter(f, h), total: total, last: -1}
		_, copyErr := io.Copy(pw, io.LimitReader(resp.Body, 300*1024*1024))
		if total > 0 {
			gUpdate.set(func(st *updateState) { st.pct = 100 })
		}
		closeErr := f.Close()
		if copyErr != nil || closeErr != nil {
			_ = os.Remove(tmp)
			fail(fmt.Errorf("download interrupted"))
			return
		}
		if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, latest.effectiveSHA256()) {
			_ = os.Remove(tmp)
			fail(fmt.Errorf("SHA-256 mismatch (got %s) - download discarded", got))
			return
		}
		name := latest.effectiveFile()
		if name == "" {
			name = filepath.Base(dl)
		}
		dst := filepath.Join(dir, name)
		if err := os.Rename(tmp, dst); err != nil {
			fail(err)
			return
		}
		if runtime.GOOS != "windows" {
			_ = os.Chmod(dst, 0o755)
		}
		gUpdate.set(func(st *updateState) {
			st.busy, st.phase = false, ""
			st.stagedPath = dst
			st.msg = fmt.Sprintf("Update v%s downloaded and SHA-256 verified - click Install update to run it.", latest.effVersion())
		})
	}()
}

// updateHealthLine is the Dashboard one-liner about updates.
func updateHealthLine() HealthItem {
	st := updateSnapshot()
	switch {
	case st.busy:
		return HealthItem{"Updates", "checking the AEGIS-Desktop repo...", true, true}
	case st.stagedPath != "":
		return HealthItem{"Updates", "v" + st.latest.effVersion() + " downloaded and ready - Settings -> Install update", false, true}
	case st.haveRemote:
		return HealthItem{"Updates", "v" + st.latest.effVersion() + " available - Settings -> Check for updates", false, true}
	case st.checked:
		return HealthItem{"Updates", "up to date (v" + appVersion + "), checked against the repo", true, false}
	default:
		if latest, p, err := findUpdate(); err == nil && p != "" && versionNewer(latest.effVersion(), appVersion) {
			return HealthItem{"Updates", "v" + latest.effVersion() + " staged in the Updates folder - Settings -> Install update", false, true}
		}
		return HealthItem{"Updates", "not checked yet this run - Settings -> Check for updates", true, true}
	}
}
