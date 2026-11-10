package main

// System info (v1.8.9): a concise, live picture of the PC this
// Companion runs on - CPU, GPU, RAM, disk, temperatures and usage -
// gathered with the standard library only (no new dependencies).
// Linux reads /proc + /sys directly. Windows uses one cached
// PowerShell CIM query (CPU load, memory, GPU) refreshed at most
// every 15 s, plus the registry environment for the CPU name;
// Windows does not expose CPU temperatures without third-party
// drivers, and the page says so instead of faking a number.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type sysSnapshot struct {
	OS        string
	Host      string
	CPUModel  string
	Cores     int
	CPUPct    float64 // -1 = unknown
	RAMTotal  uint64
	RAMUsed   uint64
	GPU       string
	Temps     []string // "CPU 62°C" lines
	DiskPath  string
	DiskTotal uint64
	DiskFree  uint64
	Uptime    string
	At        time.Time
}

var (
	gSysMu   sync.Mutex
	gSys     sysSnapshot
	gSysPrev struct{ total, idle uint64 }
	gSysOnce sync.Once
)

func startSysInfo() {
	gSysOnce.Do(func() { go sysLoop() })
}

func sysLoop() {
	for {
		s := gatherSys()
		gSysMu.Lock()
		gSys = s
		gSysMu.Unlock()
		time.Sleep(4 * time.Second)
	}
}

func sysSnapshotNow() sysSnapshot {
	gSysMu.Lock()
	defer gSysMu.Unlock()
	return gSys
}

func gatherSys() sysSnapshot {
	s := sysSnapshot{OS: runtime.GOOS + "/" + runtime.GOARCH, Cores: runtime.NumCPU(), CPUPct: -1, At: time.Now()}
	s.Host, _ = os.Hostname()
	if runtime.GOOS == "linux" {
		gatherLinux(&s)
	} else if runtime.GOOS == "windows" {
		gatherWindows(&s)
	}
	return s
}

func readFileTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func gatherLinux(s *sysSnapshot) {
	// CPU model
	for _, ln := range strings.Split(readFileTrim("/proc/cpuinfo"), "\n") {
		if strings.HasPrefix(ln, "model name") {
			if i := strings.Index(ln, ":"); i >= 0 {
				s.CPUModel = strings.TrimSpace(ln[i+1:])
			}
			break
		}
	}
	// CPU usage from /proc/stat deltas
	if ln := strings.SplitN(readFileTrim("/proc/stat"), "\n", 2)[0]; strings.HasPrefix(ln, "cpu ") {
		f := strings.Fields(ln)[1:]
		var tot, idle uint64
		for i, v := range f {
			n, _ := strconv.ParseUint(v, 10, 64)
			tot += n
			if i == 3 || i == 4 {
				idle += n
			}
		}
		gSysMu.Lock()
		if gSysPrev.total > 0 && tot > gSysPrev.total {
			dt := tot - gSysPrev.total
			di := idle - gSysPrev.idle
			if dt > 0 {
				s.CPUPct = 100 * float64(dt-di) / float64(dt)
			}
		}
		gSysPrev.total, gSysPrev.idle = tot, idle
		gSysMu.Unlock()
	}
	// RAM
	var total, avail uint64
	for _, ln := range strings.Split(readFileTrim("/proc/meminfo"), "\n") {
		f := strings.Fields(ln)
		if len(f) >= 2 {
			n, _ := strconv.ParseUint(f[1], 10, 64)
			switch f[0] {
			case "MemTotal:":
				total = n * 1024
			case "MemAvailable:":
				avail = n * 1024
			}
		}
	}
	s.RAMTotal, s.RAMUsed = total, total-avail
	// Uptime
	if f := strings.Fields(readFileTrim("/proc/uptime")); len(f) > 0 {
		if sec, err := strconv.ParseFloat(f[0], 64); err == nil {
			s.Uptime = fmt.Sprintf("%dd %dh %dm", int(sec)/86400, int(sec)/3600%24, int(sec)/60%60)
		}
	}
	// GPU: DRM card vendor/device names are numeric; lspci is friendlier.
	if out, err := exec.Command("lspci").CombinedOutput(); err == nil {
		for _, ln := range strings.Split(string(out), "\n") {
			if strings.Contains(ln, "VGA") || strings.Contains(ln, "3D controller") {
				if i := strings.Index(ln, ": "); i >= 0 {
					s.GPU = strings.TrimSpace(ln[i+2:])
				}
				break
			}
		}
	}
	if s.GPU == "" {
		ents, _ := filepath.Glob("/sys/class/drm/card[0-9]/device")
		if len(ents) > 0 {
			v := readFileTrim(ents[0] + "/vendor")
			s.GPU = "PCI vendor " + v + " (install pciutils for the model name)"
		}
	}
	// Temperatures: hwmon (coretemp/k10temp/...) then thermal zones.
	seen := map[string]bool{}
	hw, _ := filepath.Glob("/sys/class/hwmon/hwmon*")
	for _, h := range hw {
		name := readFileTrim(h + "/name")
		temps, _ := filepath.Glob(h + "/temp*_input")
		for _, t := range temps {
			raw := readFileTrim(t)
			n, err := strconv.Atoi(raw)
			if err != nil || n <= 0 {
				continue
			}
			label := strings.TrimSuffix(filepath.Base(t), "_input")
			lbl := readFileTrim(h + "/" + label + "_label")
			key := name + "/" + lbl
			if seen[key] {
				continue
			}
			seen[key] = true
			nice := name
			if lbl != "" {
				nice += " " + lbl
			}
			s.Temps = append(s.Temps, fmt.Sprintf("%s %.0f°C", nice, float64(n)/1000))
			if len(s.Temps) >= 6 {
				break
			}
		}
	}
	if len(s.Temps) == 0 {
		zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
		for _, z := range zones {
			raw := readFileTrim(z + "/temp")
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				s.Temps = append(s.Temps, fmt.Sprintf("%s %.0f°C", readFileTrim(z+"/type"), float64(n)/1000))
			}
			if len(s.Temps) >= 4 {
				break
			}
		}
	}
	// Disk where the config/library live
	s.DiskPath = exeDir()
	s.DiskTotal, s.DiskFree = diskUsage(s.DiskPath)
}

var gWinCache struct {
	at   time.Time
	text string
}

func gatherWindows(s *sysSnapshot) {
	s.CPUModel = os.Getenv("PROCESSOR_IDENTIFIER")
	// One cached CIM query for load + memory + GPU (15 s cache).
	if time.Since(gWinCache.at) > 15*time.Second {
		const ps = `$cpu=(Get-CimInstance Win32_Processor);$os=Get-CimInstance Win32_OperatingSystem;$gpu=(Get-CimInstance Win32_VideoController | Select-Object -First 1).Name;"LOAD=$($cpu.LoadPercentage)";"TOT=$($os.TotalVisibleMemorySize)";"FREE=$($os.FreePhysicalMemory)";"GPU=$gpu"`
		if out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).CombinedOutput(); err == nil {
			gWinCache.at, gWinCache.text = time.Now(), string(out)
		}
	}
	for _, ln := range strings.Split(gWinCache.text, "\n") {
		ln = strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(ln, "LOAD="):
			if n, err := strconv.ParseFloat(strings.TrimPrefix(ln, "LOAD="), 64); err == nil {
				s.CPUPct = n
			}
		case strings.HasPrefix(ln, "TOT="):
			n, _ := strconv.ParseUint(strings.TrimPrefix(ln, "TOT="), 10, 64)
			s.RAMTotal = n * 1024
		case strings.HasPrefix(ln, "FREE="):
			n, _ := strconv.ParseUint(strings.TrimPrefix(ln, "FREE="), 10, 64)
			if s.RAMTotal > 0 {
				s.RAMUsed = s.RAMTotal - n*1024
			}
		case strings.HasPrefix(ln, "GPU="):
			s.GPU = strings.TrimPrefix(ln, "GPU=")
		}
	}
	s.Temps = []string{"CPU temperature: Windows doesn't expose this without extra drivers - left blank on purpose"}
	s.DiskPath = exeDir()
}
