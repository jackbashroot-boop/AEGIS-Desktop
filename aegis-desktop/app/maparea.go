package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Saved offline map area: a 100x100-mile square of OpenStreetMap tiles
// around the user's home, downloaded once (Settings or the Wardrive Map
// page) into the same tile cache the live map uses. With "saved map
// only" on, the Wardrive Map builds its street map purely from disk -
// no web call - so wardrives near home always render, internet or not.
// Downloads resume for free: tiles already in the cache are skipped.

const (
	areaHalfMiles = 50.0 // 100x100 miles total
	areaMinZoom   = 10
	areaMaxZoom   = 14
	areaMaxTiles  = 20000 // hard safety stop, ~300 MB worst case
)

// OfflineArea is Library/maps/area.json - the receipt for a saved area.
type OfflineArea struct {
	HomeLat   float64 `json:"home_lat"`
	HomeLon   float64 `json:"home_lon"`
	HalfMiles float64 `json:"half_miles"`
	MinZoom   int     `json:"min_zoom"`
	MaxZoom   int     `json:"max_zoom"`
	Tiles     int     `json:"tiles"` // tiles present when the save finished
	Bytes     int64   `json:"bytes"` // tile cache size at save time
	SavedAt   string  `json:"saved_at"`
}

func areaFile(root string) string { return filepath.Join(root, "maps", "area.json") }

func LoadOfflineArea(root string) (OfflineArea, bool) {
	raw, err := os.ReadFile(areaFile(root))
	if err != nil {
		return OfflineArea{}, false
	}
	var a OfflineArea
	if err := json.Unmarshal(raw, &a); err != nil || a.Tiles == 0 {
		return OfflineArea{}, false
	}
	return a, true
}

