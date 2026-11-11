package main

import "syscall"

func diskUsage(path string) (total, free uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err == nil {
		return st.Blocks * uint64(st.Bsize), st.Bavail * uint64(st.Bsize)
	}
	return 0, 0
}
