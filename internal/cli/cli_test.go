package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUsage(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		code int
		text string
	}{
		{"root help", []string{"--help"}, 0, "Usage:"},
		{"command help", []string{"summary", "--help"}, 0, "--crc"},
		{"missing command", nil, 2, "expected a command"},
		{"unknown command", []string{"unknown"}, 2, "unknown command"},
		{"missing file", []string{"summary"}, 2, "exactly one"},
		{"extra file", []string{"summary", "a", "b"}, 2, "exactly one"},
		{"unknown flag", []string{"summary", "--unknown"}, 2, "flag provided but not defined"},
		{"invalid crc", []string{"summary", "--crc", "sometimes", "a"}, 2, "invalid --crc"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := Run(tt.args, &out, &errOut); code != tt.code {
				t.Fatalf("exit = %d, want %d; stderr: %s", code, tt.code, &errOut)
			}
			if tt.code == 0 {
				if errOut.Len() != 0 || !strings.Contains(out.String(), tt.text) {
					t.Fatalf("stdout: %q; stderr: %q", &out, &errOut)
				}
			} else if out.Len() != 0 || !strings.Contains(errOut.String(), tt.text) {
				t.Fatalf("stdout: %q; stderr: %q", &out, &errOut)
			}
		})
	}
}

func TestInvalidReplay(t *testing.T) {
	dir := t.TempDir()
	malformed := filepath.Join(dir, "bad.replay")
	if err := os.WriteFile(malformed, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "missing.replay"), malformed, dir} {
		var out, errOut bytes.Buffer
		if code := Run([]string{"summary", path}, &out, &errOut); code != 1 {
			t.Fatalf("%s: exit = %d, want 1", path, code)
		}
		if out.Len() != 0 || !strings.Contains(errOut.String(), path) {
			t.Fatalf("stdout: %q; stderr: %q", &out, &errOut)
		}
	}
}
