package cli

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"

	sqlite3 "github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/embed"
	rocket "github.com/sbauer/go-rocket"
)

const (
	maxQuerySQLiteBytes = 128 << 20
	maxQueryValueBytes  = 1 << 20
)

const querySchema = `
CREATE TABLE match (
  map TEXT, date TEXT, match_type TEXT, team_size INTEGER,
  team0_score INTEGER, team1_score INTEGER, winner INTEGER,
  team0_score_derived INTEGER, team1_score_derived INTEGER, winner_derived INTEGER
);
CREATE TABLE teams (
  team INTEGER, name TEXT, score INTEGER, score_derived INTEGER,
  is_winner INTEGER, winner_derived INTEGER
);
CREATE TABLE players (
  name TEXT, team INTEGER, score INTEGER, goals INTEGER, assists INTEGER,
  saves INTEGER, shots INTEGER, is_bot INTEGER, platform TEXT,
  online_id TEXT, epic_account_id TEXT
);
CREATE TABLE goals (frame INTEGER, player TEXT, team INTEGER);
CREATE TABLE highlights (frame INTEGER, car TEXT, ball TEXT, goal_actor TEXT);
`

const queryPositionsSchema = `
CREATE TABLE positions (
  actor_id INTEGER, instance INTEGER, kind TEXT, object_name TEXT,
  frame INTEGER, time REAL, x REAL, y REAL, z REAL
);
`

func runQuery(replay *rocket.Replay, match rocket.MatchData, positions []rocket.ActorTrajectory, decoded bool, sqlText string) (result queryResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// go-sqlite3 panics with an internal error value for SQLITE_NOMEM.
			panicErr, ok := recovered.(error)
			if !ok || panicErr.Error() != sqlite3.NOMEM.Error() {
				panic(recovered)
			}
			result = queryResult{}
			err = errors.Join(err, fmt.Errorf("query exceeds SQLite resource limits: %w", panicErr))
		}
	}()
	db, err := sqlite3.OpenFlags(":memory:", sqlite3.OPEN_READWRITE|sqlite3.OPEN_CREATE|sqlite3.OPEN_MEMORY|sqlite3.OPEN_PRIVATECACHE)
	if err != nil {
		return queryResult{}, fmt.Errorf("open query database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close query database: %w", closeErr))
		}
	}()
	db.HardHeapLimit(maxQuerySQLiteBytes)
	if db.HardHeapLimit(-1) != maxQuerySQLiteBytes {
		return queryResult{}, errors.New("could not set query database memory limit")
	}
	for _, setting := range []struct {
		option sqlite3.DBConfig
		value  bool
	}{
		{sqlite3.DBCONFIG_DEFENSIVE, true},
		{sqlite3.DBCONFIG_TRUSTED_SCHEMA, false},
		{sqlite3.DBCONFIG_ENABLE_LOAD_EXTENSION, false},
	} {
		if actual, configErr := db.Config(setting.option, setting.value); configErr != nil || actual != setting.value {
			if configErr != nil {
				return queryResult{}, fmt.Errorf("configure query database: %w", configErr)
			}
			return queryResult{}, errors.New("could not configure query database")
		}
	}
	for _, limit := range []struct {
		category sqlite3.LimitCategory
		value    int
	}{
		{sqlite3.LIMIT_LENGTH, maxQueryValueBytes},
		{sqlite3.LIMIT_SQL_LENGTH, maxQuerySQLBytes},
		{sqlite3.LIMIT_COLUMN, maxQueryColumns},
		{sqlite3.LIMIT_EXPR_DEPTH, 100},
		{sqlite3.LIMIT_COMPOUND_SELECT, 25},
		{sqlite3.LIMIT_VDBE_OP, 25000},
		{sqlite3.LIMIT_FUNCTION_ARG, 32},
		{sqlite3.LIMIT_ATTACHED, 0},
		{sqlite3.LIMIT_LIKE_PATTERN_LENGTH, 8192},
		{sqlite3.LIMIT_VARIABLE_NUMBER, 64},
		{sqlite3.LIMIT_WORKER_THREADS, 0},
	} {
		db.Limit(limit.category, limit.value)
		if db.Limit(limit.category, -1) > limit.value {
			return queryResult{}, fmt.Errorf("could not set query database limit %d", limit.category)
		}
	}
	schema := querySchema
	if decoded {
		schema += queryPositionsSchema
	}
	if err := db.Exec("PRAGMA temp_store = MEMORY; " + schema); err != nil {
		return queryResult{}, fmt.Errorf("create query tables: %w", err)
	}
	if err := db.Exec("BEGIN"); err != nil {
		return queryResult{}, fmt.Errorf("begin query data load: %w", err)
	}
	if err := populateQueryTables(db, replay, match, positions); err != nil {
		if rollbackErr := db.Exec("ROLLBACK"); rollbackErr != nil {
			err = errors.Join(err, fmt.Errorf("rollback query data load: %w", rollbackErr))
		}
		return queryResult{}, err
	}
	if err := db.Exec("COMMIT; PRAGMA query_only = ON"); err != nil {
		return queryResult{}, fmt.Errorf("finish query data load: %w", err)
	}
	return executeQuery(db, decoded, sqlText)
}

