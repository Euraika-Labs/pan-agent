//go:build windows

package claw3d

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	modkernel32     = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess = modkernel32.NewProc("OpenProcess")
)

const processQueryLimitedInfo = 0x1000

// probeAlive checks whether the process is still alive on Windows by opening
// a handle with PROCESS_QUERY_LIMITED_INFORMATION rights.
func probeAlive(p *os.Process) bool {
	h, _, _ := procOpenProcess.Call(processQueryLimitedInfo, 0, uintptr(p.Pid))
	if h == 0 {
		return false
	}
	syscall.CloseHandle(syscall.Handle(h))
	_ = unsafe.Sizeof(h) // keep unsafe import live
	return true
}

// killProcess terminates the process on Windows.
func killProcess(p *os.Process) error {
	return p.Kill()
}
