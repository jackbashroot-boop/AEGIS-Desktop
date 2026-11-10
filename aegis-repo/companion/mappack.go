package main

import (
	"archive/zip"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Standard map library: a saved tile area can be exported as ONE
// portable pack file (Library/maps/packs/*.aegismap) and imported on
// any machine running the companion - no web call, ever, after the
// first download. Build it once at home, copy it with the program,
// share it between your own computers.
//
// Packs are SIGNED (v1.5.0+ suggestion, implemented v1.6.0): the
// manifest carries a SHA-256 tree hash of every tile, and an
// aegismap.sig entry holds HMAC-SHA256(companion token, manifest).
// Import verifies the tree and, when a signature is present, the
// HMAC - a tampered or foreign-signed pack is refused, never merged.
// Unsigned (legacy or hand-made) packs still import manually, and
// say so; only signature-verified packs are auto-imported at startup.
//
// Layout (plain ZIP): tiles/<z>/<x>/<y>.png + area.json +
// aegismap.json + aegismap.sig.

const mapPackFormat = 1

type PackSigState int

const (
	PackUnsigned PackSigState = iota // no signature (or no manifest)
	PackVerified                     // HMAC verified against our token
)

type mapPackManifest struct {
	Format     int     `json:"format"`
	Exported   string  `json:"exported"`
	App        string  `json:"app"`
	Tiles      int     `json:"tiles"`
	TreeSHA256 string  `json:"tree_sha256,omitempty"`
	HomeLat    float64 `json:"home_lat,omitempty"`
	HomeLon    float64 `json:"home_lon,omitempty"`
	HalfMiles  float64 `json:"half_miles,omitempty"`
	MinZoom    int     `json:"min_zoom,omitempty"`
	MaxZoom    int     `json:"max_zoom,omitempty"`
}

func mapPacksDir(root string) string { return filepath.Join(root, "maps", "packs") }

// ListMapPacks returns pack files waiting in the standard library folder.
func ListMapPacks(root string) []string {
	ents, err := os.ReadDir(mapPacksDir(root))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".aegismap") {
			out = append(out, filepath.Join(mapPacksDir(root), e.Name()))
		}
	}
	return out
}

