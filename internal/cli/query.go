package cli

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	rocket "github.com/sbauer/go-rocket"
	"github.com/sbauer/go-rocket/parsers"
	"github.com/sbauer/go-rocket/source"
)

const (
	maxQueryReplayBytes  = 64 << 20
	maxQueryHeaderBytes  = 8 << 20
	maxQueryNetworkBytes = 8 << 20
	maxQueryFrames       = 250000
	maxQueryActorEvents  = 1000000
	maxQueryPositions    = 1000000
	maxQuerySQLBytes     = 64 << 10
)

func query(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("query", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	crc := flags.String("crc", "always", "CRC validation: always, never, or on-error")
	format := flags.String("format", "table", "Output format: table, json, or csv")
	sqlText := flags.String("sql", "", "Read-only SQL statement")
	decode := flags.Bool("decode-network", false, "Decode network data for positions")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err := io.WriteString(out, queryHelp)
			return err
		}
		return &usageError{err}
	}
	if flags.NArg() != 1 {
		return &usageError{fmt.Errorf("query requires exactly one replay file; use rocket query --help")}
	}
	if *format != "table" && *format != "json" && *format != "csv" {
		return &usageError{fmt.Errorf("invalid --format %q (want table, json, or csv)", *format)}
	}
	if strings.TrimSpace(*sqlText) == "" {
		return &usageError{errors.New("query requires --sql")}
	}
	if len(*sqlText) > maxQuerySQLBytes {
		return &usageError{fmt.Errorf("SQL exceeds %d bytes", maxQuerySQLBytes)}
	}
	if !utf8.ValidString(*sqlText) || strings.ContainsRune(*sqlText, '\x00') {
		return &usageError{errors.New("SQL must be valid UTF-8 without NUL bytes")}
	}
	mode, err := crcMode(*crc)
	if err != nil {
		return &usageError{err}
	}

	path := flags.Arg(0)
	replay, err := parseQueryReplay(path, mode, *decode)
	if err != nil {
		return err
	}
	match, err := replay.MatchData()
	if err != nil {
		return fmt.Errorf("match data in %q: %w", path, err)
	}
	var positions []rocket.ActorTrajectory
	if *decode {
		if err := checkTrajectoryBudget(replay, maxQueryActorEvents, maxQueryPositions); err != nil {
			return err
		}
		positions, err = replay.Trajectories()
		if err != nil {
			return fmt.Errorf("trajectories in %q: %w", path, err)
		}
	}
	result, err := runQuery(replay, match, positions, *decode, *sqlText)
	if err != nil {
		return err
	}
	return writeQueryResult(out, result, *format)
}

func parseQueryReplay(path string, crc parsers.CRCValidationMode, decode bool) (replay *rocket.Replay, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("open %q: replay must be a regular file", path)
	}
	file, err := openQueryReplay(path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			replay = nil
			err = errors.Join(err, fmt.Errorf("close %q: %w", path, closeErr))
		}
	}()
	info, err = file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("open %q: replay must be a regular file", path)
	}
	if info.Size() < 0 || info.Size() > maxQueryReplayBytes {
		return nil, fmt.Errorf("replay %q exceeds the %d-byte query input limit", path, maxQueryReplayBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxQueryReplayBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	if len(data) > maxQueryReplayBytes {
		return nil, fmt.Errorf("replay %q exceeds the %d-byte query input limit", path, maxQueryReplayBytes)
	}
	if len(data) >= 4 && binary.LittleEndian.Uint32(data[:4]) > maxQueryHeaderBytes {
		return nil, fmt.Errorf("replay %q exceeds the %d-byte query header limit", path, maxQueryHeaderBytes)
	}
	options := parsers.ParseOptions{CRCValidation: crc}
	replay, err = parsers.ParseWithOptions(source.FromBytes(data), options)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	if !decode {
		return replay, nil
	}
	if err := checkQueryDecodeBudget(replay, path); err != nil {
		return nil, err
	}
	options.DecodeNetwork = true
	replay, err = parsers.ParseWithOptions(source.FromBytes(data), options)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return replay, nil
}

func checkQueryDecodeBudget(replay *rocket.Replay, path string) error {
	if len(replay.NetworkData) > maxQueryNetworkBytes {
		return fmt.Errorf("network data in %q exceeds the %d-byte query limit", path, maxQueryNetworkBytes)
	}
	if property := replay.Properties["NumFrames"]; property != nil {
		if frames, ok := property.Value.(int32); ok && frames > maxQueryFrames {
			return fmt.Errorf("replay %q exceeds the %d-frame query limit", path, maxQueryFrames)
		}
	}
	return nil
}

func checkTrajectoryBudget(replay *rocket.Replay, maxActors, maxPositions int) error {
	actors, positions := 0, 0
	for _, frame := range replay.Frames {
		for _, actor := range frame.Actors {
			if actors >= maxActors {
				return fmt.Errorf("network data exceeds the %d-actor-event query limit", maxActors)
			}
			actors++
			if actor.Spawn != nil && actor.Spawn.Location != nil {
				if positions >= maxPositions {
					return fmt.Errorf("network data exceeds the %d-position query limit", maxPositions)
				}
				positions++
			}
			for _, attribute := range actor.Attributes {
				if _, ok := attribute.Value.(rocket.RigidBodyState); ok {
					if positions >= maxPositions {
						return fmt.Errorf("network data exceeds the %d-position query limit", maxPositions)
					}
					positions++
				}
			}
		}
	}
	return nil
}

const queryHelp = `Usage: rocket query [options] <replay-file>

Run one read-only SQL query over replay data.

Options (must precede the file):
  --sql <statement>            SQL SELECT or WITH statement
  --format table|json|csv      Output format (default: table)
  --decode-network             Decode network data for positions
  --crc always|never|on-error  CRC validation (default: always)
  --help                       Show this help

Example: rocket query --sql "SELECT name, goals FROM players" match.replay
`
