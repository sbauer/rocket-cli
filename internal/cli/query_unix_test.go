//go:build unix

package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestQueryRejectsFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run([]string{"query", "--sql", "SELECT 1", path}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, stderr %s", code, &errOut)
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "regular file") {
		t.Fatalf("stdout %q, stderr %q", &out, &errOut)
	}
}
