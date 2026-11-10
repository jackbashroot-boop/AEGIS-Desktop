package main

// Companion-pushed device firmware (v1.8.2, user request): the PC
// fetches the newest PHOENIX image from the repo feed ONCE, verifies
// it, then offers it to live-linked Cardputers over the existing
// AEGIS/1 link. The device still decides: ENTER on the Cardputer
// accepts, then it pulls the image from this PC over the LAN (the
// fw_get exchange in server.go), SHA-256-verifies the whole file,
// and flashes itself. Nothing is ever pushed onto a device silently.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type fwFeedInfo struct {
	Version string `json:"version"`
	File    string `json:"file"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	BinURL  string `json:"-"`
}

type fwView struct {
	FeedVer   string
	StagedVer string
	Msg       string
	Busy      bool
	Pct       int
}

var gFw struct {
	sync.Mutex
	fwView
}

func fwSnapshot() fwView {
	gFw.Lock()
	defer gFw.Unlock()
	return gFw.fwView
}

func fwSet(fn func(*fwView)) {
	gFw.Lock()
	defer gFw.Unlock()
	fn(&gFw.fwView)
}

// fwFeedURL derives phoenix-latest.json next to the Companion feed.
func fwFeedURL(cfg *Config) string {
	feed := defaultUpdateFeed
	if cfg != nil && cfg.UpdateFeed != "" {
		feed = cfg.UpdateFeed
	}
	if strings.HasSuffix(feed, "latest.json") {
		return strings.TrimSuffix(feed, "latest.json") + "phoenix-latest.json"
	}
	return ""
}

func fetchFwFeed(cfg *Config) (fwFeedInfo, error) {
	var info fwFeedInfo
	feed := fwFeedURL(cfg)
	if feed == "" {
		return info, fmt.Errorf("update feed does not sit next to a phoenix-latest.json")
	}
	req, err := feedRequest(cfg, feed)
	if err != nil {
		return info, err
	}
	resp, err := feedClient().Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("device firmware feed%s", feedHint(resp.StatusCode))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return info, fmt.Errorf("device firmware feed is not valid JSON")
	}
	if info.Version == "" || info.File == "" || info.SHA256 == "" {
		return info, fmt.Errorf("device firmware feed is missing version/file/sha256")
	}
	base := feed[:strings.LastIndex(feed, "/")+1]
	info.BinURL = base + path.Base(info.File)
	return info, nil
}

func fwDir(cfg *Config) string {
	return filepath.Join(cfg.Library, "Updates", "firmware")
}

// stagedFw returns the verified PHOENIX image this PC can offer.
func stagedFw(cfg *Config) (fwFeedInfo, string, bool) {
	var info fwFeedInfo
	dir := fwDir(cfg)
	meta, err := os.ReadFile(filepath.Join(dir, "phoenix.json"))
	if err != nil {
		return info, "", false
	}
	if err := json.Unmarshal(meta, &info); err != nil || info.File == "" {
		return info, "", false
	}
	p := filepath.Join(dir, path.Base(info.File))
	st, err := os.Stat(p)
	if err != nil || (info.Size > 0 && st.Size() != info.Size) {
		return info, "", false
	}
	return info, p, true
}

func startFwCheck(cfg *Config, log *Log) {
	fwSet(func(v *fwView) {
		if v.Busy {
			return
		}
		v.Busy, v.Msg = true, "checking the device firmware feed..."
	})
	go func() {
		defer fwSet(func(v *fwView) { v.Busy = false })
		info, err := fetchFwFeed(cfg)
		if err != nil {
			fwSet(func(v *fwView) { v.Msg = "PHOENIX firmware: " + err.Error() })
			log.Warnf("device firmware check failed: %v", err)
			return
		}
		fwSet(func(v *fwView) {
			v.FeedVer = info.Version
			if st, _, ok := stagedFw(cfg); ok {
				v.StagedVer = st.Version
			}
			switch {
			case v.StagedVer == info.Version:
				v.Msg = fmt.Sprintf("PHOENIX %s is downloaded here, ready to offer to your Cardputer.", info.Version)
			case v.StagedVer != "":
				v.Msg = fmt.Sprintf("PHOENIX %s is out (this PC has %s ready). Download the new one to offer it.", info.Version, v.StagedVer)
			default:
				v.Msg = fmt.Sprintf("PHOENIX %s is the latest on the feed. Download it here to offer it to your Cardputer.", info.Version)
			}
		})
		log.Infof("device firmware check: latest PHOENIX %s", info.Version)
	}()
}

func startFwDownload(cfg *Config, log *Log) {
	fwSet(func(v *fwView) {
		if v.Busy {
			return
		}
		v.Busy, v.Pct, v.Msg = true, -1, "downloading the PHOENIX image here..."
	})
	go func() {
		defer fwSet(func(v *fwView) { v.Busy = false; v.Pct = -1 })
		info, err := fetchFwFeed(cfg)
		if err == nil {
			if st, _, ok := stagedFw(cfg); ok && st.Version == info.Version {
				fwSet(func(v *fwView) {
					v.StagedVer = st.Version
					v.FeedVer = info.Version
					v.Msg = "PHOENIX " + info.Version + " is already downloaded here, ready to offer."
				})
				return
			}
			err = downloadFwBin(cfg, info)
		}
		if err != nil {
			fwSet(func(v *fwView) { v.Msg = "PHOENIX download failed: " + err.Error() })
			log.Warnf("device firmware download failed: %v", err)
			return
		}
		fwSet(func(v *fwView) {
			v.FeedVer, v.StagedVer = info.Version, info.Version
			v.Msg = "PHOENIX " + info.Version + " verified on this PC. Use 'Send update' on the device's card (or Update everything)."
		})
		log.Infof("device firmware staged: PHOENIX %s (SHA-256 verified)", info.Version)
	}()
}

func downloadFwBin(cfg *Config, info fwFeedInfo) error {
	req, err := feedRequest(cfg, info.BinURL)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("image download%s", feedHint(resp.StatusCode))
	}
	dir := fwDir(cfg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".download.tmp")
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	total := resp.ContentLength
	if info.Size > 0 {
		total = info.Size
	}
	buf := make([]byte, 64*1024)
	var got int64
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(tmp)
				return werr
			}
			h.Write(buf[:n])
			got += int64(n)
			if total > 0 {
				pct := int(got * 100 / total)
				fwSet(func(v *fwView) { v.Pct = pct })
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(tmp)
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(sum, info.SHA256) || (info.Size > 0 && got != info.Size) {
		os.Remove(tmp)
		return fmt.Errorf("image failed SHA-256 verification - refused, nothing offered to devices")
	}
	dest := filepath.Join(dir, path.Base(info.File))
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	meta, _ := json.Marshal(info)
	return os.WriteFile(filepath.Join(dir, "phoenix.json"), meta, 0o644)
}

// fwNewer reports whether version a is strictly newer than b (x.y.z).
func fwNewer(a, b string) bool {
	pa, pb := parseSemver(a), parseSemver(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parseSemver(s string) [3]int {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		out[i], _ = strconv.Atoi(strings.TrimSpace(parts[i]))
	}
	return out
}

// offerFirmware sends the staged image offer to one live device. The
// device still gates everything behind its ENTER key.
func (u *UI) offerFirmware(prog string) {
	info, _, ok := stagedFw(u.core.Cfg)
	if !ok {
		u.toast("No PHOENIX image downloaded here yet - use 'Get latest PHOENIX' first.", 6)
		return
	}
	if sent := u.core.Server.SendFirmwareOffer(prog, info); sent {
		u.toastGood("Update "+info.Version+" offered to "+prog+" - now press ENTER on the Cardputer. It downloads over Wi-Fi and flashes itself; watch its screen.", 9)
		u.core.Log.Infof("firmware offer sent: PHOENIX %s -> %s", info.Version, prog)
	} else {
		u.toast(prog+" is not live-linked right now - turn it on, let it join Wi-Fi and link, then send again.", 7)
	}
}

// syncEverything is Dashboard's one click (v1.8.2): refresh both
// feeds, fetch the newest PHOENIX here if needed, offer it to every
// live Cardputer that is behind, and stage the Companion's own
// update download so Settings can finish it (the app must restart
// to replace itself, so that last step stays a click there).
func (u *UI) syncEverything() {
	cfg := u.core.Cfg
	startUpdateCheck(cfg)
	startFwCheck(cfg, u.core.Log)
	fw := fwSnapshot()
	uv := updateSnapshot()
	offers := 0
	if fw.StagedVer != "" {
		for _, d := range u.core.Live.Snapshot() {
			if d.Prog == "PHOENIX" && d.Ver != "" && fwNewer(fw.StagedVer, d.Ver) &&
				u.core.Live.Online(d, time.Now()) {
				if u.core.Server.SendFirmwareOffer(d.Prog, mustStaged(cfg)) {
					offers++
				}
			}
		}
	}
	switch {
	case fw.FeedVer != "" && fw.StagedVer != fw.FeedVer:
		startFwDownload(cfg, u.core.Log)
		u.toastGood("Getting PHOENIX "+fw.FeedVer+" here first - when the download finishes, Update everything offers it to your Cardputer (ENTER there accepts).", 9)
	case offers > 0:
		u.toastGood("Update offered - press ENTER on the Cardputer to accept. It pulls the image over Wi-Fi and flashes itself.", 9)
	case uv.haveRemote:
		u.toastGood("Companion "+uv.latest.Version+" is available - Settings > Install update finishes it (the app restarts itself). Devices are already current.", 9)
	default:
		u.toastGood("Checking everything... watch the Updates panel - if a newer PHOENIX appears, it downloads here, then offers itself to your Cardputer.", 8)
		if fw.FeedVer == "" {
			startFwDownload(cfg, u.core.Log)
		}
	}
}

func mustStaged(cfg *Config) fwFeedInfo {
	info, _, _ := stagedFw(cfg)
	return info
}
