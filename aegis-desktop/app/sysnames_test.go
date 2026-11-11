package main

import "testing"

func TestFriendlyCPU(t *testing.T) {
	cases := map[string]string{
		"12th Gen Intel(R) Core(TM) i5-12600K":     "Intel Core i5-12600K (12th Gen)",
		"Intel(R) Core(TM) i7-9700K CPU @ 3.60GHz": "Intel Core i7-9700K",
		"AMD Ryzen 7 5800X 8-Core Processor":       "AMD Ryzen 7 5800X",
		"13th Gen Intel(R) Core(TM) i9-13900K":     "Intel Core i9-13900K (13th Gen)",
	}
	for in, want := range cases {
		if got := friendlyCPU(in); got != want {
			t.Errorf("friendlyCPU(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFriendlyGPU(t *testing.T) {
	cases := map[string]string{
		"Advanced Micro Devices, Inc. [AMD/ATI] Navi 31 [Radeon RX 7900 XTX/7900 GRE/7900M] (rev c8)": "AMD Radeon RX 7900 XTX · Navi 31",
		"NVIDIA Corporation GA102 [GeForce RTX 3080] (rev a1)":                                        "NVIDIA GeForce RTX 3080 · GA102",
		"AMD Radeon RX 7900 XTX": "AMD Radeon RX 7900 XTX",
		"Intel Corporation Alder Lake-P GT2 [Iris Xe Graphics] (rev 0c)": "Intel Iris Xe Graphics · Alder Lake-P GT2",
	}
	for in, want := range cases {
		if got := friendlyGPU(in); got != want {
			t.Errorf("friendlyGPU(%q) = %q, want %q", in, got, want)
		}
	}
}
