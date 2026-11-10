package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Real street-map tiles for the Wardrive Map page (OpenStreetMap
// "slippy map" tiles, Web Mercator). Tiles are fetched once, cached
// under Library/maps/tiles, and composited into a single image the
// plot draws under the same route/pins as the offline schematic.
// Nothing about the user's data leaves the PC: the tile server only
// ever sees which map squares were viewed, like any map website.

const tilePx = 256

func lonToWorldX(lon float64, z int) float64 {
	n := math.Ldexp(1, z)
	return (lon + 180) / 360 * n * tilePx
}

func latToWorldY(lat float64, z int) float64 {
	lat = math.Max(-85.05112878, math.Min(85.05112878, lat))
	n := math.Ldexp(1, z)
	s := math.Sin(lat * math.Pi / 180)
	return (0.5 - math.Log((1+s)/(1-s))/(4*math.Pi)) * n * tilePx
}

// pickZoom chooses the deepest zoom whose bounds still fit the target
// pixel box, clamped to sane wardrive levels.
func pickZoom(minLat, minLon, maxLat, maxLon float64, boxW, boxH int) int {
	for z := 17; z > 3; z-- {
		w := lonToWorldX(maxLon, z) - lonToWorldX(minLon, z)
		h := latToWorldY(minLat, z) - latToWorldY(maxLat, z)
		if w <= float64(boxW) && h <= float64(boxH) {
			return z
		}
	}
	return 3
}

type tileSet struct {
	Key     string
	Img     *image.NRGBA
	OriginX float64 // world-pixel coords of the composite's top-left
	OriginY float64
	Zoom    int
	Err     bool
	Loading bool
	Partial bool // some squares were missing (saved-map mode)
}

var tileHTTP = &http.Client{Timeout: 7 * time.Second}

// loadCachedTile reads one square from the disk cache only - no
// network. This is what saved-map mode and download-resume checks use.
func loadCachedTile(root string, z, x, y int) (image.Image, bool) {
	cache := filepath.Join(root, "maps", "tiles", fmt.Sprint(z), fmt.Sprint(x), fmt.Sprintf("%d.png", y))
	raw, err := os.ReadFile(cache)
	if err != nil {
		return nil, false
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}
	return img, true
}

func fetchTile(root string, z, x, y int) (image.Image, error) {
	cache := filepath.Join(root, "maps", "tiles", fmt.Sprint(z), fmt.Sprint(x), fmt.Sprintf("%d.png", y))
	if img, ok := loadCachedTile(root, z, x, y); ok {
		return img, nil
	}
	url := fmt.Sprintf("https://tile.openstreetmap.org/%d/%d/%d.png", z, x, y)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AEGIS-Companion/"+appVersion+" (personal wardrive map; contact: local user)")
	resp, err := tileHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tile server: %s", resp.Status)
	}
	img, err := png.Decode(resp.Body)
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(filepath.Dir(cache), 0o755)
	if f, err := os.Create(cache); err == nil {
		_ = png.Encode(f, img)
		_ = f.Close()
	}
	return img, nil
}