func populateQueryTables(db *sqlite3.Conn, replay *rocket.Replay, match rocket.MatchData, positions []rocket.ActorTrajectory) error {
	if err := withQueryInsert(db, "INSERT INTO match VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", func(stmt *sqlite3.Stmt) error {
		return insertQueryRow(stmt,
			present(replay.Properties, "MapName", match.MapName),
			present(replay.Properties, "Date", match.Date),
			present(replay.Properties, "MatchType", match.MatchType),
			present(replay.Properties, "TeamSize", match.TeamSize),
			match.Team0Score, match.Team1Score, match.WinningTeam,
			match.Team0ScoreDerived, match.Team1ScoreDerived, match.WinningTeamDerived)
	}); err != nil {
		return fmt.Errorf("load match: %w", err)
	}
	if err := withQueryInsert(db, "INSERT INTO teams VALUES (?, ?, ?, ?, ?, ?)", func(stmt *sqlite3.Stmt) error {
		for _, team := range match.Teams {
			var score *int
			if team.HasScore {
				score = &team.Score
			}
			var winner *bool
			if match.WinningTeam != nil && (*match.WinningTeam == 0 || *match.WinningTeam == 1) {
				winner = &team.IsWinner
			}
			if err := insertQueryRow(stmt, team.Index, team.Name, score, team.ScoreDerived, winner, team.WinnerDerived); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("load teams: %w", err)
	}
	if err := withQueryInsert(db, "INSERT INTO players VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", func(stmt *sqlite3.Stmt) error {
		for i, player := range match.PlayerStats {
			fields, err := queryGroupFields(replay, "PlayerStats", i)
			if err != nil {
				return err
			}
			var onlineID *string
			if player.OnlineID != nil {
				value := strconv.FormatUint(*player.OnlineID, 10)
				onlineID = &value
			}
			if err := insertQueryRow(stmt,
				present(fields, "Name", player.Name), player.Team,
				present(fields, "Score", player.Score), present(fields, "Goals", player.Goals),
				present(fields, "Assists", player.Assists), present(fields, "Saves", player.Saves),
				present(fields, "Shots", player.Shots), present(fields, "bBot", player.IsBot),
				present(fields, "Platform", player.Platform), onlineID, player.EpicAccountID); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("load players: %w", err)
	}
	if err := withQueryInsert(db, "INSERT INTO goals VALUES (?, ?, ?)", func(stmt *sqlite3.Stmt) error {
		for _, goal := range match.Goals {
			if err := insertQueryRow(stmt, goal.Frame, goal.PlayerName, goal.PlayerTeam); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("load goals: %w", err)
	}
	if err := withQueryInsert(db, "INSERT INTO highlights VALUES (?, ?, ?, ?)", func(stmt *sqlite3.Stmt) error {
		for i, highlight := range match.Highlights {
			fields, err := queryGroupFields(replay, "HighLights", i)
			if err != nil {
				return err
			}
			if err := insertQueryRow(stmt, highlight.Frame, highlight.CarName, highlight.BallName,
				present(fields, "GoalActorName", highlight.GoalActorName)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("load highlights: %w", err)
	}
	if len(positions) == 0 {
		return nil
	}
	if err := withQueryInsert(db, "INSERT INTO positions VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", func(stmt *sqlite3.Stmt) error {
		for _, trajectory := range positions {
			for _, sample := range trajectory.Samples {
				if sample.Frame < 0 || trajectory.Instance < 0 {
					return errors.New("negative trajectory frame or instance")
				}
				if err := insertQueryRow(stmt, trajectory.ActorID, trajectory.Instance, string(trajectory.Kind),
					trajectory.ObjectName, sample.Frame, sample.Time,
					sample.Position.X, sample.Position.Y, sample.Position.Z); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("load positions: %w", err)
	}
	return nil
}

func queryGroupFields(replay *rocket.Replay, name string, index int) (map[string]*rocket.Property, error) {
	property := replay.Properties[name]
	if property == nil || index < 0 || index >= len(property.Groups) || property.Groups[index] == nil {
		return nil, fmt.Errorf("missing %s property group %d", name, index)
	}
	return property.Groups[index].Properties, nil
}

func withQueryInsert(db *sqlite3.Conn, sqlText string, insert func(*sqlite3.Stmt) error) (err error) {
	stmt, tail, err := db.Prepare(sqlText)
	if err != nil {
		return err
	}
	if stmt == nil || tail != "" {
		if stmt != nil {
			if closeErr := stmt.Close(); closeErr != nil {
				return errors.Join(errors.New("invalid internal insert statement"), closeErr)
			}
		}
		return errors.New("invalid internal insert statement")
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	return insert(stmt)
}

func insertQueryRow(stmt *sqlite3.Stmt, values ...any) error {
	if stmt.BindCount() != len(values) {
		return errors.New("incorrect number of query data values")
	}
	for i, value := range values {
		if err := bindQueryValue(stmt, i+1, value); err != nil {
			return fmt.Errorf("bind column %d: %w", i+1, err)
		}
	}
	return stmt.Exec()
}

func bindQueryValue(stmt *sqlite3.Stmt, index int, value any) error {
	switch value := value.(type) {
	case nil:
		return stmt.BindNull(index)
	case *string:
		if value == nil {
			return stmt.BindNull(index)
		}
		return bindQueryValue(stmt, index, *value)
	case *int:
		if value == nil {
			return stmt.BindNull(index)
		}
		return bindQueryValue(stmt, index, *value)
	case *int32:
		if value == nil {
			return stmt.BindNull(index)
		}
		return bindQueryValue(stmt, index, *value)
	case *bool:
		if value == nil {
			return stmt.BindNull(index)
		}
		return bindQueryValue(stmt, index, *value)
	case string:
		if len(value) > maxQueryValueBytes || !utf8.ValidString(value) {
			return errors.New("text value exceeds the query limit or is not valid UTF-8")
		}
		return stmt.BindText(index, value)
	case int:
		return stmt.BindInt64(index, int64(value))
	case int32:
		return stmt.BindInt64(index, int64(value))
	case int64:
		return stmt.BindInt64(index, value)
	case bool:
		return stmt.BindBool(index, value)
	case float32:
		number := float64(value)
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return errors.New("non-finite query data value")
		}
		return stmt.BindFloat(index, number)
	default:
		return fmt.Errorf("unsupported query data type %T", value)
	}
}
