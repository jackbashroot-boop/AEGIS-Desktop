package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Core wires the pieces together; GUI and headless modes share it.
type Core struct {
	Cfg    *Config
	Log    *Log
	Live   *LiveRegistry
	Store  *Store
	Server *Server
	Disco  *Discovery

	healthMu  sync.Mutex
	lastProbe time.Time
	probeOK   bool
	probed    bool
}

func NewCore() (*Core, error) {
	cfg, err := loadOrCreateConfig()
	if err != nil {
		return nil, err
	}
	log := NewLog()
	store := NewStore(cfg.Library, log)
	live := NewLiveRegistry()
	server := NewServer(cfg, log, live, store)
	disco := NewDiscovery(cfg.Token, log)
	// Standard map library: adopt any new signature-verified pack a
	// user dropped into Library/maps/packs - in the background, so a
	// big pack never delays the window opening.
	go autoImportMapPacks(cfg.Library, cfg.Token, log)
	return &Core{Cfg: cfg, Log: log, Live: live, Store: store, Server: server, Disco: disco}, nil
}

func main() {
	headless := flag.Bool("headless", false, "run the sync/live server with no window (terminal mode)")
	selftest := flag.Bool("selftest", false, "run the built-in protocol self-test against a scratch library and exit")
	initFlag := flag.Bool("init", false, "print the /AEGIS_LINK/desktop.txt contents for your SD cards and exit")
	importDir := flag.String("import", "", "import known log files from a mounted SD card folder, rebuild reports, exit")
	version := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *version {
		fmt.Println("AEGIS Companion", appVersion, "(protocol AEGIS/1)")
		return
	}

	core, err := NewCore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "startup error:", err)
		os.Exit(1)
	}

	if *initFlag {
		fmt.Println("Create this file on each Cardputer SD card as /AEGIS_LINK/desktop.txt :")
		fmt.Println()
		fmt.Print(core.Cfg.desktopTxt(preferredLANIP()))
		fmt.Println()
		fmt.Println("Config:", configPath())
		fmt.Println("Library:", core.Cfg.Library)
		return
	}
	if *importDir != "" {
		n, err := core.Store.ImportFrom(*importDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "import error:", err)
			os.Exit(1)
		}
		fmt.Printf("imported %d file(s); library at %s\n", n, core.Cfg.Library)
		return
	}
	if *selftest {
		if !runSelfTest(core) {
			os.Exit(1)
		}
		return
	}

	// Single instance (v1.7.3): a second long-lived copy used to
	// start happily next to the first (its listener failed, its GUI
	// opened anyway), and the zombie then re-created files after an
	// uninstall - making uninstall look broken and "close" look
	// like minimize. One lock listener on loopback ends that.
	if ln, err := net.Listen("tcp", "127.0.0.1:8792"); err != nil {
		fmt.Fprintln(os.Stderr, "AEGIS Companion is already running - close the other copy first (check your taskbar/dock).")
		os.Exit(1)
	} else {
		defer ln.Close()
		_ = ln
	}

	core.Store.Rebuild()
	if err := core.Server.Start(core.Cfg.Port); err != nil {
		core.Log.Errorf("cannot listen on port %d: %v", core.Cfg.Port, err)
		if !*headless {
			// GUI still opens so the user can fix the port in Settings.
			core.Log.Warnf("opening window anyway - change the port in Settings")
		} else {
			os.Exit(1)
		}
	}

	if err := core.Disco.Start(core.Cfg.Port); err != nil {
		core.Log.Warnf("discovery responder could not start on udp :%d: %v (devices will use the desktop.txt address)", core.Cfg.Port, err)
	}
	core.selfCheck()
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go core.linkWatchdog(stopWatch)

	if *headless {
		fmt.Println("AEGIS Companion headless - Ctrl-C to stop. Library:", core.Cfg.Library)
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		core.Server.Stop()
		return
	}

	runGUI(core)
}

// libraryIndexURL is the file:// URL of the browseable library index.
func (c *Core) libraryIndexURL() string {
	p := filepath.Join(c.Cfg.Library, "index.html")
	return "file://" + p
}

// RestartListener restarts the TCP listener and the UDP discovery
// responder together (Settings port changes go through here).
func (c *Core) RestartListener(port int) error {
	if err := c.Server.Restart(port); err != nil {
		return err
	}
	if err := c.Disco.Start(port); err != nil {
		c.Log.Warnf("discovery responder could not restart on udp :%d: %v", port, err)
	}
	return nil
}
