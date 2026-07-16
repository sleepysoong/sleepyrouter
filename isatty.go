package sleepyrouter

import (
	"os"
	"syscall"
	"unsafe"
)

const ioctlReadTermios = 0x5401 // TCGETS on Linux

// isTerminal returns true if f is a terminal device.
func isTerminal(f *os.File) bool {
	fd := f.Fd()
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, ioctlReadTermios, uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}
