package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	rocket "github.com/sbauer/go-rocket"
)

func TestSummary(t *testing.T) {
	replay := &rocket.Replay{
		HeaderInfo: &rocket.ReplayHeader{ClassName: "TAGame.Replay_Soccar_TA"},
		Properties: map[string]*rocket.Property{
			"MapName":  {Value: "Stadium"},
			"TeamSize": {Value: int32(1)},
			"PlayerStats": {Type: "ArrayProperty", Groups: []*rocket.PropertyGroup{
				{Properties: map[string]*rocket.Property{
					"Name": {Value: "Player\nOne"}, "Team": {Value: int32(0)}, "Score": {Value: int32(100)},
				}},
			}},
			"Goals": {Type: "ArrayProperty", Groups: []*rocket.PropertyGroup{
				{Properties: map[string]*rocket.Property{
					"frame": {Value: int32(42)}, "PlayerName": {Value: "Player\nOne"}, "PlayerTeam": {Value: int32(0)},
				}},
			}},
		},
	}
	match, err := replay.MatchData()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeSummary(&out, replay, match); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Map: Stadium", "Date: unknown", "Team size: 1",
		"Blue score: 1 (derived)", "Orange score: 0 (derived)", "Winner: Blue (derived)",
		"Players:", `Player\nOne`, "100", "Goals:", "42"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary missing %q:\n%s", want, &out)
		}
	}
}

func TestSummaryMissingMetadata(t *testing.T) {
	replay := &rocket.Replay{HeaderInfo: &rocket.ReplayHeader{}}
	match, err := replay.MatchData()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeSummary(&out, replay, match); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Team size: unknown", "Blue score: unknown",
		"Winner: unknown", "No player stats recorded.", "No goals recorded."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary missing %q:\n%s", want, &out)
		}
	}
	if err := writeSummary(failingWriter{}, replay, match); !errors.Is(err, errWrite) {
		t.Fatalf("write error = %v", err)
	}
}

func TestSummaryMissingPlayerGroups(t *testing.T) {
	tests := []struct {
		name       string
		properties map[string]*rocket.Property
	}{
		{name: "missing property"},
		{name: "nil property", properties: map[string]*rocket.Property{"PlayerStats": nil}},
		{name: "missing groups", properties: map[string]*rocket.Property{
			"PlayerStats": {Type: "ArrayProperty"},
		}},
		{name: "short groups", properties: map[string]*rocket.Property{
			"PlayerStats": {Type: "ArrayProperty", Groups: []*rocket.PropertyGroup{}},
		}},
		{name: "nil group", properties: map[string]*rocket.Property{
			"PlayerStats": {Type: "ArrayProperty", Groups: []*rocket.PropertyGroup{nil}},
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			replay := &rocket.Replay{
				HeaderInfo: &rocket.ReplayHeader{},
				Properties: test.properties,
			}
			match := rocket.MatchData{PlayerStats: []rocket.PlayerStat{{Name: "Player"}}}
			var out bytes.Buffer
			if err := writeSummary(&out, replay, match); err != nil {
				t.Fatal(err)
			}
			for _, line := range strings.Split(out.String(), "\n") {
				if strings.HasPrefix(line, "Player ") {
					if !strings.Contains(line, "unknown") {
						t.Errorf("player stats should be unknown, got %q", line)
					}
					return
				}
			}
			t.Fatalf("player row missing:\n%s", out.String())
		})
	}
}

func TestStatTextNilValue(t *testing.T) {
	fields := map[string]*rocket.Property{"Score": {}}
	for _, value := range []interface{}{nil, (*int32)(nil), map[string]string(nil), []int(nil)} {
		if got := statText(fields, "Score", value); got != "unknown" {
			t.Errorf("statText(%T(nil)) = %q, want unknown", value, got)
		}
	}
}

func TestSummaryUnknownDerivedWinner(t *testing.T) {
	replay := &rocket.Replay{HeaderInfo: &rocket.ReplayHeader{}}
	match := rocket.MatchData{WinningTeamDerived: true}
	var out bytes.Buffer
	if err := writeSummary(&out, replay, match); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Winner: unknown (derived)") {
		t.Fatalf("unknown winner should not be marked derived:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Winner: unknown\n") {
		t.Fatalf("summary missing unknown winner:\n%s", out.String())
	}
}

var errWrite = errors.New("output unavailable")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }
