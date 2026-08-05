//go:build !linux && !darwin

package output

import "os"

func terminalWidthFromFile(_ *os.File) int { return 0 }