func saveOfflineArea(root string, a OfflineArea) error {
	if err := os.MkdirAll(filepath.Dir(areaFile(root)), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(areaFile(root), raw, 0o644)
}

func DeleteOfflineArea(root string) error {
	err := os.Remove(areaFile(root))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// areaBounds converts a centre + half-size in miles to a lat/lon box.
func areaBounds(homeLat, homeLon, halfMiles float64) (minLat, minLon, maxLat, maxLon float64) {
	dLat := halfMiles / 69.0
	cosLat := math.Cos(homeLat * math.Pi / 180)
	if math.Abs(cosLat) < 0.05 {
		cosLat = 0.05
	}
	dLon := halfMiles / (69.172 * cosLat)
	minLat = math.Max(homeLat-dLat, -85)
	maxLat = math.Min(homeLat+dLat, 85)
	minLon = math.Max(homeLon-dLon, -180)
	maxLon = math.Min(homeLon+dLon, 180)
	return minLat, minLon, maxLat, maxLon
}

func tileRangeFor(minLat, minLon, maxLat, maxLon float64, z int) (x0, y0, x1, y1 int) {
	x0 = int(lonToWorldX(minLon, z)) / tilePx
	x1 = int(lonToWorldX(maxLon, z)) / tilePx
	y0 = int(latToWorldY(maxLat, z)) / tilePx
	y1 = int(latToWorldY(minLat, z)) / tilePx
	return x0, y0, x1, y1
}

// AreaTilePlan counts the tiles a save would need, per zoom.
func AreaTilePlan(homeLat, homeLon float64) (total int, perZoom map[int]int) {
	return AreaTilePlanR(homeLat, homeLon, areaHalfMiles)
}

// AreaTilePlanR is AreaTilePlan for a chosen half-size in miles
// (v1.8.12: the user picks a 20x20, 50x50 or 100x100-mile square).
func AreaTilePlanR(homeLat, homeLon, halfMiles float64) (total int, perZoom map[int]int) {
	minLat, minLon, maxLat, maxLon := areaBounds(homeLat, homeLon, halfMiles)
	perZoom = map[int]int{}
	for z := areaMinZoom; z <= areaMaxZoom; z++ {
		x0, y0, x1, y1 := tileRangeFor(minLat, minLon, maxLat, maxLon, z)
		n := (x1 - x0 + 1) * (y1 - y0 + 1)
		perZoom[z] = n
		total += n
	}
	return total, perZoom
}

// Covers reports whether a drive's bounds sit inside the saved square.
func (a OfflineArea) Covers(minLat, minLon, maxLat, maxLon float64) bool {
	aMinLat, aMinLon, aMaxLat, aMaxLon := areaBounds(a.HomeLat, a.HomeLon, a.HalfMiles)
	return minLat >= aMinLat && maxLat <= aMaxLat && minLon >= aMinLon && maxLon <= aMaxLon
}

// --- resumable, cancellable area download -------------------------------

type areaProgress struct {
	mu       sync.Mutex
	running  bool
	done     int
	total    int
	fetched  int
	failed   int
	finished bool
	msg      string
	cancel   atomic.Bool
}

func newAreaProgress() *areaProgress { return &areaProgress{} }

func (p *areaProgress) Snapshot() (running bool, done, total, fetched, failed int, msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running, p.done, p.total, p.fetched, p.failed, p.msg
}

func (p *areaProgress) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

func (p *areaProgress) Cancel() { p.cancel.Store(true) }

// Start downloads every missing tile in the plan in the background.
// Tiles already cached are counted without any network traffic, so
// re-running after a cancel or a crash simply resumes.
func (p *areaProgress) Start(root string, homeLat, homeLon float64, token string, onDone func()) {
	p.StartR(root, homeLat, homeLon, token, areaHalfMiles, onDone)
}

// StartR is Start for a chosen half-size in miles (v1.8.12).
func (p *areaProgress) StartR(root string, homeLat, homeLon float64, token string, halfMiles float64, onDone func()) {
	total, _ := AreaTilePlanR(homeLat, homeLon, halfMiles)
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	if total > areaMaxTiles {
		p.msg = fmt.Sprintf("That area needs %d map squares - more than the safety limit. Move home or ask for a smaller area.", total)
		p.mu.Unlock()
		return
	}
	p.running, p.finished = true, false
	p.done, p.total, p.fetched, p.failed = 0, total, 0, 0
	p.msg = "Saving home map…"
	p.cancel.Store(false)
	p.mu.Unlock()

	go func() {
		defer func() {
			p.mu.Lock()
			p.running = false
			p.finished = true
			p.mu.Unlock()
			if onDone != nil {
				onDone()
			}
		}()
		minLat, minLon, maxLat, maxLon := areaBounds(homeLat, homeLon, halfMiles)
		type job struct{ z, x, y int }
		jobs := make(chan job, 64)
		var wg sync.WaitGroup
		for w := 0; w < 3; w++ { // polite: OSM tiles are a shared free resource
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range jobs {
					if p.cancel.Load() {
						continue
					}
					fetched := false
					if _, ok := loadCachedTile(root, j.z, j.x, j.y); !ok {
						if _, err := fetchTile(root, j.z, j.x, j.y); err != nil {
							p.mu.Lock()
							p.failed++
							p.mu.Unlock()
						} else {
							fetched = true
						}
					}
					p.mu.Lock()
					p.done++
					if fetched {
						p.fetched++
					}
					p.mu.Unlock()
				}
			}()
		}
	feed:
		for z := areaMinZoom; z <= areaMaxZoom; z++ {
			x0, y0, x1, y1 := tileRangeFor(minLat, minLon, maxLat, maxLon, z)
			for ty := y0; ty <= y1; ty++ {
				for tx := x0; tx <= x1; tx++ {
					if p.cancel.Load() {
						break feed
					}
					jobs <- job{z, tx, ty}
				}
			}
		}
		close(jobs)
		wg.Wait()

		p.mu.Lock()
		if p.cancel.Load() {
			p.msg = fmt.Sprintf("Home map save stopped at %d of %d squares - run it again any time to resume where it left off.", p.done, p.total)
			p.mu.Unlock()
			return
		}
		done, total, failed := p.done, p.total, p.failed
		p.mu.Unlock()

		present := 0
		for z := areaMinZoom; z <= areaMaxZoom; z++ {
			x0, y0, x1, y1 := tileRangeFor(minLat, minLon, maxLat, maxLon, z)
			for ty := y0; ty <= y1; ty++ {
				for tx := x0; tx <= x1; tx++ {
					if _, ok := loadCachedTile(root, z, tx, ty); ok {
						present++
					}
				}
			}
		}
		a := OfflineArea{
			HomeLat: homeLat, HomeLon: homeLon, HalfMiles: halfMiles,
			MinZoom: areaMinZoom, MaxZoom: areaMaxZoom,
			Tiles: present, Bytes: TileCacheBytes(root),
			SavedAt: time.Now().Format("2006-01-02 15:04"),
		}
		if err := saveOfflineArea(root, a); err != nil {
			p.mu.Lock()
			p.msg = "Map squares downloaded but the receipt could not be saved: " + err.Error()
			p.mu.Unlock()
			return
		}
		msg := ""
		if failed > 0 {
			msg = fmt.Sprintf("Home map saved: %d of %d squares (%d could not be fetched - check your internet and re-run to fill gaps).", present, total, failed)
		} else {
			msg = fmt.Sprintf("Home map saved: %d squares around home. Wardrives there now work with no internet at all.", done)
		}
		// Keep the standard library in step: refresh the portable,
		// signed pack so any other machine can go fully offline too.
		packDest := filepath.Join(mapPacksDir(root), "AEGIS-MAP-home.aegismap")
		if n, err := ExportMapPack(root, packDest, token); err == nil && n > 0 {
			markPackImported(root, packDest, "auto-export after area save", true)
			msg += fmt.Sprintf(" Standard pack refreshed (%s, %d squares) - copy that one file to another computer and it needs no internet for maps.", filepath.Base(packDest), n)
		}
		p.mu.Lock()
		p.msg = msg
		p.mu.Unlock()
	}()
}

// buildTileSetCached composites a street map purely from the disk
// cache - it never touches the network. Missing squares stay
// transparent and the result is marked Partial.
func buildTileSetCached(root string, d MapData, zoomDelta int, area OfflineArea) *tileSet {
	return buildTileSetCachedFB(root, d, zoomDelta, area, 0, 0, false)
}

func buildTileSetCachedFB(root string, d MapData, zoomDelta int, area OfflineArea, fbLat, fbLon float64, fbOK bool) *tileSet {
	return buildTileSetCachedFBR(root, d, zoomDelta, area, fbLat, fbLon, fbOK, areaHalfMiles)
}

func buildTileSetCachedFBR(root string, d MapData, zoomDelta int, area OfflineArea, fbLat, fbLon float64, fbOK bool, halfMiles float64) *tileSet {
	minLat, minLon, maxLat, maxLon := viewBoundsR(d, fbLat, fbLon, fbOK, halfMiles)
	if !finite4(minLat, minLon, maxLat, maxLon) {
		return &tileSet{Err: true}
	}
	padLat := math.Max((maxLat-minLat)*0.12, 0.0015)
	padLon := math.Max((maxLon-minLon)*0.12, 0.0015)
	minLat, maxLat = minLat-padLat, maxLat+padLat
	minLon, maxLon = minLon-padLon, maxLon+padLon
	z := pickZoom(minLat, minLon, maxLat, maxLon, 1150, 380) + zoomDelta
	if z < area.MinZoom {
		z = area.MinZoom
	}
	if z > area.MaxZoom {
		z = area.MaxZoom
	}
	var x0, y0, x1, y1 int
	for {
		x0, y0, x1, y1 = tileRangeFor(minLat, minLon, maxLat, maxLon, z)
		if (x1-x0+1)*(y1-y0+1) <= 96 || z <= 3 {
			break
		}
		z--
	}
	comp := image.NewNRGBA(image.Rect(0, 0, (x1-x0+1)*tilePx, (y1-y0+1)*tilePx))
	okCount, totalTiles := 0, (x1-x0+1)*(y1-y0+1)
	for ty := y0; ty <= y1; ty++ {
		for tx := x0; tx <= x1; tx++ {
			img, ok := loadCachedTile(root, z, tx, ty)
			if !ok {
				continue
			}
			draw.Draw(comp, image.Rect((tx-x0)*tilePx, (ty-y0)*tilePx,
				(tx-x0+1)*tilePx, (ty-y0+1)*tilePx), img, image.Point{}, draw.Src)
			okCount++
		}
	}
	if okCount == 0 {
		return &tileSet{Err: true, Zoom: z}
	}
	return &tileSet{
		Img: comp, Zoom: z, Partial: okCount < totalTiles,
		OriginX: float64(x0 * tilePx), OriginY: float64(y0 * tilePx),
	}
}
