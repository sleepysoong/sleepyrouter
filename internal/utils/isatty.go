package utils

import (
	"os"
	"syscall"
	"unsafe"
)

const ioctlReadTermios = 0x5401 // TCGETS on Linux

// IsTerminal returns true if f is a terminal device.
func IsTerminal(f *os.File) bool {
	fd := f.Fd()
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, ioctlReadTermios, uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}
