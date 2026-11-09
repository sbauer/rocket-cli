package cli

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"

	rocket "github.com/sbauer/go-rocket"
)

func writeSummary(out io.Writer, replay *rocket.Replay, match rocket.MatchData) error {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Replay type: %s\n", text(replay.HeaderInfo.ClassName))
	fmt.Fprintf(&buf, "Map: %s\nDate: %s\nMatch type: %s\n",
		text(match.MapName), text(match.Date), text(match.MatchType))
	if _, ok := replay.Properties["TeamSize"]; ok {
		fmt.Fprintf(&buf, "Team size: %d\n", match.TeamSize)
	} else {
		fmt.Fprintln(&buf, "Team size: unknown")
	}
	fmt.Fprintf(&buf, "Blue score: %s\nOrange score: %s\n",
		score(match.Team0Score, match.Team0ScoreDerived),
		score(match.Team1Score, match.Team1ScoreDerived))
	winner := teamName(match.WinningTeam)
	if match.WinningTeamDerived {
		winner += " (derived)"
	}
	fmt.Fprintf(&buf, "Winner: %s\n", winner)

	fmt.Fprintln(&buf, "\nPlayers:")
	if len(match.PlayerStats) == 0 {
		fmt.Fprintln(&buf, "No player stats recorded.")
	} else {
		table := tabwriter.NewWriter(&buf, 0, 4, 2, ' ', 0)
		fmt.Fprintln(table, "Name\tTeam\tScore\tGoals\tAssists\tSaves\tShots\tBot\tPlatform\tOnline ID\tEpic ID")
		for i, player := range match.PlayerStats {
			fields := replay.Properties["PlayerStats"].Groups[i].Properties
			onlineID := "unknown"
			if player.OnlineID != nil {
				onlineID = strconv.FormatUint(*player.OnlineID, 10)
			}
			epicID := "unknown"
			if player.EpicAccountID != nil {
				epicID = text(*player.EpicAccountID)
			}
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				text(player.Name), teamName(player.Team), statText(fields, "Score", player.Score),
				statText(fields, "Goals", player.Goals), statText(fields, "Assists", player.Assists),
				statText(fields, "Saves", player.Saves), statText(fields, "Shots", player.Shots),
				statText(fields, "bBot", player.IsBot),
				text(player.Platform), onlineID, epicID)
		}
		if err := table.Flush(); err != nil {
			return fmt.Errorf("format players: %w", err)
		}
	}

	fmt.Fprintln(&buf, "\nGoals:")
	if len(match.Goals) == 0 {
		fmt.Fprintln(&buf, "No goals recorded.")
	} else {
		table := tabwriter.NewWriter(&buf, 0, 4, 2, ' ', 0)
		fmt.Fprintln(table, "Frame\tPlayer\tTeam")
		for _, goal := range match.Goals {
			fmt.Fprintf(table, "%d\t%s\t%s\n", goal.Frame, text(goal.PlayerName), teamName(&goal.PlayerTeam))
		}
		if err := table.Flush(); err != nil {
			return fmt.Errorf("format goals: %w", err)
		}
	}
	if _, err := out.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	return nil
}

func statText(fields map[string]*rocket.Property, name string, value interface{}) string {
	if _, ok := fields[name]; !ok {
		return "unknown"
	}
	return fmt.Sprint(value)
}

func text(value string) string {
	if value == "" {
		return "unknown"
	}
	quoted := strconv.Quote(value)
	return quoted[1 : len(quoted)-1]
}

func teamName(index *int32) string {
	if index == nil {
		return "unknown"
	}
	switch *index {
	case 0:
		return "Blue"
	case 1:
		return "Orange"
	default:
		return fmt.Sprintf("Team %d", *index)
	}
}

func score(value *int32, derived bool) string {
	if value == nil {
		return "unknown"
	}
	result := strconv.FormatInt(int64(*value), 10)
	if derived {
		result += " (derived)"
	}
	return result
}
