package main

// Publish-to-GitHub (v1.8.3, user request): the Companion can upload
// the staged, verified PHOENIX image + a fresh phoenix-latest.json
// to the repo's current latest release, using the GitHub CLI the
// user is already logged into (`gh auth login`). The app never sees
// or stores the token - gh keeps it in the OS keyring; we only run
// the CLI and report what it says. The Structural Fix for builds
// (GitHub Actions) lives in .github/workflows/release.yml in the
// repo; this button covers "I have a firmware file, put it up".

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// repoFromFeed extracts OWNER/REPO from a .../OWNER/REPO/releases/...
// update-feed URL. "" when it isn't a GitHub releases feed.
func repoFromFeed(feed string) string {
	i := strings.Index(feed, "github.com/")
	if i < 0 {
		return ""
	}
	rest := feed[i+len("github.com/"):]
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func ghRun(args ...string) (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("the GitHub CLI (gh) is not installed - install it, run 'gh auth login', then try again")
	}
	out, err := exec.Command("gh", args...).CombinedOutput()
	txt := strings.TrimSpace(string(out))
	if err != nil {
		if txt == "" {
			txt = err.Error()
		}
		return txt, fmt.Errorf("gh said: %s", txt)
	}
	return txt, nil
}

// startPublishFirmware uploads the staged image to the latest
// release so Cardputers (feed pull) and this PC (push offers) both
// see it. Async; progress lands in the firmware status line + log.
func startPublishFirmware(cfg *Config, log *Log) {
	info, binPath, ok := stagedFw(cfg)
	if !ok {
		log.Warnf("publish: no staged firmware to publish")
		return
	}
	repo := repoFromFeed(cfg.UpdateFeed)
	if cfg.UpdateFeed == "" {
		repo = repoFromFeed(defaultUpdateFeed)
	}
	if repo == "" {
		fwSet(func(v *fwView) { v.Msg = "Publish failed: your update feed is not a GitHub releases URL." })
		return
	}
	fwSet(func(v *fwView) {
		v.Busy = true
		v.Msg = "publishing PHOENIX " + info.Version + " to GitHub (" + repo + ")..."
	})
	go func() {
		defer fwSet(func(v *fwView) { v.Busy = false })
		fail := func(err error) {
			fwSet(func(v *fwView) { v.Msg = "Publish failed: " + err.Error() })
			log.Warnf("publish firmware: %v", err)
		}
		if _, err := ghRun("auth", "status"); err != nil {
			fail(fmt.Errorf("not logged into gh - run 'gh auth login' in a terminal first (%v)", err))
			return
		}
		tag, err := ghRun("release", "view", "-R", repo, "--json", "tagName", "-q", ".tagName")
		if err != nil {
			fail(fmt.Errorf("could not read the latest release of %s (%v)", repo, err))
			return
		}
		// Fresh feed file pointing at the staged image.
		dir := filepath.Join(cfg.Library, "Updates", "firmware", "publish")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fail(err)
			return
		}
		feed := map[string]any{"version": info.Version, "file": info.File,
			"sha256": info.SHA256, "size": info.Size,
			"notes": "Published from Aegis Companion " + appVersion}
		feedJSON, _ := json.MarshalIndent(feed, "", "  ")
		feedPath := filepath.Join(dir, "phoenix-latest.json")
		if err := os.WriteFile(feedPath, feedJSON, 0o644); err != nil {
			fail(err)
			return
		}
		binCopy := filepath.Join(dir, info.File)
		data, err := os.ReadFile(binPath)
		if err != nil {
			fail(err)
			return
		}
		if err := os.WriteFile(binCopy, data, 0o644); err != nil {
			fail(err)
			return
		}
		out, err := ghRun("release", "upload", tag, binCopy, feedPath, "--clobber", "-R", repo)
		if err != nil {
			fail(err)
			return
		}
		_ = out
		fwSet(func(v *fwView) {
			v.Msg = "Published PHOENIX " + info.Version + " to " + repo + " release " + tag + " - Cardputers can pull it, and Send update offers this same image."
		})
		log.Infof("published PHOENIX %s to %s@%s via gh", info.Version, repo, tag)
	}()
}
