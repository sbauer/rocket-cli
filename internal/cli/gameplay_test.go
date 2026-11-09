package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rocket "github.com/sbauer/go-rocket"
)

const fixturePath = "testdata/territory.replay"

func TestGameplayFixture(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"frames", fixturePath}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, &errOut)
	}
	var frames framesJSON
	if err := json.Unmarshal(out.Bytes(), &frames); err != nil {
		t.Fatal(err)
	}
	if len(frames.Frames) != 248 || frames.Frames[0].Index != 0 || len(frames.Frames[0].Actors) == 0 {
		t.Fatalf("unexpected frame export: %d frames", len(frames.Frames))
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", &errOut)
	}
	out.Reset()
	if code := Run([]string{"trajectories", fixturePath}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, &errOut)
	}
	var trajectories trajectoriesJSON
	if err := json.Unmarshal(out.Bytes(), &trajectories); err != nil {
		t.Fatal(err)
	}
	kinds := map[rocket.TrajectoryKind]int{}
	for _, trajectory := range trajectories.Trajectories {
		kinds[trajectory.Kind] += len(trajectory.Samples)
		for _, sample := range trajectory.Samples {
			if sample.Frame < 0 || sample.Frame >= 248 {
				t.Errorf("sample outside frame range: %+v", sample)
			}
		}
	}
	if kinds[rocket.TrajectoryKindCar] == 0 || kinds[rocket.TrajectoryKindBall] == 0 {
		t.Fatalf("missing car/ball position samples: %v", kinds)
	}
}

func TestNetworkDecodeIsOptIn(t *testing.T) {
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	name := []byte("TAGame.RBActor_TA:ReplicatedRBState")
	if !bytes.Contains(data, name) {
		t.Fatal("fixture no longer contains test attribute")
	}
	data = bytes.ReplaceAll(data, name, bytes.Repeat([]byte("X"), len(name)))
	path := filepath.Join(t.TempDir(), "unsupported.replay")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"summary", "frames", "trajectories"} {
		var out, errOut bytes.Buffer
		code := Run([]string{command, "--crc", "never", path}, &out, &errOut)
		if command == "summary" {
			if code != 0 || errOut.Len() != 0 || out.Len() == 0 {
				t.Fatalf("summary should not decode: exit %d, stderr %s", code, &errOut)
			}
		} else if code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), "network:") {
			t.Fatalf("%s: exit %d, stdout %s, stderr %s", command, code, &out, &errOut)
		}
	}
}

func TestFrameExportPreservesOrderAndRejectsInvalidReferences(t *testing.T) {
	replay := &rocket.Replay{
		Objects: []string{"Archetypes.Ball.Ball_Default", "RBState"},
		Frames: []rocket.NetworkFrame{{Time: 1, Actors: []rocket.ActorReplication{
			{ActorID: 7, Spawn: &rocket.SpawnedActor{ObjectIndex: 0}},
			{ActorID: 7, Deleted: true},
			{ActorID: 7, Spawn: &rocket.SpawnedActor{ObjectIndex: 0},
				Attributes: []rocket.AttributeUpdate{{ObjectIndex: 1, Value: int32(3)}}},
		}}},
	}
	result, err := newFramesJSON(replay)
	if err != nil {
		t.Fatal(err)
	}
	actors := result.Frames[0].Actors
	if len(actors) != 3 || actors[0].Spawn == nil || !actors[1].Deleted ||
		actors[2].Attributes[0].ObjectName != "RBState" {
		t.Fatalf("event order or attribute identity changed: %+v", actors)
	}
	replay.Frames[0].Actors[2].Attributes[0].ObjectIndex = -1
	if _, err := newFramesJSON(replay); err == nil {
		t.Fatal("expected invalid attribute index error")
	}
	replay.Frames[0].Actors[0].Spawn.ObjectIndex = 2
	if _, err := newFramesJSON(replay); err == nil {
		t.Fatal("expected invalid spawn index error")
	}
}
