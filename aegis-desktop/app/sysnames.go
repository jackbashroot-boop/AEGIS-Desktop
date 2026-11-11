package main

// Friendly hardware names (v1.8.12, user request): the System page
// used to print raw probe strings verbatim - lspci's
// "Advanced Micro Devices, Inc. [AMD/ATI] Navi 31 [Radeon RX 7900
// XTX/7900 GRE/7900M] (rev c8)" is accurate but unreadable, and
// "12th Gen Intel(R) Core(TM) i5-12600K" is marketing-ese. These
// cleaners turn probe output into the names the parts are actually
// sold under (e.g. "AMD Radeon RX 7900 XTX · Navi 31",
// "Intel Core i5-12600K (12th Gen)").
//
// Everything here is LOCAL string surgery on data the PC already
// collected about itself. Nothing is uploaded, looked up online, or
// shared - the correct-name rules are baked in, per the user's
// "do NOT give that data away".

import (
	"regexp"
	"strings"
)

// cleanTrademark strips legal symbols and collapses whitespace.
func cleanTrademark(s string) string {
	r := strings.NewReplacer("(R)", "", "(TM)", "", "®", "", "™", "",
		"Corporation", "", "Inc.", "", "Inc", "")
	return strings.Join(strings.Fields(r.Replace(s)), " ")
}

var (
	cpuGenPrefix = regexp.MustCompile(`^(\d+(?:st|nd|rd|th) Gen)\s+(.*)$`)
	cpuSpeedTail = regexp.MustCompile(`\s*@\s*[0-9.]+ ?[GM]Hz.*$`)
	cpuCoreTail  = regexp.MustCompile(`\s+\d+-Core Processor$`)
	cpuCPUTail   = regexp.MustCompile(`\s+CPU$`)
)

// friendlyCPU renders a probe CPU string the way it is sold:
// "12th Gen Intel(R) Core(TM) i5-12600K" -> "Intel Core i5-12600K (12th Gen)".
func friendlyCPU(model string) string {
	s := cleanTrademark(model)
	s = cpuSpeedTail.ReplaceAllString(s, "")
	s = cpuCoreTail.ReplaceAllString(s, "")
	s = cpuCPUTail.ReplaceAllString(s, "")
	if m := cpuGenPrefix.FindStringSubmatch(s); m != nil {
		s = m[2] + " (" + m[1] + ")"
	}
	return strings.TrimSpace(s)
}

var bracketGroup = regexp.MustCompile(`\[([^\]]+)\]`)

// gpuModelish reports whether a bracketed lspci segment looks like a
// retail GPU model rather than a vendor tag.
func gpuModelish(s string) bool {
	for _, kw := range []string{"Radeon", "GeForce", "RTX", "GTX", "Quadro", "FirePro", "UHD Graphics", "Iris", "Arc ", "Arc(TM", "HD Graphics", "RX ", "Vega", "Tesla", "Instinct"} {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

// friendlyGPU renders an lspci/CIM GPU string under its retail name:
//
//	"Advanced Micro Devices, Inc. [AMD/ATI] Navi 31 [Radeon RX 7900 XTX/7900 GRE/7900M] (rev c8)"
//	  -> "AMD Radeon RX 7900 XTX · Navi 31"
//	"NVIDIA Corporation GA102 [GeForce RTX 3080] (rev a1)"
//	  -> "NVIDIA GeForce RTX 3080 · GA102"
//	"AMD Radeon RX 7900 XTX" (Windows CIM, already friendly) passes through.
func friendlyGPU(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, " (rev "); i >= 0 {
		s = s[:i]
	}
	vendor := ""
	switch {
	case strings.Contains(s, "Advanced Micro Devices") || strings.Contains(s, "[AMD/ATI]") || strings.HasPrefix(s, "AMD") || strings.Contains(s, "Radeon"):
		vendor = "AMD"
	case strings.Contains(s, "NVIDIA"):
		vendor = "NVIDIA"
	case strings.Contains(s, "Intel"):
		vendor = "Intel"
	}
	groups := bracketGroup.FindAllStringSubmatch(s, -1)
	model := ""
	for _, g := range groups {
		if gpuModelish(g[1]) {
			model = strings.TrimSpace(strings.Split(g[1], "/")[0])
			break
		}
	}
	// Chip name: text outside the brackets, minus vendor words.
	chip := bracketGroup.ReplaceAllString(s, " ")
	chip = cleanTrademark(chip)
	for _, v := range []string{"AMD", "NVIDIA", "Intel", "Advanced Micro Devices", "VGA compatible controller", "3D controller", "Display controller"} {
		chip = strings.ReplaceAll(chip, v, " ")
	}
	chip = strings.TrimSpace(strings.Join(strings.Fields(strings.ReplaceAll(chip, ",", " ")), " "))
	if strings.HasPrefix(chip, "AMD/ATI") {
		chip = strings.TrimSpace(strings.TrimPrefix(chip, "AMD/ATI"))
	}
	if model == "" {
		// No bracketed model: the cleaned string IS the name (CIM).
		out := cleanTrademark(s)
		out = bracketGroup.ReplaceAllString(out, "")
		out = strings.TrimSpace(strings.Join(strings.Fields(out), " "))
		if vendor != "" && !strings.HasPrefix(out, vendor) {
			out = vendor + " " + out
		}
		return out
	}
	out := model
	if vendor != "" && !strings.HasPrefix(out, vendor) {
		out = vendor + " " + out
	}
	if chip != "" && !strings.Contains(out, chip) && len(chip) <= 24 {
		out += " · " + chip
	}
	return out
}

// prettyOSName reads the distribution's own name on Linux
// (/etc/os-release PRETTY_NAME, e.g. "CachyOS Linux"). "" elsewhere.
func prettyOSName() string {
	b, err := readFileBytes("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(ln, "PRETTY_NAME="), "\"")
		}
	}
	return ""
}

// machineName reads the DMI product identity on Linux (e.g. a
// motherboard or laptop model). Vendor placeholder strings are
// treated as unknown - an honest blank beats "To be filled by O.E.M.".
func machineName() string {
	name := readFileTrim("/sys/devices/virtual/dmi/id/product_name")
	ver := readFileTrim("/sys/devices/virtual/dmi/id/product_version")
	for _, junk := range []string{"To be filled by O.E.M.", "Default string", "Not Applicable", "None", "System Product Name", "0000000000000000"} {
		if name == junk {
			name = ""
		}
		if ver == junk {
			ver = ""
		}
	}
	out := strings.TrimSpace(name + " " + ver)
	if len(out) < 3 {
		return ""
	}
	return out
}
