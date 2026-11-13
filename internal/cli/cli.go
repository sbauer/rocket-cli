package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	rocket "github.com/sbauer/go-rocket"
	"github.com/sbauer/go-rocket/parsers"
	"github.com/sbauer/go-rocket/source"
)

var version = "dev"

type usageError struct {
	err error
}

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// Run executes the CLI with isolated arguments and output streams.
func Run(args []string, stdout, stderr io.Writer) int {
	if err := run(args, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "rocket: %v\n", err)
		var usage *usageError
		if errors.As(err, &usage) {
			return 2
		}
		return 1
	}
	return 0
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return &usageError{fmt.Errorf("expected a command; use rocket --help")}
	}
	switch args[0] {
	case "--help", "-h", "help":
		_, err := io.WriteString(stdout, rootHelp)
		return err
	case "--version":
		_, err := fmt.Fprintln(stdout, version)
		return err
	case "summary":
		return summary(args[1:], stdout, stderr)
	case "replays":
		return replays(args[1:], stdout, stderr)
	case "query":
		if err := query(args[1:], stdout); err != nil {
			var usage *usageError
			if errors.As(err, &usage) {
				return &usageError{safeQueryError{usage.err}}
			}
			return safeQueryError{err}
		}
		return nil
	case "frames", "trajectories":
		return gameplay(args[0], args[1:], stdout)
	default:
		return &usageError{fmt.Errorf("unknown command %q; use rocket --help", args[0])}
	}
}

func summary(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("summary", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	crc := flags.String("crc", "always", "CRC validation: always, never, or on-error")
	format := flags.String("format", "text", "Output format: text or json")
	dir := flags.String("dir", "", "Replay library directory")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err := io.WriteString(stdout, summaryHelp)
			return err
		}
		return &usageError{err}
	}
	if flags.NArg() != 1 {
		return &usageError{fmt.Errorf("summary requires exactly one replay file; use rocket summary --help")}
	}
	if *format != "text" && *format != "json" {
		return &usageError{fmt.Errorf("invalid --format %q (want text or json)", *format)}
	}
	mode, err := crcMode(*crc)
	if err != nil {
		return &usageError{err}
	}
	selector := flags.Arg(0)
	path, err := resolveReplay(selector, *dir, mode, stderr)
	if err != nil {
		return err
	}
	replay, err := parseReplay(path, mode, false)
	if err != nil {
		if path != selector {
			return fmt.Errorf("%s", text(err.Error()))
		}
		return err
	}
	match, err := replay.MatchData()
	if err != nil {
		return fmt.Errorf("match data in %q: %w", path, err)
	}
	if *format == "json" {
		return writeJSON(stdout, newSummaryJSON(replay, match))
	}
	return writeSummary(stdout, replay, match)
}

func parseReplay(path string, crc parsers.CRCValidationMode, decode bool) (*rocket.Replay, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect replay %q: %w", path, err)
	}
	if info.Size() > maxReplayInputBytes {
		return nil, replayInputLimitError(path)
	}

	data, err := io.ReadAll(io.LimitReader(file, maxReplayInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read replay %q: %w", path, err)
	}
	if int64(len(data)) > maxReplayInputBytes {
		return nil, replayInputLimitError(path)
	}
	replay, err := parsers.ParseWithOptions(source.FromBytes(data),
		parsers.ParseOptions{CRCValidation: crc, DecodeNetwork: decode})
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return replay, nil
}

const maxReplayInputBytes = 64 << 20

func replayInputLimitError(path string) error {
	return fmt.Errorf("parse %q: replay input exceeds 64 MiB limit", path)
}

func crcMode(value string) (parsers.CRCValidationMode, error) {
	switch value {
	case "always":
		return parsers.CRCValidationAlways, nil
	case "never":
		return parsers.CRCValidationNever, nil
	case "on-error":
		return parsers.CRCValidationOnParseError, nil
	default:
		return 0, fmt.Errorf("invalid --crc %q (want always, never, or on-error)", value)
	}
}

const rootHelp = `Usage: rocket <command> [options] [replay-file|selector]

Read-only Rocket League replay inspection using go-rocket.

Commands:
  summary     Show match details, teams, players, and goals
  replays list List the replay library, newest first
  query       Query replay data with read-only SQL
  frames      Export decoded network frames as JSON
  trajectories Export known car/ball positions as JSON

Options:
  --help      Show this help
  --version   Show the build version

Use rocket <command> --help for command options.
`

const summaryHelp = `Usage: rocket summary [options] <replay-file|latest|position|name>

Options (must precede the file or selector):
  --crc always|never|on-error   CRC validation (default: always)
  --format text|json           Output format (default: text)
  --dir directory              Library override (then ROCKET_REPLAY_DIR, then discovery)
  --help                       Show this help

Example: rocket summary match.replay
         rocket summary --dir "/path/to/Demos" latest
`
