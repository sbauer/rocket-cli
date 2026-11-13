//go:build unix

package cli

import (
	"os"
	"syscall"
)

func openQueryReplay(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