// treeHash is the pack's integrity root: SHA-256 over the sorted
// "tiles/<z>/<x>/<y>.png <sha256-of-content>" lines.
func treeHash(hashes map[string]string) string {
	names := make([]string, 0, len(hashes))
	for n := range hashes {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%s %s\n", n, hashes[n])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ExportMapPack writes the whole tile cache (+ saved-area receipt) to
// dest as one signed .aegismap pack. Returns tiles written.
func ExportMapPack(root, dest, token string) (int, error) {
	tilesDir := filepath.Join(root, "maps", "tiles")
	count := 0
	hashes := map[string]string{}
	man := mapPackManifest{Format: mapPackFormat, Exported: time.Now().Format("2006-01-02 15:04"), App: "AEGIS Companion " + appVersion}
	if area, ok := LoadOfflineArea(root); ok {
		man.HomeLat, man.HomeLon = area.HomeLat, area.HomeLon
		man.HalfMiles, man.MinZoom, man.MaxZoom = area.HalfMiles, area.MinZoom, area.MaxZoom
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 0, err
	}
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	zw := zip.NewWriter(f)
	werr := filepath.Walk(tilesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(tilesDir, path)
		if err != nil {
			return nil
		}
		name := "tiles/" + filepath.ToSlash(rel)
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return nil // vanished mid-walk: skip, never fail the export
		}
		defer src.Close()
		hw := sha256.New()
		if _, err := io.Copy(io.MultiWriter(w, hw), src); err != nil {
			return err
		}
		hashes[name] = hex.EncodeToString(hw.Sum(nil))
		count++
		return nil
	})
	if werr == nil {
		if raw, err := os.ReadFile(areaFile(root)); err == nil {
			if w, err := zw.Create("area.json"); err == nil {
				_, werr = w.Write(raw)
			}
		}
	}
	if werr == nil {
		man.Tiles = count
		man.TreeSHA256 = treeHash(hashes)
		raw, err := json.MarshalIndent(man, "", "  ")
		if err != nil {
			werr = err
		} else {
			if w, err := zw.Create("aegismap.json"); err == nil {
				if _, werr = w.Write(raw); werr == nil {
					if w2, err := zw.Create("aegismap.sig"); err == nil {
						_, werr = w2.Write([]byte(hmacHex(token, string(raw))))
					} else {
						werr = err
					}
				}
			} else {
				werr = err
			}
		}
	}
	if err := zw.Close(); werr == nil {
		werr = err
	}
	if err := f.Close(); werr == nil {
		werr = err
	}
	if werr != nil {
		_ = os.Remove(tmp)
		return 0, werr
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return count, nil
}

var tileEntryRe = regexp.MustCompile(`^tiles/\d{1,2}/\d+/\d+\.png$`)

// ImportMapPack verifies, then merges a pack into the tile cache.
// Extraction goes to a staging folder first: NOTHING reaches the live
// cache until the tree hash (and the HMAC signature, when present)
// checks out. Returns tiles imported, entries skipped, and whether
// the pack's signature verified against our token.
func ImportMapPack(root, packPath, token string) (tiles int, skipped int, state PackSigState, err error) {
	return importMapPack(root, packPath, token, false)
}

// importMapPack is the shared engine. With verifiedOnly set (startup
// auto-import), an unsigned pack is verified-and-reported but NEVER
// merged - silently trusting any file dropped in a folder is exactly
// what the signature exists to prevent.
func importMapPack(root, packPath, token string, verifiedOnly bool) (tiles int, skipped int, state PackSigState, err error) {
	r, err := zip.OpenReader(packPath)
	if err != nil {
		return 0, 0, PackUnsigned, fmt.Errorf("not a map pack: %w", err)
	}
	defer r.Close()
	const maxTileBytes = 2 << 20    // a 256px PNG tile is far below this
	const maxTotalBytes = 800 << 20 // whole-pack ceiling (zip-bomb guard)
	const maxEntries = 40000
	if len(r.File) > maxEntries {
		return 0, 0, PackUnsigned, fmt.Errorf("pack has %d entries (limit %d)", len(r.File), maxEntries)
	}
	var totalBytes uint64
	for _, e := range r.File {
		totalBytes += e.UncompressedSize64
	}
	if totalBytes > maxTotalBytes {
		return 0, 0, PackUnsigned, fmt.Errorf("pack expands to %d MB (limit %d MB)", totalBytes>>20, maxTotalBytes>>20)
	}

	mapsDir := filepath.Join(root, "maps")
	if err := os.MkdirAll(mapsDir, 0o755); err != nil {
		return 0, 0, PackUnsigned, err
	}
	stage, err := os.MkdirTemp(mapsDir, ".import-")
	if err != nil {
		return 0, 0, PackUnsigned, err
	}
	defer os.RemoveAll(stage)

	hashes := map[string]string{}
	var manifestRaw []byte
	var man mapPackManifest
	var sigHex string
	haveManifest := false
	stagedArea := false
	for _, entry := range r.File {
		name := entry.Name
		switch {
		case tileEntryRe.MatchString(name):
			if entry.UncompressedSize64 > maxTileBytes {
				skipped++
				continue
			}
			rc, err := entry.Open()
			if err != nil {
				skipped++
				continue
			}
			dst := filepath.Join(stage, filepath.FromSlash(name))
			werr := func() error {
				defer rc.Close()
				if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
					return err
				}
				w, err := os.Create(dst)
				if err != nil {
					return err
				}
				hw := sha256.New()
				_, err = io.Copy(io.MultiWriter(w, hw), io.LimitReader(rc, maxTileBytes+1))
				if cerr := w.Close(); err == nil {
					err = cerr
				}
				if err == nil {
					hashes[name] = hex.EncodeToString(hw.Sum(nil))
				}
				return err
			}()
			if werr != nil {
				skipped++
				continue
			}
			tiles++
		case name == "area.json":
			if entry.UncompressedSize64 > 64<<10 {
				skipped++
				continue
			}
			rc, err := entry.Open()
			if err != nil {
				skipped++
				continue
			}
			raw, rerr := io.ReadAll(io.LimitReader(rc, 64<<10))
			_ = rc.Close()
			if rerr != nil {
				skipped++
				continue
			}
			var a OfflineArea
			if json.Unmarshal(raw, &a) != nil || a.Tiles == 0 {
				skipped++
				continue
			}
			if err := os.WriteFile(filepath.Join(stage, "area.json"), raw, 0o644); err == nil {
				stagedArea = true
			}
		case name == "aegismap.json":
			if entry.UncompressedSize64 > 256<<10 {
				skipped++
				continue
			}
			rc, err := entry.Open()
			if err != nil {
				skipped++
				continue
			}
			raw, rerr := io.ReadAll(io.LimitReader(rc, 256<<10))
			_ = rc.Close()
			if rerr == nil && json.Unmarshal(raw, &man) == nil {
				manifestRaw, haveManifest = raw, true
			} else {
				skipped++
			}
		case name == "aegismap.sig":
			rc, err := entry.Open()
			if err != nil {
				skipped++
				continue
			}
			raw, rerr := io.ReadAll(io.LimitReader(rc, 4096))
			_ = rc.Close()
			if rerr == nil {
				sigHex = strings.TrimSpace(string(raw))
			}
		default:
			skipped++
		}
	}

	// --- verify BEFORE anything touches the live cache ---
	state = PackUnsigned
	if haveManifest && man.TreeSHA256 != "" {
		if got := treeHash(hashes); got != man.TreeSHA256 {
			return 0, skipped, PackUnsigned, fmt.Errorf("pack contents do not match its manifest (tree hash mismatch) - refusing to import")
		}
	}
	if sigHex != "" {
		if !haveManifest {
			return 0, skipped, PackUnsigned, fmt.Errorf("pack has a signature but no manifest - refusing to import")
		}
		want := hmacHex(token, string(manifestRaw))
		if subtle.ConstantTimeCompare([]byte(want), []byte(sigHex)) != 1 {
			return 0, skipped, PackUnsigned, fmt.Errorf("pack signature does NOT verify against this companion's token - it was signed by a different companion (or tampered with); refusing to import")
		}
		state = PackVerified
	}

	if verifiedOnly && state != PackVerified {
		return 0, skipped, state, nil // staging is wiped by the deferred cleanup
	}

	// --- merge the verified staging tree into the cache ---
	mergeErr := filepath.Walk(stage, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(stage, path)
		if err != nil {
			return nil
		}
		dst := filepath.Join(mapsDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.Rename(path, dst)
	})
	_ = stagedArea // area.json rode along in the same walk
	if mergeErr != nil {
		return 0, skipped, state, mergeErr
	}
	return tiles, skipped, state, nil
}

