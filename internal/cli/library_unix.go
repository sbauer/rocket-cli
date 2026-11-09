//go:build !windows

package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

func platformReplayDirectories(home string) ([]string, error) {
	if runtime.GOOS == "darwin" {
		return []string{filepath.Join(home, "Library", "Application Support", "Rocket League", "TAGame", "Demos")}, nil
	}
	return linuxReplayDirectories(home)
}

func linuxReplayDirectories(home string) ([]string, error) {
	roots := []string{
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".local", "share", "Steam"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
	}
	pathPattern := regexp.MustCompile(`"path"\s*"((?:\\.|[^"\\])*)"`)
	for _, root := range append([]string(nil), roots...) {
		config := filepath.Join(root, "steamapps", "libraryfolders.vdf")
		data, err := os.ReadFile(config)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read Steam libraries %q: %w; use --dir or ROCKET_REPLAY_DIR", config, err)
		}
		for _, match := range pathPattern.FindAllStringSubmatch(string(data), -1) {
			path := strings.ReplaceAll(strings.ReplaceAll(match[1], `\\`, `\`), `\"`, `"`)
			if filepath.IsAbs(path) {
				roots = append(roots, path)
			}
		}
	}
	prefixes := []string{filepath.Join(home, ".wine")}
	for _, root := range roots {
		prefixes = append(prefixes, filepath.Join(root, "steamapps", "compatdata", "252950", "pfx"))
	}
	for _, pattern := range []string{
		filepath.Join(home, "Games", "Heroic", "Prefixes", "*"),
		filepath.Join(home, "Games", "Heroic", "Prefixes", "*", "pfx"),
		filepath.Join(home, "Games", "*"),
		filepath.Join(home, ".local", "share", "bottles", "bottles", "*"),
		filepath.Join(home, ".var", "app", "com.usebottles.bottles", "data", "bottles", "bottles", "*"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("discover Wine prefixes: %w", err)
		}
		prefixes = append(prefixes, matches...)
	}
	var candidates []string
	for _, prefix := range prefixes {
		for _, documents := range []string{"Documents", "My Documents", filepath.Join("OneDrive", "Documents")} {
			pattern := filepath.Join(prefix, "drive_c", "users", "*", documents, "My Games", "Rocket League", "TAGame", "Demos")
			matches, err := filepath.Glob(pattern)
			if err != nil {
				return nil, fmt.Errorf("discover replay directories: %w", err)
			}
			candidates = append(candidates, matches...)
		}
	}
	return candidates, nil
}
