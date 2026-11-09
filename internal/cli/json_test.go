package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	rocket "github.com/sbauer/go-rocket"
)

func TestSummaryJSON(t *testing.T) {
	replay := &rocket.Replay{
		HeaderInfo: &rocket.ReplayHeader{ClassName: "Soccar", EngineVersion: 868},
		Properties: map[string]*rocket.Property{
			"MapName": {Value: "Stadium"},
			"PlayerStats": {Type: "ArrayProperty", Groups: []*rocket.PropertyGroup{
				{Properties: map[string]*rocket.Property{
					"Name": {Value: "Player"}, "OnlineID": {Value: uint64(math.MaxUint64)}, "Score": {Value: int32(0)},
				}},
			}},
		},
	}
	match, err := replay.MatchData()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeJSON(&out, newSummaryJSON(replay, match)); err != nil {
		t.Fatal(err)
	}
	var result summaryJSON
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Metadata.ClassName != "Soccar" || result.Match.MapName == nil || *result.Match.MapName != "Stadium" {
		t.Fatalf("unexpected metadata: %+v", result)
	}
	player := result.Match.Players[0]
	if player.OnlineID == nil || *player.OnlineID != "18446744073709551615" {
		t.Fatalf("identifier precision lost: %+v", player)
	}
	if player.Score == nil || *player.Score != 0 || player.Goals != nil || player.Team != nil {
		t.Fatalf("missing and zero fields conflated: %+v", player)
	}
	for _, want := range []string{`"date": null`, `"goals": []`, `"keyframes": []`, `"score": null`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("JSON missing %s:\n%s", want, &out)
		}
	}
}

func TestJSONErrors(t *testing.T) {
	if err := writeJSON(failingWriter{}, summaryJSON{}); !errors.Is(err, errWrite) {
		t.Fatalf("write error = %v", err)
	}
	var out bytes.Buffer
	if err := writeJSON(&out, math.NaN()); err == nil || out.Len() != 0 {
		t.Fatalf("encode failure produced output: %v, %q", err, &out)
	}
	var errOut bytes.Buffer
	if code := Run([]string{"summary", "--format", "xml", "missing.replay"}, &out, &errOut); code != 2 {
		t.Fatalf("invalid format: exit %d, stderr %s", code, &errOut)
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "invalid --format") {
		t.Fatalf("stdout %q, stderr %q", &out, &errOut)
	}
}
