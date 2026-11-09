package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/sbauer/go-rocket/parsers"
)

var version = "dev"

type usageError struct {
	err error
}

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// Run executes the CLI with isolated arguments and output streams.
func Run(args []string, stdout, stderr io.Writer) int {
	if err := run(args, stdout); err != nil {
		fmt.Fprintf(stderr, "rocket-cli: %v\n", err)
		var usage *usageError
		if errors.As(err, &usage) {
			return 2
		}
		return 1
	}
	return 0
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return &usageError{fmt.Errorf("expected a command; use rocket-cli --help")}
	}
	switch args[0] {
	case "--help", "-h", "help":
		_, err := io.WriteString(stdout, rootHelp)
		return err
	case "--version":
		_, err := fmt.Fprintln(stdout, version)
		return err
	case "summary":
		return summary(args[1:], stdout)
	default:
		return &usageError{fmt.Errorf("unknown command %q; use rocket-cli --help", args[0])}
	}
}

func summary(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("summary", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	crc := flags.String("crc", "always", "CRC validation: always, never, or on-error")
	format := flags.String("format", "text", "Output format: text or json")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err := io.WriteString(stdout, summaryHelp)
			return err
		}
		return &usageError{err}
	}
	if flags.NArg() != 1 {
		return &usageError{fmt.Errorf("summary requires exactly one replay file; use rocket-cli summary --help")}
	}
	if *format != "text" && *format != "json" {
		return &usageError{fmt.Errorf("invalid --format %q (want text or json)", *format)}
	}
	mode, err := crcMode(*crc)
	if err != nil {
		return &usageError{err}
	}
	path := flags.Arg(0)
	replay, err := parsers.ParseFile(path, parsers.ParseOptions{CRCValidation: mode})
	if err != nil {
		return fmt.Errorf("parse %q: %w", path, err)
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

const rootHelp = `Usage: rocket-cli <command> [options] <replay-file>

Read-only Rocket League replay inspection using go-rocket.

Commands:
  summary     Show match details, teams, players, and goals

Options:
  --help      Show this help
  --version   Show the build version

Use rocket-cli <command> --help for command options.
`

const summaryHelp = `Usage: rocket-cli summary [options] <replay-file>

Options (must precede the file):
  --crc always|never|on-error   CRC validation (default: always)
  --format text|json           Output format (default: text)
  --help                       Show this help

Example: rocket-cli summary match.replay
`
