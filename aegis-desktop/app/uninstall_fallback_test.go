package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// The built-in uninstall fallback must at least launch its
// detached removal script (v1.7.8). HOME is faked; the script's
// rm target is this test binary's own disposable build dir.
func TestSelfUninstallLaunches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := selfUninstall(); err != nil {
		t.Fatalf("selfUninstall: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ents, _ := os.ReadDir(os.TempDir())
		found := false
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), "aegis-uninstall-") {
				found = true
			}
		}
		if found {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("removal script not created in temp dir")
}
