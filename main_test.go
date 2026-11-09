package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCLIIntegration(t *testing.T) {
	dir := t.TempDir()
	name := "rocket-cli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(dir, name)
	if output, err := exec.Command("go", "build", "-mod=readonly", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	fixture := filepath.Join("internal", "cli", "testdata", "territory.replay")
	original, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, wantCode int, args ...string) (string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		cmd := exec.Command(binary, args...)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		code := 0
		if err := cmd.Run(); err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("execute CLI: %v", err)
			}
			code = exit.ExitCode()
		}
		if code != wantCode {
			t.Fatalf("args %v: exit %d, want %d\nstdout: %s\nstderr: %s",
				args, code, wantCode, &stdout, &stderr)
		}
		if code == 0 && stderr.Len() != 0 {
			t.Fatalf("successful command wrote stderr: %s", &stderr)
		}
		if code != 0 && (stdout.Len() != 0 || stderr.Len() == 0) {
			t.Fatalf("failed command streams: stdout %q, stderr %q", &stdout, &stderr)
		}
		return stdout.String(), stderr.String()
	}

	t.Run("help and errors", func(t *testing.T) {
		for _, command := range []string{"summary", "frames", "trajectories"} {
			out, _ := run(t, 0, command, "--help")
			if !strings.Contains(out, "Usage: rocket-cli "+command) {
				t.Fatalf("missing command help: %s", out)
			}
			run(t, 2, command)
		}
		run(t, 2, "unknown")
		missing := filepath.Join(dir, "missing.replay")
		_, errOut := run(t, 1, "summary", missing)
		if !strings.Contains(errOut, missing) {
			t.Fatalf("missing path not identified: %s", errOut)
		}
		run(t, 1, "summary", dir)
		malformed := filepath.Join(dir, "malformed.replay")
		if err := os.WriteFile(malformed, []byte{1, 2}, 0600); err != nil {
			t.Fatal(err)
		}
		run(t, 1, "summary", "--format", "json", malformed)
		run(t, 2, "summary", "--format", "xml", fixture)
	})

	t.Run("match summary", func(t *testing.T) {
		out, _ := run(t, 0, "summary", fixture)
		for _, want := range []string{"Map: UF_Day_P", "Match type: Private",
			"Blue score: unknown", "Players:", "Drogings", "76561198052527453", "No goals recorded."} {
			if !strings.Contains(out, want) {
				t.Errorf("summary missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("JSON summary", func(t *testing.T) {
		out, _ := run(t, 0, "summary", "--format", "json", fixture)
		var result struct {
			Metadata struct {
				ClassName string `json:"class_name"`
			} `json:"metadata"`
			Match struct {
				Map      string `json:"map"`
				TeamSize *int   `json:"team_size"`
				Players  []struct {
					Name     string `json:"name"`
					OnlineID string `json:"online_id"`
				} `json:"players"`
				Goals []any `json:"goals"`
			} `json:"match"`
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		if result.Metadata.ClassName != "TAGame.Replay_Soccar_TA" || result.Match.Map != "UF_Day_P" ||
			result.Match.TeamSize != nil || len(result.Match.Players) != 1 ||
			result.Match.Players[0].OnlineID != "76561198052527453" || result.Match.Goals == nil {
			t.Fatalf("unexpected JSON summary: %+v", result)
		}
	})

	t.Run("network exports", func(t *testing.T) {
		out, _ := run(t, 0, "frames", fixture)
		var frames struct {
			Frames []struct {
				Index  int `json:"index"`
				Actors []struct {
					ActorID int32 `json:"actor_id"`
				} `json:"actors"`
			} `json:"frames"`
		}
		if err := json.Unmarshal([]byte(out), &frames); err != nil {
			t.Fatal(err)
		}
		if len(frames.Frames) != 248 || frames.Frames[0].Index != 0 || len(frames.Frames[0].Actors) == 0 {
			t.Fatalf("unexpected decoded frames: %d", len(frames.Frames))
		}
		out, _ = run(t, 0, "trajectories", fixture)
		var trajectories struct {
			Trajectories []struct {
				Kind    string `json:"kind"`
				Samples []any  `json:"samples"`
			} `json:"trajectories"`
		}
		if err := json.Unmarshal([]byte(out), &trajectories); err != nil {
			t.Fatal(err)
		}
		kinds := map[string]int{}
		for _, trajectory := range trajectories.Trajectories {
			kinds[trajectory.Kind] += len(trajectory.Samples)
		}
		if kinds["car"] == 0 || kinds["ball"] == 0 {
			t.Fatalf("missing position samples: %v", kinds)
		}
	})

	t.Run("CRC modes", func(t *testing.T) {
		corrupt := bytes.Clone(original)
		corrupt[4] ^= 1
		path := filepath.Join(dir, "bad-crc.replay")
		if err := os.WriteFile(path, corrupt, 0600); err != nil {
			t.Fatal(err)
		}
		_, errOut := run(t, 1, "summary", "--format", "json", path)
		if !strings.Contains(errOut, "CRC") {
			t.Fatalf("missing CRC diagnostic: %s", errOut)
		}
		run(t, 0, "summary", "--crc", "never", path)
		run(t, 0, "summary", "--crc", "on-error", path)
	})

	after, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(after) != sha256.Sum256(original) {
		t.Fatal("CLI modified the replay fixture")
	}
}
