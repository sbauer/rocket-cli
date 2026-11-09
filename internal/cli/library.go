package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	rocket "github.com/sbauer/go-rocket"
	"github.com/sbauer/go-rocket/parsers"
)

type libraryReplay struct {
	path     string
	modified time.Time
	err      error
}

func replays(args []string, stdout, stderr io.Writer) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := io.WriteString(stdout, replaysHelp)
		return err
	}
	if len(args) == 0 || args[0] != "list" {
		return &usageError{fmt.Errorf("expected replays list; use rocket replays --help")}
	}
	flags := flag.NewFlagSet("replays list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("dir", "", "Replay library directory")
	crc := flags.String("crc", "always", "CRC validation: always, never, or on-error")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err := io.WriteString(stdout, replaysHelp)
			return err
		}
		return &usageError{err}
	}
	if flags.NArg() != 0 {
		return &usageError{fmt.Errorf("replays list takes no positional arguments")}
	}
	mode, err := crcMode(*crc)
	if err != nil {
		return &usageError{err}
	}
	directory, err := replayDirectory(*dir)
	if err != nil {
		return err
	}
	files, err := scanLibrary(directory)
	if err != nil {
		return err
	}
	table := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "Position\tFile\tName\tDate\tMap\tMode\tBlue\tOrange\tStatus")
	for i, file := range files {
		name, match, err := libraryMetadata(file, mode)
		status := "ok"
		if err != nil {
			fmt.Fprintf(stderr, "rocket: library metadata: %v\n", err)
			status = "unreadable metadata"
		}
		fmt.Fprintf(table, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			i+1, text(filepath.Base(file.path)), text(name), text(match.Date),
			text(match.MapName), text(match.MatchType), score(match.Team0Score, match.Team0ScoreDerived),
			score(match.Team1Score, match.Team1ScoreDerived), status)
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("write replay list: %w", err)
	}
	if len(files) == 0 {
		_, err = fmt.Fprintf(stdout, "No replays found in %s.\n", text(directory))
	}
	return err
}

func scanLibrary(dir string) ([]libraryReplay, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read replay directory %q: %w; use --dir or ROCKET_REPLAY_DIR", dir, err)
	}
	files := make([]libraryReplay, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".replay") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Stat(path)
		if err != nil {
			files = append(files, libraryReplay{path: path, err: fmt.Errorf("inspect replay %q: %w", path, err)})
			continue
		}
		if info.Mode().IsRegular() {
			files = append(files, libraryReplay{path: path, modified: info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].modified.Equal(files[j].modified) {
			return filepath.Base(files[i].path) < filepath.Base(files[j].path)
		}
		return files[i].modified.After(files[j].modified)
	})
	return files, nil
}

func libraryMetadata(file libraryReplay, crc parsers.CRCValidationMode) (string, rocket.MatchData, error) {
	if file.err != nil {
		return "", rocket.MatchData{}, file.err
	}
	replay, err := parseReplay(file.path, crc, false)
	if err != nil {
		return "", rocket.MatchData{}, err
	}
	name := ""
	if property := replay.Properties["ReplayName"]; property != nil {
		if value, ok := property.Value.(string); ok {
			name = value
		}
	}
	match, err := replay.MatchData()
	if err != nil {
		return name, rocket.MatchData{}, fmt.Errorf("match data in %q: %w", file.path, err)
	}
	return name, match, nil
}

func resolveReplay(selector, override string, crc parsers.CRCValidationMode, stderr io.Writer) (string, error) {
	// Existing local paths always retain their original meaning, even for "latest" or "3".
	if _, err := os.Stat(selector); err == nil {
		return selector, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect replay %q: %w", selector, err)
	}
	if filepath.IsAbs(selector) || strings.ContainsAny(selector, `/\`) {
		return selector, nil
	}
	dir, err := replayDirectory(override)
	if err != nil {
		return "", err
	}
	files, err := scanLibrary(dir)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no replays found in %q; use --dir or ROCKET_REPLAY_DIR", dir)
	}
	position, numberErr := strconv.Atoi(selector)
	if selector == "latest" {
		position, numberErr = 1, nil
	}
	if numberErr == nil {
		if position < 1 || position > len(files) {
			return "", fmt.Errorf("replay position %q out of range (1-%d); use rocket replays list", selector, len(files))
		}
		return files[position-1].path, nil
	}
	matches := make([]string, 0)
	for _, file := range files {
		base := filepath.Base(file.path)
		name, _, metadataErr := libraryMetadata(file, crc)
		if metadataErr != nil {
			fmt.Fprintf(stderr, "rocket: library metadata: %v\n", metadataErr)
		}
		if selector == base || selector == strings.TrimSuffix(base, filepath.Ext(base)) ||
			(name != "" && selector == name) {
			matches = append(matches, file.path)
		}
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("replay name %q is ambiguous: %q; select a list position or explicit path",
			selector, matches)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("replay %q not found in %q; use rocket replays list, --dir, or an explicit file path", selector, dir)
	}
	return matches[0], nil
}

func replayDirectory(override string) (string, error) {
	if override == "" {
		override = os.Getenv("ROCKET_REPLAY_DIR")
	}
	if override != "" {
		return filepath.Abs(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w; use --dir or ROCKET_REPLAY_DIR", err)
	}
	candidates, err := platformReplayDirectories(home)
	if err != nil {
		return "", err
	}
	return chooseReplayDirectory(candidates)
}

func chooseReplayDirectory(candidates []string) (string, error) {
	found := make([]string, 0)
	seen := make(map[string]bool)
	for _, path := range candidates {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("inspect replay directory %q: %w; use --dir or ROCKET_REPLAY_DIR", path, err)
		}
		if !info.IsDir() {
			continue
		}
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", fmt.Errorf("resolve replay directory %q: %w", path, err)
		}
		canonical, err = filepath.Abs(canonical)
		if err != nil {
			return "", err
		}
		if !seen[canonical] {
			seen[canonical] = true
			found = append(found, canonical)
		}
	}
	sort.Strings(found)
	if len(found) == 0 {
		return "", fmt.Errorf("no replay library detected; specify --dir or ROCKET_REPLAY_DIR with your TAGame/Demos directory")
	}
	if len(found) > 1 {
		return "", fmt.Errorf("multiple replay libraries detected: %q; choose one with --dir or ROCKET_REPLAY_DIR", found)
	}
	return found[0], nil
}

const replaysHelp = `Usage: rocket replays list [options]

List .replay files directly in the library, newest modification time first.
Ties are ordered by filename. Positions are 1-based and may change as files change.

Options:
  --dir directory              Library override (then ROCKET_REPLAY_DIR, then discovery)
  --crc always|never|on-error   CRC validation (default: always)
  --help                       Show this help

Select with: rocket summary [--dir directory] <latest|position|name>
Local files take precedence. Names match exact filenames, stems, or ReplayName.
Unreadable metadata produces a marked row and a diagnostic, without hiding other files.
`
