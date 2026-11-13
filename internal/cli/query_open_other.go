//go:build !unix

package cli

import "os"

func openQueryReplay(path string) (*os.File, error) {
	return os.Open(path)
}