// --- auto-import bookkeeping -------------------------------------------

type packMark struct {
	Size     int64  `json:"size"`
	ModUnix  int64  `json:"mod"`
	Verified bool   `json:"verified"`
	Outcome  string `json:"outcome"`
}

func packMarksPath(root string) string { return filepath.Join(mapPacksDir(root), ".imported.json") }

func loadPackMarks(root string) map[string]packMark {
	out := map[string]packMark{}
	if raw, err := os.ReadFile(packMarksPath(root)); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func markPackImported(root, packPath, outcome string, verified bool) {
	st, err := os.Stat(packPath)
	if err != nil {
		return
	}
	marks := loadPackMarks(root)
	marks[filepath.Base(packPath)] = packMark{Size: st.Size(), ModUnix: st.ModTime().Unix(), Verified: verified, Outcome: outcome}
	_ = os.MkdirAll(mapPacksDir(root), 0o755)
	if raw, err := json.MarshalIndent(marks, "", "  "); err == nil {
		_ = os.WriteFile(packMarksPath(root), raw, 0o644)
	}
}

// autoImportMapPacks runs at startup: packs already handled (same
// size+mtime) are skipped; NEW packs are imported only when their
// signature verifies against our token. Unsigned packs are left for
// the manual Settings button - silently trusting any file dropped in
// a folder is exactly what the signature is for. Either way the
// outcome is recorded so we do not nag (or re-scan 150 MB) each boot.
func autoImportMapPacks(root, token string, log *Log) {
	for _, pack := range ListMapPacks(root) {
		st, err := os.Stat(pack)
		if err != nil {
			continue
		}
		if m, ok := loadPackMarks(root)[filepath.Base(pack)]; ok && m.Size == st.Size() && m.ModUnix == st.ModTime().Unix() {
			continue // already handled this exact file
		}
		tiles, _, state, err := importMapPack(root, pack, token, true)
		switch {
		case err != nil:
			log.Warnf("map pack %s not auto-imported: %v", filepath.Base(pack), err)
			markPackImported(root, pack, "error: "+err.Error(), false)
		case state != PackVerified:
			log.Warnf("map pack %s is unsigned - not auto-imported. Import it manually in Settings > Wardrive map if you trust it.", filepath.Base(pack))
			markPackImported(root, pack, "unsigned, left for manual import", false)
		default:
			log.Infof("auto-imported signed map pack %s: %d squares (no internet used)", filepath.Base(pack), tiles)
			markPackImported(root, pack, "auto-imported", true)
		}
	}
}
