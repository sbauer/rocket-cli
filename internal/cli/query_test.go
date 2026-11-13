package cli

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	sqlite3 "github.com/ncruces/go-sqlite3"
	rocket "github.com/sbauer/go-rocket"
	"github.com/sbauer/go-rocket/parsers"
)

func TestQueryFixture(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want queryResult
	}{
		{
			name: "metadata and join",
			args: []string{"--sql", `SELECT p.name, p.goals, p.online_id, m.map
				FROM players AS p CROSS JOIN match AS m ORDER BY p.name`},
			want: queryResult{SchemaVersion: 1, Columns: []string{"name", "goals", "online_id", "map"},
				Rows: [][]any{{"Drogings", float64(0), "76561198052527453", "UF_Day_P"}}},
		},
		{
			name: "aggregate",
			args: []string{"--sql", "SELECT count(*) AS players FROM players"},
			want: queryResult{SchemaVersion: 1, Columns: []string{"players"}, Rows: [][]any{{float64(1)}}},
		},
		{
			name: "empty with duplicate columns",
			args: []string{"--sql", "SELECT name AS value, goals AS value FROM players WHERE 0"},
			want: queryResult{SchemaVersion: 1, Columns: []string{"value", "value"}, Rows: [][]any{}},
		},
		{
			name: "blob and null",
			args: []string{"--sql", "SELECT x'' AS empty_blob, x'fa' AS blob, NULL AS missing, '' AS empty"},
			want: queryResult{SchemaVersion: 1, Columns: []string{"empty_blob", "blob", "missing", "empty"},
				Rows: [][]any{{"", "+g==", nil, ""}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"query", "--format", "json"}, tt.args...)
			args = append(args, fixturePath)
			var out, errOut bytes.Buffer
			if code := Run(args, &out, &errOut); code != 0 {
				t.Fatalf("exit %d, stderr %s", code, &errOut)
			}
			var result queryResult
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result, tt.want) || errOut.Len() != 0 {
				t.Fatalf("result %#v, want %#v; stderr %s", result, tt.want, &errOut)
			}
		})
	}
}

