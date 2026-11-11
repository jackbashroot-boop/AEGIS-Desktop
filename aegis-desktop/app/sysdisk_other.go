//go:build !linux && !windows

package main

func diskUsage(path string) (total, free uint64) { return 0, 0 }
