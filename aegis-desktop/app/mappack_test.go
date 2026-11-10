package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

const packTestToken = "testtoken0123456789abcdef"

func seedTiles(t *testing.T, root string, n int) {
	t.Helper()
	pngFile := filepath.Join(root, "seed.png")
	pf, err := os.Create(pngFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(pf, image.NewNRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	_ = pf.Close()
	img, err := os.ReadFile(pngFile)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(pngFile)
	dir := filepath.Join(root, "maps", "tiles", "14", "4911")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		name := filepath.Join(dir, fmt.Sprintf("%d.png", 6100+i))
		if err := os.WriteFile(name, img, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func exportTestPack(t *testing.T, src, token string) string {
	t.Helper()
	dest := filepath.Join(src, "maps", "packs", "AEGIS-MAP-test.aegismap")
	if _, err := ExportMapPack(src, dest, token); err != nil {
		t.Fatal(err)
	}
	return dest
}

func copyPackTo(t *testing.T, pack, dstRoot string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dstRoot, "maps", "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(pack)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dstRoot, "maps", "packs", "AEGIS-MAP-test.aegismap")
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestMapPackSignedRoundTrip(t *testing.T) {
	src := t.TempDir()
	seedTiles(t, src, 5)
	a := OfflineArea{HomeLat: 41.5247, HomeLon: -72.0759, HalfMiles: 50, MinZoom: 10, MaxZoom: 14, Tiles: 10655}
	if err := saveOfflineArea(src, a); err != nil {
		t.Fatal(err)
	}
	pack := exportTestPack(t, src, packTestToken)

	dst := t.TempDir()
	packs := ListMapPacks(dst)
	if len(packs) != 0 {
		t.Fatalf("fresh root should list no packs, got %v", packs)
	}
	dstPack := copyPackTo(t, pack, dst)
	tiles, skipped, state, err := ImportMapPack(dst, dstPack, packTestToken)
	if err != nil {
		t.Fatal(err)
	}
	if tiles != 5 {
		t.Fatalf("imported %d tiles (skipped %d), want 5", tiles, skipped)
	}
	if state != PackVerified {
		t.Fatal("same-token import must verify the signature")
	}
	if _, ok := LoadOfflineArea(dst); !ok {
		t.Fatal("area receipt not restored")
	}
	if _, ok := loadCachedTile(dst, 14, 4911, 6100); !ok {
		t.Fatal("imported tile not readable from cache")
	}
}

func TestMapPackWrongTokenRefused(t *testing.T) {
	src := t.TempDir()
	seedTiles(t, src, 3)
	pack := exportTestPack(t, src, packTestToken)

	dst := t.TempDir()
	dstPack := copyPackTo(t, pack, dst)
	if _, _, _, err := ImportMapPack(dst, dstPack, "adifferenttoken0000000000"); err == nil {
		t.Fatal("pack signed by another token must be refused")
	}
	if _, ok := loadCachedTile(dst, 14, 4911, 6100); ok {
		t.Fatal("refused pack must not leak tiles into the cache")
	}
}

func TestMapPackTamperedRefused(t *testing.T) {
	src := t.TempDir()
	seedTiles(t, src, 3)
	pack := exportTestPack(t, src, packTestToken)

	// rewrite the zip with one tile's bytes flipped
	raw, err := os.ReadFile(pack)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	evil := filepath.Join(t.TempDir(), "evil.aegismap")
	f, err := os.Create(evil)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, entry := range zr.File {
		rc, _ := entry.Open()
		data := make([]byte, entry.UncompressedSize64)
		n, _ := rc.Read(data)
		data = data[:n]
		_ = rc.Close()
		if entry.Name == "tiles/14/4911/6100.png" && len(data) > 20 {
			data[20] ^= 0xFF
		}
		w, _ := zw.Create(entry.Name)
		_, _ = w.Write(data)
	}
	_ = zw.Close()
	_ = f.Close()

	dst := t.TempDir()
	if _, _, _, err := ImportMapPack(dst, evil, packTestToken); err == nil {
		t.Fatal("tampered pack must be refused")
	}
	if _, ok := loadCachedTile(dst, 14, 4911, 6100); ok {
		t.Fatal("tampered pack leaked tiles into the cache")
	}
}

func TestMapPackUnsignedLegacy(t *testing.T) {
	// a hand-made pack with no manifest/signature: imports, unsigned
	dst := t.TempDir()
	pack := filepath.Join(dst, "legacy.aegismap")
	f, err := os.Create(pack)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	// one real PNG tile
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 4, 4)))
	w, _ := zw.Create("tiles/14/4911/6100.png")
	_, _ = w.Write(buf.Bytes())
	_ = zw.Close()
	_ = f.Close()
	tiles, _, state, err := ImportMapPack(dst, pack, packTestToken)
	if err != nil {
		t.Fatal(err)
	}
	if tiles != 1 || state != PackUnsigned {
		t.Fatalf("legacy pack: tiles=%d state=%v, want 1/unsigned", tiles, state)
	}
	if _, ok := loadCachedTile(dst, 14, 4911, 6100); !ok {
		t.Fatal("legacy tile not imported")
	}
}

func TestImportMapPackRejectsTraversal(t *testing.T) {
	dst := t.TempDir()
	pack := filepath.Join(dst, "evil.aegismap")
	f, err := os.Create(pack)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 4, 4)))
	for _, name := range []string{"tiles/../../evil.png", "../evil.png", "tiles/14/1/2.png.exe", "/abs.png", "tiles/14/4911/6100.png"} {
		w, _ := zw.Create(name)
		_, _ = w.Write(buf.Bytes())
	}
	_ = zw.Close()
	_ = f.Close()
	tiles, skipped, _, err := ImportMapPack(dst, pack, packTestToken)
	if err != nil {
		t.Fatal(err)
	}
	if tiles != 1 || skipped != 4 {
		t.Fatalf("traversal pack: tiles=%d skipped=%d, want 1/4", tiles, skipped)
	}
	if _, err := os.Stat(filepath.Join(dst, "evil.png")); !os.IsNotExist(err) {
		t.Fatal("traversal entry escaped the cache")
	}
	bad := filepath.Join(dst, "junk.aegismap")
	_ = os.WriteFile(bad, []byte("not a zip at all"), 0o644)
	if _, _, _, err := ImportMapPack(dst, bad, packTestToken); err == nil {
		t.Fatal("junk pack must error")
	}
}

