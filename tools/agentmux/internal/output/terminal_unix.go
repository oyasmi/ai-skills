//go:build linux || darwin

package output

import (
	"os"
	"syscall"
	"unsafe"
)

func terminalWidthFromFile(file *os.File) int {
	var size struct {
		rows uint16
		cols uint16
		x    uint16
		y    uint16
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
		return 0
	}
	return int(size.cols)
}
