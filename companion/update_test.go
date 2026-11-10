package main

import "testing"

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.7.0", "1.6.0", true},
		{"1.6.0", "1.7.0", false},
		{"1.6.0", "1.6.0", false},
		{"2.0.0", "1.99.99", true},
		{"v1.8.1", "1.8.0", true},
		{"banana", "1.0.0", false},
		{"1.0.0", "banana", false},
		{"1.0", "0.9.9", false},
	}
	for _, c := range cases {
		if got := versionNewer(c.a, c.b); got != c.want {
			t.Errorf("versionNewer(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