func TestAutoImportOnlyVerified(t *testing.T) {
	// signed pack auto-imports; unsigned one is left alone
	root := t.TempDir()
	src := t.TempDir()
	seedTiles(t, src, 2)
	pack := exportTestPack(t, src, packTestToken)
	copyPackTo(t, pack, root)

	log := NewLog()
	autoImportMapPacks(root, packTestToken, log)
	if _, ok := loadCachedTile(root, 14, 4911, 6100); !ok {
		t.Fatal("verified pack should have auto-imported")
	}
	// second run is a no-op (marks) and must not error or duplicate
	autoImportMapPacks(root, packTestToken, log)

	root2 := t.TempDir()
	legacy := filepath.Join(root2, "maps", "packs", "legacy.aegismap")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Create(legacy)
	zw := zip.NewWriter(f)
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 4, 4)))
	w, _ := zw.Create("tiles/14/4911/6100.png")
	_, _ = w.Write(buf.Bytes())
	_ = zw.Close()
	_ = f.Close()
	autoImportMapPacks(root2, packTestToken, log)
	if _, ok := loadCachedTile(root2, 14, 4911, 6100); ok {
		t.Fatal("unsigned pack must NOT auto-import")
	}
}

func FuzzImportMapPack(f *testing.F) {
	f.Add([]byte("PK\x03\x04junk"))
	f.Add([]byte(""))
	f.Add([]byte("not a zip"))
	dir := f.TempDir()
	p := filepath.Join(dir, "x.aegismap")
	f.Fuzz(func(t *testing.T, b []byte) {
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		// must never panic, never verify garbage, never write outside maps/
		_, _, _, _ = ImportMapPack(dir, p, packTestToken)
	})
}
