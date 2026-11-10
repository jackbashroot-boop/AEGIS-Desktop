package main

import (
	"syscall"
	"unsafe"
)

func diskUsage(path string) (total, free uint64) {
	k := syscall.NewLazyDLL("kernel32.dll")
	fn := k.NewProc("GetDiskFreeSpaceExW")
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0
	}
	var freeAvail, tot, totFree uint64
	r, _, _ := fn.Call(uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeAvail)),
		uintptr(unsafe.Pointer(&tot)),
		uintptr(unsafe.Pointer(&totFree)))
	if r == 0 {
		return 0, 0
	}
	return tot, freeAvail
}
