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

// signalZero checks whether a process is alive on Windows by opening a handle.
func signalZero(p *os.Process) error {
	h, _, err := procOpenProcess.Call(processQueryLimitedInfo, 0, uintptr(p.Pid))
	if h == 0 {
		return err
	}
	syscall.CloseHandle(syscall.Handle(h))
	_ = unsafe.Sizeof(h) // suppress unused import
	return nil
}

// killProcess terminates the process on Windows.
func killProcess(p *os.Process) error {
	return p.Kill()
}
