package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	rocket "github.com/sbauer/go-rocket"
)

func gameplay(command string, args []string, out io.Writer) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	crc := flags.String("crc", "always", "CRC validation: always, never, or on-error")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err := fmt.Fprintf(out, gameplayHelp, command, command)
			return err
		}
		return &usageError{err}
	}
	if flags.NArg() != 1 {
		return &usageError{fmt.Errorf("%s requires exactly one replay file; use rocket %s --help", command, command)}
	}
	mode, err := crcMode(*crc)
	if err != nil {
		return &usageError{err}
	}
	path := flags.Arg(0)
	replay, err := parseReplay(path, mode, true)
	if err != nil {
		return err
	}
	if command == "frames" {
		result, err := newFramesJSON(replay)
		if err != nil {
			return fmt.Errorf("frames in %q: %w", path, err)
		}
		return writeJSON(out, result)
	}
	trajectories, err := replay.Trajectories()
	if err != nil {
		return fmt.Errorf("trajectories in %q: %w", path, err)
	}
	return writeJSON(out, newTrajectoriesJSON(trajectories))
}

type framesJSON struct {
	SchemaVersion int         `json:"schema_version"`
	Objects       []string    `json:"objects"`
	Names         []string    `json:"names"`
	Frames        []frameJSON `json:"frames"`
}

type frameJSON struct {
	Index  int         `json:"index"`
	Time   float32     `json:"time"`
	Delta  float32     `json:"delta"`
	Actors []actorJSON `json:"actors"`
}

type actorJSON struct {
	ActorID    int32           `json:"actor_id"`
	Spawn      *spawnJSON      `json:"spawn"`
	Deleted    bool            `json:"deleted"`
	Attributes []attributeJSON `json:"attributes"`
}

type spawnJSON struct {
	NameIndex   *int32        `json:"name_index"`
	ObjectIndex int32         `json:"object_index"`
	ObjectName  string        `json:"object_name"`
	Location    *vectorIJSON  `json:"location"`
	Rotation    *rotationJSON `json:"rotation"`
}

type vectorIJSON struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
	Z int32 `json:"z"`
}

type rotationJSON struct {
	Yaw   *int8 `json:"yaw"`
	Pitch *int8 `json:"pitch"`
	Roll  *int8 `json:"roll"`
}

type attributeJSON struct {
	StreamID    int32  `json:"stream_id"`
	ObjectIndex int32  `json:"object_index"`
	ObjectName  string `json:"object_name"`
	ValueType   string `json:"value_type"`
	Value       any    `json:"value"`
}

func newFramesJSON(replay *rocket.Replay) (framesJSON, error) {
	result := framesJSON{
		SchemaVersion: 1, Objects: nonNil(replay.Objects), Names: nonNil(replay.Names), Frames: []frameJSON{},
	}
	for i, frame := range replay.Frames {
		entry := frameJSON{Index: i, Time: frame.Time, Delta: frame.Delta, Actors: []actorJSON{}}
		for _, actor := range frame.Actors {
			event := actorJSON{ActorID: actor.ActorID, Deleted: actor.Deleted, Attributes: []attributeJSON{}}
			if spawn := actor.Spawn; spawn != nil {
				name, err := objectName(replay.Objects, spawn.ObjectIndex)
				if err != nil {
					return framesJSON{}, fmt.Errorf("frame %d actor %d spawn: %w", i, actor.ActorID, err)
				}
				event.Spawn = &spawnJSON{
					NameIndex: spawn.NameIndex, ObjectIndex: spawn.ObjectIndex, ObjectName: name,
				}
				if location := spawn.Location; location != nil {
					event.Spawn.Location = &vectorIJSON{X: location.X, Y: location.Y, Z: location.Z}
				}
				if rotation := spawn.Rotation; rotation != nil {
					event.Spawn.Rotation = &rotationJSON{Yaw: rotation.Yaw, Pitch: rotation.Pitch, Roll: rotation.Roll}
				}
			}
			for _, attribute := range actor.Attributes {
				name, err := objectName(replay.Objects, attribute.ObjectIndex)
				if err != nil {
					return framesJSON{}, fmt.Errorf("frame %d actor %d attribute: %w", i, actor.ActorID, err)
				}
				event.Attributes = append(event.Attributes, attributeJSON{
					StreamID: attribute.StreamID, ObjectIndex: attribute.ObjectIndex, ObjectName: name,
					ValueType: fmt.Sprintf("%T", attribute.Value), Value: attribute.Value,
				})
			}
			entry.Actors = append(entry.Actors, event)
		}
		result.Frames = append(result.Frames, entry)
	}
	return result, nil
}

func objectName(objects []string, index int32) (string, error) {
	if index < 0 || int64(index) >= int64(len(objects)) {
		return "", fmt.Errorf("invalid object index %d", index)
	}
	return objects[index], nil
}

type trajectoriesJSON struct {
	SchemaVersion int              `json:"schema_version"`
	Trajectories  []trajectoryJSON `json:"trajectories"`
}

type trajectoryJSON struct {
	ActorID    int32                 `json:"actor_id"`
	Instance   int                   `json:"instance"`
	Kind       rocket.TrajectoryKind `json:"kind"`
	ObjectName string                `json:"object_name"`
	Samples    []sampleJSON          `json:"samples"`
}

type sampleJSON struct {
	Frame    int         `json:"frame"`
	Time     float32     `json:"time"`
	Position vectorFJSON `json:"position"`
}

type vectorFJSON struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	Z float32 `json:"z"`
}

func newTrajectoriesJSON(trajectories []rocket.ActorTrajectory) trajectoriesJSON {
	result := trajectoriesJSON{SchemaVersion: 1, Trajectories: []trajectoryJSON{}}
	for _, trajectory := range trajectories {
		entry := trajectoryJSON{
			ActorID: trajectory.ActorID, Instance: trajectory.Instance, Kind: trajectory.Kind,
			ObjectName: trajectory.ObjectName, Samples: []sampleJSON{},
		}
		for _, sample := range trajectory.Samples {
			entry.Samples = append(entry.Samples, sampleJSON{
				Frame: sample.Frame, Time: sample.Time,
				Position: vectorFJSON{X: sample.Position.X, Y: sample.Position.Y, Z: sample.Position.Z},
			})
		}
		result.Trajectories = append(result.Trajectories, entry)
	}
	return result
}

const gameplayHelp = `Usage: rocket %s [options] <replay-file>

Decode network data and export JSON. Unsupported network data is an error.

Options (must precede the file):
  --crc always|never|on-error   CRC validation (default: always)
  --help                       Show this help

Example: rocket %s match.replay
`