func TestQueryTypedReplayValues(t *testing.T) {
	replay := &rocket.Replay{Properties: map[string]*rocket.Property{
		"MapName": {Value: "Stadium"},
		"PlayerStats": {Type: "ArrayProperty", Groups: []*rocket.PropertyGroup{{Properties: map[string]*rocket.Property{
			"Name": {Value: "Alice"}, "Team": {Value: int32(1)},
			"Score": {Value: int32(0)}, "OnlineID": {Value: uint64(math.MaxUint64)},
		}}}},
		"Goals": {Type: "ArrayProperty", Groups: []*rocket.PropertyGroup{{Properties: map[string]*rocket.Property{
			"frame": {Value: int32(17)}, "PlayerName": {Value: "Alice"}, "PlayerTeam": {Value: int32(1)},
		}}}},
		"HighLights": {Type: "ArrayProperty", Groups: []*rocket.PropertyGroup{{Properties: map[string]*rocket.Property{
			"frame": {Value: int32(19)}, "CarName": {Value: "car"}, "BallName": {Value: "ball"},
		}}}},
	}}
	match, err := replay.MatchData()
	if err != nil {
		t.Fatal(err)
	}
	result, err := runQuery(replay, match, nil, false, `SELECT
		p.name, p.score, p.goals, p.online_id, p.team,
		m.team_size, m.team0_score, m.team1_score, m.team1_score_derived, m.winner, m.winner_derived,
		t.name, t.score, t.score_derived, t.is_winner, t.winner_derived,
		g.frame, g.player, g.team, h.frame, h.car, h.ball, h.goal_actor
		FROM players p CROSS JOIN match m JOIN teams t ON p.team = t.team
		JOIN goals g ON g.player = p.name CROSS JOIN highlights h`)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]any{{"Alice", int64(0), nil, "18446744073709551615", int64(1),
		nil, int64(0), int64(1), int64(1), int64(1), int64(1),
		"Orange", int64(1), int64(1), int64(1), int64(1),
		int64(17), "Alice", int64(1), int64(19), "car", "ball", nil}}
	if !reflect.DeepEqual(result.Rows, want) {
		t.Fatalf("rows %#v, want %#v", result.Rows, want)
	}
	var out bytes.Buffer
	if err := writeQueryResult(&out, result, "json"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"18446744073709551615"`) {
		t.Fatalf("unsigned ID lost precision: %s", &out)
	}
}

func TestQueryNetworkDecodeIsOptIn(t *testing.T) {
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
	for _, tt := range []struct {
		name, sql string
		options   []string
		code      int
		want      string
	}{
		{"metadata", "SELECT name FROM players", nil, 0, "Drogings"},
		{"position guard", "SELECT count(*) FROM positions", nil, 1, "requires --decode-network"},
		{"decode failure", "SELECT name FROM players", []string{"--decode-network"}, 1, "network:"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"query", "--crc", "never", "--sql", tt.sql}
			args = append(args, tt.options...)
			args = append(args, path)
			var out, errOut bytes.Buffer
			if code := Run(args, &out, &errOut); code != tt.code {
				t.Fatalf("exit %d, want %d; stderr %s", code, tt.code, &errOut)
			}
			if tt.code == 0 {
				if errOut.Len() != 0 || !strings.Contains(out.String(), tt.want) {
					t.Fatalf("stdout %q, stderr %q", &out, &errOut)
				}
			} else if out.Len() != 0 || !strings.Contains(errOut.String(), tt.want) {
				t.Fatalf("stdout %q, stderr %q", &out, &errOut)
			}
		})
	}
	var out, errOut bytes.Buffer
	args := []string{"query", "--decode-network", "--format", "json", "--sql",
		"SELECT kind, count(*) FROM positions GROUP BY kind ORDER BY kind", fixturePath}
	if code := Run(args, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr %s", code, &errOut)
	}
	var result queryResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 2 || result.Rows[0][0] != "ball" || result.Rows[1][0] != "car" ||
		result.Rows[0][1].(float64) <= 0 || result.Rows[1][1].(float64) <= 0 {
		t.Fatalf("unexpected position samples: %#v", result.Rows)
	}
}

func TestQueryRejectsOversizedNetworkBeforeDecode(t *testing.T) {
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parseQueryReplay(fixturePath, parsers.CRCValidationAlways, false)
	if err != nil {
		t.Fatal(err)
	}
	offset := bytes.Index(data, replay.NetworkData)
	if offset < 4 || len(replay.NetworkData) == 0 ||
		int(binary.LittleEndian.Uint32(data[offset-4:offset])) != len(replay.NetworkData) {
		t.Fatal("cannot locate fixture network section")
	}
	body := 8 + int(binary.LittleEndian.Uint32(data[:4]))
	delta := maxQueryNetworkBytes + 1 - len(replay.NetworkData)
	if delta <= 0 || body+4 > offset-4 {
		t.Fatal("unexpected fixture body layout")
	}
	enlarged := make([]byte, len(data)+delta)
	copy(enlarged, data[:offset])
	copy(enlarged[offset+maxQueryNetworkBytes+1:], data[offset+len(replay.NetworkData):])
	binary.LittleEndian.PutUint32(enlarged[offset-4:offset], maxQueryNetworkBytes+1)
	bodySize := binary.LittleEndian.Uint32(data[body : body+4])
	binary.LittleEndian.PutUint32(enlarged[body:body+4], bodySize+uint32(delta))
	path := filepath.Join(t.TempDir(), "oversized-network.replay")
	if err := os.WriteFile(path, enlarged, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run([]string{"query", "--crc", "never", "--decode-network", "--sql", "SELECT 1", path}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, stderr %s", code, &errOut)
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "network data") || !strings.Contains(errOut.String(), "query limit") {
		t.Fatalf("stdout %q, stderr %q", &out, &errOut)
	}
}

func queryTestDB(t *testing.T) *sqlite3.Conn {
	t.Helper()
	db, err := sqlite3.OpenFlags(":memory:", sqlite3.OPEN_READWRITE|sqlite3.OPEN_CREATE|sqlite3.OPEN_MEMORY|sqlite3.OPEN_PRIVATECACHE)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(querySchema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close query database: %v", err)
		}
	})
	return db
}

func TestQuerySQLRestrictions(t *testing.T) {
	db := queryTestDB(t)
	tests := []string{
		"INSERT INTO players (name) VALUES ('x')",
		"UPDATE players SET name = 'x'",
		"DELETE FROM players",
		"CREATE TABLE evil (x)",
		"DROP TABLE players",
		"ATTACH DATABASE ':memory:' AS evil",
		"DETACH DATABASE main",
		"PRAGMA query_only = OFF",
		"SELECT load_extension('evil')",
		"SELECT readfile('/etc/passwd')",
		"SELECT writefile('evil', 'data')",
		"SELECT * FROM sqlite_schema",
		"SELECT 1 FROM sqlite_schema",
		"SELECT 1 FROM sqlite_temp_schema",
		"SELECT * FROM pragma_table_info('players')",
		"SELECT random()",
		"SELECT printf('%10000000s','x')",
		"SELECT format('%10000000s','x')",
		"SELECT 1; SELECT 2",
		"SELECT 1; /* comment */ DELETE FROM players",
		"SELECT ?1",
		"WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c WHERE x<3) SELECT * FROM c",
	}
	for _, sqlText := range tests {
		t.Run(sqlText, func(t *testing.T) {
			if _, err := executeQuery(db, false, sqlText); err == nil {
				t.Fatalf("accepted prohibited SQL %q", sqlText)
			}
		})
	}
	for _, sqlText := range []string{
		"SELECT count(*) FROM players",
		"SELECT 1; /* comment */ -- comment",
		"WITH c(x) AS (SELECT 1) SELECT x FROM c",
		"WITH c(x) AS (SELECT 1) SELECT 1 FROM c",
	} {
		if _, err := executeQuery(db, false, sqlText); err != nil {
			t.Errorf("rejected safe SQL %q: %v", sqlText, err)
		}
	}
	for _, sqlText := range []string{
		"SELECT count(*) FROM positions",
		"SELECT EXISTS(SELECT 1 FROM positions)",
		"WITH p AS (SELECT * FROM positions) SELECT count(*) FROM p",
	} {
		if _, err := executeQuery(db, false, sqlText); err == nil ||
			!strings.Contains(err.Error(), "requires --decode-network") {
			t.Errorf("%s without decode: %v", sqlText, err)
		}
	}
	if err := db.SetAuthorizer(nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(queryPositionsSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := executeQuery(db, true, "SELECT count(*) FROM positions"); err != nil {
		t.Fatalf("positions with decode: %v", err)
	}
}

func TestQueryLimitsAndSQLFailures(t *testing.T) {
	db := queryTestDB(t)
	for _, tt := range []struct {
		name, sql, want   string
		rows, cells, size int
	}{
		{"rows", "SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3", "row result limit", 2, 100, 1000},
		{"cells", "SELECT 1,2 UNION ALL SELECT 3,4", "cell result limit", 10, 3, 1000},
		{"bytes", "SELECT '12345' AS x", "result byte limit", 10, 100, 4},
		{"nonfinite", "SELECT 1e999 AS x", "non-finite", 10, 100, 1000},
		{"invalid UTF-8", "SELECT CAST(X'ff' AS TEXT) AS x", "not valid UTF-8", 10, 100, 1000},
		{"integer sum overflow", "SELECT sum(x) FROM (SELECT 9223372036854775807 AS x UNION ALL SELECT 1)", "overflow", 10, 100, 1000},
		{"columns", "SELECT " + strings.Repeat("1,", maxQueryColumns) + "1", "column limit", 10, 1000, 10000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executeQueryContext(context.Background(), db, false, tt.sql, tt.rows, tt.cells, tt.size)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %v, want %q", err, tt.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := executeQueryContext(ctx, db, false, "SELECT 1", 10, 10, 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled query: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	expensive := `WITH n(x) AS (SELECT 0 UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL
		SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL
		SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9)
		SELECT sum(a.x) FROM n a, n b, n c, n d, n e, n f`
	if _, err := executeQueryContext(ctx, db, false, expensive, 10, 10, 1000); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("long query was not interrupted: %v", err)
	}
	largeReplacement := "SELECT replace('" + strings.Repeat("a", 2048) + "','a','" + strings.Repeat("b", 1024) + "')"
	if result, err := runQuery(&rocket.Replay{}, rocket.MatchData{}, nil, false, largeReplacement); err == nil {
		t.Fatalf("oversized replacement returned success: %#v", result.Rows)
	}
}

func TestQueryResourceLimitPanic(t *testing.T) {
	ctes := []string{"t0(x) AS (SELECT 1)"}
	for i := 1; i <= 12; i++ {
		ctes = append(ctes, fmt.Sprintf("t%d(x) AS (SELECT x FROM t%d UNION ALL SELECT x FROM t%d)", i, i-1, i-1))
	}
	sqlText := "WITH " + strings.Join(ctes, ", ") + " SELECT count(*) FROM t12"
	result, err := runQuery(&rocket.Replay{}, rocket.MatchData{}, nil, false, sqlText)
	if err == nil || !strings.Contains(err.Error(), "resource limit") || len(result.Rows) != 0 {
		t.Fatalf("resource limit result %#v, error %v", result, err)
	}
	var out, errOut bytes.Buffer
	if code := Run([]string{"query", "--sql", sqlText, fixturePath}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1; stderr %s", code, &errOut)
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "resource limit") {
		t.Fatalf("stdout %q, stderr %q", &out, &errOut)
	}
	if _, err := runQuery(&rocket.Replay{}, rocket.MatchData{}, nil, false, "SELECT 1"); err != nil {
		t.Fatalf("query after resource limit: %v", err)
	}
}

func TestQueryPreservesUnrelatedPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("runQuery swallowed an unrelated panic")
		}
	}()
	runQuery(nil, rocket.MatchData{}, nil, false, "SELECT 1")
}

func TestQueryPositionsCTEShadowing(t *testing.T) {
	for _, decoded := range []bool{false, true} {
		for _, sqlText := range []string{
			"WITH positions(x) AS (SELECT 1) SELECT count(*) FROM positions",
			"WITH positions(x) AS (SELECT 1) SELECT x FROM positions",
			"WITH positions(x) AS (SELECT 1) SELECT 1 FROM positions",
		} {
			t.Run(fmt.Sprintf("decoded=%t/%s", decoded, sqlText), func(t *testing.T) {
				result, err := runQuery(&rocket.Replay{}, rocket.MatchData{}, nil, decoded, sqlText)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(result.Rows, [][]any{{int64(1)}}) {
					t.Fatalf("unexpected result: %#v", result)
				}
			})
		}
	}
	for _, sqlText := range []string{
		"SELECT count(*) FROM positions",
		"SELECT frame FROM main.positions",
		`SELECT count(*) FROM "MAIN"."PoSiTiOnS"`,
		"WITH positions(x) AS (SELECT 1) SELECT count(*) FROM main.positions",
		"WITH p AS (SELECT * FROM positions) SELECT count(*) FROM p",
	} {
		t.Run(sqlText, func(t *testing.T) {
			if _, err := runQuery(&rocket.Replay{}, rocket.MatchData{}, nil, false, sqlText); err == nil ||
				!strings.Contains(err.Error(), "requires --decode-network") {
				t.Fatalf("positions without decode: %v", err)
			}
		})
	}
}

func TestQueryCommandErrors(t *testing.T) {
	dir := t.TempDir()
	large := filepath.Join(dir, "large.replay")
	file, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxQueryReplayBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	header := filepath.Join(dir, "header.replay")
	if err := os.WriteFile(header, []byte{1, 0, 128, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(dir, "must-not-exist.db")
	manyRows := `WITH n(x) AS (SELECT 0 UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL
		SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL
		SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9)
		SELECT 1 FROM n a, n b, n c, n d, n e, n f`
	for _, tt := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"missing SQL", []string{"query", fixturePath}, 2, "requires --sql"},
		{"invalid format", []string{"query", "--sql", "SELECT 1", "--format", "xml", fixturePath}, 2, "invalid --format"},
		{"long SQL", []string{"query", "--sql", strings.Repeat("x", maxQuerySQLBytes+1), fixturePath}, 2, "SQL exceeds"},
		{"invalid UTF-8", []string{"query", "--sql", "SELECT '\xff'", fixturePath}, 2, "valid UTF-8"},
		{"NUL", []string{"query", "--sql", "SELECT 1\x00", fixturePath}, 2, "without NUL"},
		{"directory", []string{"query", "--sql", "SELECT 1", dir}, 1, "regular file"},
		{"large replay", []string{"query", "--sql", "SELECT 1", large}, 1, "input limit"},
		{"large header", []string{"query", "--sql", "SELECT 1", header}, 1, "header limit"},
		{"multiple statements", []string{"query", "--sql", "SELECT 1; SELECT 2", fixturePath}, 1, "exactly one statement"},
		{"too many rows", []string{"query", "--sql", manyRows, fixturePath}, 1, "row result limit"},
		{"attach file", []string{"query", "--sql", fmt.Sprintf("ATTACH DATABASE '%s' AS evil", database), fixturePath}, 1, "read-only"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := Run(tt.args, &out, &errOut); code != tt.code {
				t.Fatalf("exit %d, want %d; stderr %s", code, tt.code, &errOut)
			}
			if out.Len() != 0 || !strings.Contains(errOut.String(), tt.want) {
				t.Fatalf("stdout %q, stderr %q", &out, &errOut)
			}
		})
	}
	if _, err := os.Stat(database); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ATTACH created or accessed external file: %v", err)
	}

	path := filepath.Join(dir, "missing\x1b[31m.replay")
	var out, errOut bytes.Buffer
	if code := Run([]string{"query", "--sql", "SELECT 1", path}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, stderr %s", code, &errOut)
	}
	if strings.ContainsRune(errOut.String(), '\x1b') || !strings.Contains(errOut.String(), `\x1b`) {
		t.Fatalf("unsafe diagnostic %q", &errOut)
	}
	if err := run([]string{"query", "--sql", "SELECT 1", path}, io.Discard); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrapped file error: %v", err)
	}
}

func TestQueryTrajectoryBudgets(t *testing.T) {
	network := &rocket.Replay{NetworkData: make([]byte, maxQueryNetworkBytes+1)}
	if err := checkQueryDecodeBudget(network, "test"); err == nil || !strings.Contains(err.Error(), "network data") {
		t.Fatalf("network byte limit: %v", err)
	}
	network.NetworkData = nil
	network.Properties = map[string]*rocket.Property{"NumFrames": {Value: int32(maxQueryFrames + 1)}}
	if err := checkQueryDecodeBudget(network, "test"); err == nil || !strings.Contains(err.Error(), "frame query limit") {
		t.Fatalf("frame limit: %v", err)
	}
	network.Properties["NumFrames"].Value = int32(maxQueryFrames)
	if err := checkQueryDecodeBudget(network, "test"); err != nil {
		t.Fatalf("exact frame limit: %v", err)
	}

	replay := &rocket.Replay{Frames: []rocket.NetworkFrame{{Actors: []rocket.ActorReplication{{
		Spawn: &rocket.SpawnedActor{Location: &rocket.Vector3i{}},
		Attributes: []rocket.AttributeUpdate{
			{Value: rocket.RigidBodyState{}}, {Value: int32(1)},
		},
	}}}}}
	if err := checkTrajectoryBudget(replay, 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := checkTrajectoryBudget(replay, 0, 2); err == nil || !strings.Contains(err.Error(), "actor-event") {
		t.Fatalf("actor limit: %v", err)
	}
	if err := checkTrajectoryBudget(replay, 1, 1); err == nil || !strings.Contains(err.Error(), "position") {
		t.Fatalf("position limit: %v", err)
	}

	bad := []rocket.ActorTrajectory{{Samples: []rocket.PositionSample{{Position: rocket.Vector3f{X: float32(math.Inf(1))}}}}}
	if _, err := runQuery(&rocket.Replay{}, rocket.MatchData{}, bad, true, "SELECT 1"); err == nil ||
		!strings.Contains(err.Error(), "non-finite") {
		t.Fatalf("non-finite trajectory: %v", err)
	}
	match := rocket.MatchData{PlayerStats: []rocket.PlayerStat{{Name: "orphan"}}}
	if _, err := runQuery(&rocket.Replay{}, match, nil, false, "SELECT name FROM players"); err == nil ||
		!strings.Contains(err.Error(), "missing PlayerStats property group") {
		t.Fatalf("mismatched metadata: %v", err)
	}
}