// buildTileSet composites every tile covering the (padded) bounds.
// Blocking - call from a goroutine.
// finite4 reports whether four bounds are all real numbers - crafted
// or corrupt map data must never send the tile builders chasing an
// infinite world (defense in depth; the parsers already validate).
func finite4(a, b, c, dd float64) bool {
	for _, v := range []float64{a, b, c, dd} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// viewBounds returns the extent the map should frame: the data's own
// bounds when there is any, otherwise the full 100x100-mile home-map
// square around the fallback centre (v1.8.5 - an empty drive used to
// leave the map blank and depressing; the map is the map, sightings
// just decorate it). fbOK=false means no fallback is known.
func viewBounds(d MapData, fbLat, fbLon float64, fbOK bool) (minLat, minLon, maxLat, maxLon float64) {
	minLat, minLon, maxLat, maxLon = mapBounds(d)
	if finite4(minLat, minLon, maxLat, maxLon) {
		return minLat, minLon, maxLat, maxLon
	}
	if fbOK {
		return areaBounds(fbLat, fbLon, areaHalfMiles)
	}
	return minLat, minLon, maxLat, maxLon
}

func buildTileSet(root string, d MapData, zoomDelta int) *tileSet {
	return buildTileSetFB(root, d, zoomDelta, 0, 0, false)
}

func buildTileSetFB(root string, d MapData, zoomDelta int, fbLat, fbLon float64, fbOK bool) *tileSet {
	minLat, minLon, maxLat, maxLon := viewBounds(d, fbLat, fbLon, fbOK)
	if !finite4(minLat, minLon, maxLat, maxLon) {
		return &tileSet{Err: true}
	}
	padLat := math.Max((maxLat-minLat)*0.12, 0.0015)
	padLon := math.Max((maxLon-minLon)*0.12, 0.0015)
	minLat, maxLat = minLat-padLat, maxLat+padLat
	minLon, maxLon = minLon-padLon, maxLon+padLon
	z := pickZoom(minLat, minLon, maxLat, maxLon, 1150, 380) + zoomDelta
	if z < 3 {
		z = 3
	}
	if z > 18 {
		z = 18
	}
	x0 := int(lonToWorldX(minLon, z)) / tilePx
	x1 := int(lonToWorldX(maxLon, z)) / tilePx
	y0 := int(latToWorldY(maxLat, z)) / tilePx
	y1 := int(latToWorldY(minLat, z)) / tilePx
	if (x1-x0+1)*(y1-y0+1) > 28 {
		return &tileSet{Err: true, Zoom: z}
	}
	comp := image.NewNRGBA(image.Rect(0, 0, (x1-x0+1)*tilePx, (y1-y0+1)*tilePx))
	type job struct{ tx, ty int }
	jobs := make(chan job, 32)
	var wg sync.WaitGroup
	var mu sync.Mutex
	okCount := 0
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				img, err := fetchTile(root, z, j.tx, j.ty)
				if err != nil {
					continue
				}
				mu.Lock()
				draw.Draw(comp, image.Rect((j.tx-x0)*tilePx, (j.ty-y0)*tilePx,
					(j.tx-x0+1)*tilePx, (j.ty-y0+1)*tilePx), img, image.Point{}, draw.Src)
				okCount++
				mu.Unlock()
			}
		}()
	}
	for ty := y0; ty <= y1; ty++ {
		for tx := x0; tx <= x1; tx++ {
			jobs <- job{tx, ty}
		}
	}
	close(jobs)
	wg.Wait()
	if okCount == 0 {
		return &tileSet{Err: true, Zoom: z}
	}
	return &tileSet{
		Img: comp, Zoom: z,
		OriginX: float64(x0 * tilePx), OriginY: float64(y0 * tilePx),
	}
}

// mapBounds returns the lat/lon extent of points+track (valid, non-empty
// when called - the page checks first).
func mapBounds(d MapData) (minLat, minLon, maxLat, maxLon float64) {
	minLat, minLon = math.Inf(1), math.Inf(1)
	maxLat, maxLon = math.Inf(-1), math.Inf(-1)
	grow := func(lat, lon float64) {
		minLat, maxLat = math.Min(minLat, lat), math.Max(maxLat, lat)
		minLon, maxLon = math.Min(minLon, lon), math.Max(maxLon, lon)
	}
	for _, p := range d.Points {
		grow(p.Lat, p.Lon)
	}
	for _, tp := range d.Track {
		grow(tp[0], tp[1])
	}
	return minLat, minLon, maxLat, maxLon
}

// TileCacheBytes reports the on-disk tile cache size for Settings.
func TileCacheBytes(root string) int64 {
	var total int64
	_ = filepath.Walk(filepath.Join(root, "maps", "tiles"), func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func ClearTileCache(root string) error {
	return os.RemoveAll(filepath.Join(root, "maps", "tiles"))
}
