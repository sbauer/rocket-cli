//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPlatformDiscovery(t *testing.T) {
	home := t.TempDir()
	mkdir := func(path string) {
		t.Helper()
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.GOOS == "darwin" {
		path := filepath.Join(home, "Library", "Application Support", "Rocket League", "TAGame", "Demos")
		mkdir(path)
		candidates, err := platformReplayDirectories(home)
		if err != nil {
			t.Fatal(err)
		}
		got, err := chooseReplayDirectory(candidates)
		canonical, canonicalErr := filepath.EvalSymlinks(path)
		if canonicalErr != nil {
			t.Fatal(canonicalErr)
		}
		if err != nil || got != canonical {
			t.Fatalf("macOS discovery = %q, %v", got, err)
		}
	}
	steam := filepath.Join(home, ".local", "share", "Steam", "steamapps")
	mkdir(steam)
	secondary := filepath.Join(home, "Secondary Library")
	path := filepath.Join(secondary, "steamapps", "compatdata", "252950", "pfx", "drive_c", "users",
		"steamuser", "Documents", "My Games", "Rocket League", "TAGame", "Demos")
	mkdir(path)
	if err := os.WriteFile(filepath.Join(steam, "libraryfolders.vdf"),
		[]byte(`"libraryfolders" { "1" { "path" "`+secondary+`" } }`), 0600); err != nil {
		t.Fatal(err)
	}
	candidates, err := linuxReplayDirectories(home)
	if err != nil {
		t.Fatal(err)
	}
	got, err := chooseReplayDirectory(candidates)
	canonical, canonicalErr := filepath.EvalSymlinks(path)
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	if err != nil || got != canonical {
		t.Fatalf("secondary Steam library = %q, %v", got, err)
	}
	epic := filepath.Join(home, "Games", "Heroic", "Prefixes", "rocketleague", "drive_c", "users",
		"player", "Documents", "My Games", "Rocket League", "TAGame", "Demos")
	mkdir(epic)
	candidates, err = linuxReplayDirectories(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chooseReplayDirectory(candidates); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("Steam/Epic ambiguity = %v", err)
	}
}
