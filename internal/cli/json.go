package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	rocket "github.com/sbauer/go-rocket"
)

type summaryJSON struct {
	SchemaVersion int          `json:"schema_version"`
	Metadata      metadataJSON `json:"metadata"`
	Match         matchJSON    `json:"match"`
}

type metadataJSON struct {
	ClassName       string         `json:"class_name"`
	EngineVersion   int            `json:"engine_version"`
	LicenseeVersion int            `json:"licensee_version"`
	NetVersion      int            `json:"net_version"`
	Levels          []string       `json:"levels"`
	Keyframes       []keyframeJSON `json:"keyframes"`
	Tickmarks       []tickmarkJSON `json:"tickmarks"`
}

type keyframeJSON struct {
	Time     float32 `json:"time"`
	Frame    int32   `json:"frame"`
	Position int32   `json:"position"`
}

type tickmarkJSON struct {
	Frame       int32  `json:"frame"`
	Description string `json:"description"`
}

type matchJSON struct {
	MapName       *string         `json:"map"`
	Date          *string         `json:"date"`
	MatchType     *string         `json:"match_type"`
	TeamSize      *int32          `json:"team_size"`
	Winner        *int32          `json:"winner"`
	WinnerDerived bool            `json:"winner_derived"`
	Teams         []teamJSON      `json:"teams"`
	Players       []playerJSON    `json:"players"`
	Goals         []goalJSON      `json:"goals"`
	Highlights    []highlightJSON `json:"highlights"`
}

type teamJSON struct {
	Index         int      `json:"index"`
	Name          string   `json:"name"`
	Score         *int     `json:"score"`
	ScoreDerived  bool     `json:"score_derived"`
	IsWinner      bool     `json:"is_winner"`
	WinnerDerived bool     `json:"winner_derived"`
	Players       []string `json:"players"`
}

type playerJSON struct {
	Name          *string `json:"name"`
	Team          *int32  `json:"team"`
	Score         *int32  `json:"score"`
	Goals         *int32  `json:"goals"`
	Assists       *int32  `json:"assists"`
	Saves         *int32  `json:"saves"`
	Shots         *int32  `json:"shots"`
	IsBot         *bool   `json:"is_bot"`
	Platform      *string `json:"platform"`
	OnlineID      *string `json:"online_id"`
	EpicAccountID *string `json:"epic_account_id"`
}

type goalJSON struct {
	Frame  int32  `json:"frame"`
	Player string `json:"player"`
	Team   int32  `json:"team"`
}

type highlightJSON struct {
	Frame     int32  `json:"frame"`
	Car       string `json:"car"`
	Ball      string `json:"ball"`
	GoalActor string `json:"goal_actor"`
}

func newSummaryJSON(replay *rocket.Replay, match rocket.MatchData) summaryJSON {
	header := replay.HeaderInfo
	result := summaryJSON{
		SchemaVersion: 1,
		Metadata: metadataJSON{
			ClassName: header.ClassName, EngineVersion: header.EngineVersion,
			LicenseeVersion: header.LicenseeVersion, NetVersion: header.NetVersion,
			Levels: nonNil(replay.Levels), Keyframes: []keyframeJSON{}, Tickmarks: []tickmarkJSON{},
		},
		Match: matchJSON{
			MapName:   present(replay.Properties, "MapName", match.MapName),
			Date:      present(replay.Properties, "Date", match.Date),
			MatchType: present(replay.Properties, "MatchType", match.MatchType),
			TeamSize:  present(replay.Properties, "TeamSize", match.TeamSize),
			Winner:    match.WinningTeam, WinnerDerived: match.WinningTeamDerived,
			Teams: []teamJSON{}, Players: []playerJSON{}, Goals: []goalJSON{}, Highlights: []highlightJSON{},
		},
	}
	for _, keyframe := range replay.Keyframes {
		result.Metadata.Keyframes = append(result.Metadata.Keyframes,
			keyframeJSON{Time: keyframe.Time, Frame: keyframe.Frame, Position: keyframe.Position})
	}
	for _, tickmark := range replay.Tickmarks {
		result.Metadata.Tickmarks = append(result.Metadata.Tickmarks,
			tickmarkJSON{Frame: tickmark.Frame, Description: tickmark.Description})
	}
	for _, team := range match.Teams {
		entry := teamJSON{
			Index: team.Index, Name: team.Name, ScoreDerived: team.ScoreDerived,
			IsWinner: team.IsWinner, WinnerDerived: team.WinnerDerived, Players: nonNil(team.Players),
		}
		if team.HasScore {
			value := team.Score
			entry.Score = &value
		}
		result.Match.Teams = append(result.Match.Teams, entry)
	}
	for i, player := range match.PlayerStats {
		fields := replay.Properties["PlayerStats"].Groups[i].Properties
		entry := playerJSON{
			Name: present(fields, "Name", player.Name), Team: player.Team,
			Score: present(fields, "Score", player.Score), Goals: present(fields, "Goals", player.Goals),
			Assists: present(fields, "Assists", player.Assists), Saves: present(fields, "Saves", player.Saves),
			Shots: present(fields, "Shots", player.Shots), IsBot: present(fields, "bBot", player.IsBot),
			Platform: present(fields, "Platform", player.Platform), EpicAccountID: player.EpicAccountID,
		}
		if player.OnlineID != nil {
			value := strconv.FormatUint(*player.OnlineID, 10)
			entry.OnlineID = &value
		}
		result.Match.Players = append(result.Match.Players, entry)
	}
	for _, goal := range match.Goals {
		result.Match.Goals = append(result.Match.Goals, goalJSON{
			Frame: goal.Frame, Player: goal.PlayerName, Team: goal.PlayerTeam,
		})
	}
	for _, highlight := range match.Highlights {
		result.Match.Highlights = append(result.Match.Highlights, highlightJSON{
			Frame: highlight.Frame, Car: highlight.CarName, Ball: highlight.BallName, GoalActor: highlight.GoalActorName,
		})
	}
	return result
}

func present[T any](fields map[string]*rocket.Property, name string, value T) *T {
	if _, ok := fields[name]; !ok {
		return nil
	}
	return &value
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

func writeJSON(out io.Writer, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	if _, err := out.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}
	return nil
}
