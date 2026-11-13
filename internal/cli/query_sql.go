package cli

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	sqlite3 "github.com/ncruces/go-sqlite3"
)

const (
	maxQueryColumns     = 128
	maxQueryRows        = 100000
	maxQueryCells       = 1000000
	maxQueryResultBytes = 16 << 20
	queryTimeout        = 30 * time.Second
)

type queryResult struct {
	SchemaVersion int      `json:"schema_version"`
	Columns       []string `json:"columns"`
	Rows          [][]any  `json:"rows"`
}

func executeQuery(db *sqlite3.Conn, decoded bool, sqlText string) (queryResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	return executeQueryContext(ctx, db, decoded, sqlText, maxQueryRows, maxQueryCells, maxQueryResultBytes)
}

func executeQueryContext(ctx context.Context, db *sqlite3.Conn, decoded bool, sqlText string, rowLimit, cellLimit, byteLimit int) (result queryResult, err error) {
	var denied string
	var sawSelect bool
	if err := db.SetAuthorizer(func(action sqlite3.AuthorizerActionCode, name, detail, schema, _ string) sqlite3.AuthorizerReturnCode {
		switch action {
		case sqlite3.AUTH_SELECT:
			sawSelect = true
			return sqlite3.AUTH_OK
		case sqlite3.AUTH_READ:
			// SQLite omits schema and column names for FROM references that do
			// not read a column, including CTEs. Internal tables remain blocked.
			noColumn := schema == "" && detail == "" && !strings.HasPrefix(strings.ToLower(name), "sqlite_")
			if (schema == "main" && queryTableAllowed(name)) || noColumn {
				return sqlite3.AUTH_OK
			}
			denied = fmt.Sprintf("access to table %q is not allowed", name)
		case sqlite3.AUTH_FUNCTION:
			if queryFunctionAllowed(detail) {
				return sqlite3.AUTH_OK
			}
			denied = fmt.Sprintf("SQL function %q is not allowed", detail)
		case sqlite3.AUTH_RECURSIVE:
			denied = "recursive SQL queries are not supported"
		default:
			denied = "only read-only queries over replay tables are allowed"
		}
		return sqlite3.AUTH_DENY
	}); err != nil {
		return queryResult{}, fmt.Errorf("install query authorizer: %w", err)
	}
	db.SetInterrupt(ctx)
	defer db.SetInterrupt(context.Background())
	stmt, tail, err := db.PrepareFlags(sqlText, sqlite3.PREPARE_NO_VTAB)
	if err != nil {
		if denied != "" {
			return queryResult{}, errors.New(denied)
		}
		if !decoded && errors.Is(err, sqlite3.ERROR) {
			message := strings.TrimPrefix(err.Error(), sqlite3.ERROR.Error()+": ")
			if strings.EqualFold(message, "no such table: positions") ||
				strings.EqualFold(message, "no such table: main.positions") {
				return queryResult{}, errors.New("querying positions requires --decode-network")
			}
		}
		if ctx.Err() != nil {
			return queryResult{}, fmt.Errorf("query interrupted: %w", ctx.Err())
		}
		return queryResult{}, fmt.Errorf("prepare SQL: %w", safeQueryError{err})
	}
	if stmt == nil {
		return queryResult{}, errors.New("SQL must contain one read-only query")
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close query: %w", safeQueryError{closeErr}))
		}
	}()
	if !onlySQLComments(tail) {
		return queryResult{}, errors.New("SQL must contain exactly one statement")
	}
	if !sawSelect || !stmt.ReadOnly() || stmt.ColumnCount() == 0 {
		return queryResult{}, errors.New("SQL must be a read-only query")
	}
	if stmt.BindCount() != 0 {
		return queryResult{}, errors.New("SQL parameters are not supported")
	}
	columns := stmt.ColumnCount()
	if columns > maxQueryColumns {
		return queryResult{}, fmt.Errorf("query exceeds the %d-column limit", maxQueryColumns)
	}
	result = queryResult{SchemaVersion: 1, Columns: make([]string, columns), Rows: make([][]any, 0)}
	bytesUsed, cells := 0, 0
	for i := range result.Columns {
		name := stmt.ColumnName(i)
		if !utf8.ValidString(name) || len(name) > maxQueryValueBytes || len(name) > byteLimit-bytesUsed {
			return queryResult{}, errors.New("query column names exceed the result limit or are not valid UTF-8")
		}
		bytesUsed += len(name)
		result.Columns[i] = name
	}
	for stmt.Step() {
		if len(result.Rows) >= rowLimit {
			return queryResult{}, fmt.Errorf("query exceeds the %d-row result limit", rowLimit)
		}
		if columns > cellLimit-cells {
			return queryResult{}, fmt.Errorf("query exceeds the %d-cell result limit", cellLimit)
		}
		cells += columns
		row := make([]any, columns)
		for i := range row {
			value, size, valueErr := queryColumn(stmt, i, byteLimit-bytesUsed)
			if valueErr != nil {
				return queryResult{}, valueErr
			}
			bytesUsed += size
			row[i] = value
		}
		result.Rows = append(result.Rows, row)
	}
	if err := stmt.Err(); err != nil {
		if ctx.Err() != nil {
			return queryResult{}, fmt.Errorf("query interrupted: %w", ctx.Err())
		}
		return queryResult{}, fmt.Errorf("execute SQL: %w", safeQueryError{err})
	}
	return result, nil
}

func queryColumn(stmt *sqlite3.Stmt, column, remaining int) (any, int, error) {
	switch stmt.ColumnType(column) {
	case sqlite3.NULL:
		if remaining < 1 {
			return nil, 0, errors.New("query exceeds the result byte limit")
		}
		return nil, 1, nil
	case sqlite3.INTEGER:
		if remaining < 8 {
			return nil, 0, errors.New("query exceeds the result byte limit")
		}
		return stmt.ColumnInt64(column), 8, nil
	case sqlite3.FLOAT:
		if remaining < 8 {
			return nil, 0, errors.New("query exceeds the result byte limit")
		}
		value := stmt.ColumnFloat(column)
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, 0, errors.New("query returned a non-finite number")
		}
		return value, 8, nil
	case sqlite3.TEXT:
		value := stmt.ColumnRawText(column)
		if err := stmt.Err(); err != nil {
			return nil, 0, fmt.Errorf("read query value: %w", safeQueryError{err})
		}
		if len(value) > maxQueryValueBytes || len(value) > remaining {
			return nil, 0, errors.New("query exceeds the result byte limit")
		}
		if !utf8.Valid(value) {
			return nil, 0, errors.New("query returned text that is not valid UTF-8")
		}
		return string(value), len(value), nil
	case sqlite3.BLOB:
		value := stmt.ColumnRawBlob(column)
		if err := stmt.Err(); err != nil {
			return nil, 0, fmt.Errorf("read query value: %w", safeQueryError{err})
		}
		if len(value) > maxQueryValueBytes || len(value) > remaining {
			return nil, 0, errors.New("query exceeds the result byte limit")
		}
		copyOfValue := make([]byte, len(value))
		copy(copyOfValue, value)
		return copyOfValue, len(value), nil
	default:
		return nil, 0, errors.New("query returned an unsupported SQLite value")
	}
}

func queryTableAllowed(name string) bool {
	switch strings.ToLower(name) {
	case "match", "teams", "players", "goals", "highlights", "positions":
		return true
	default:
		return false
	}
}

func queryFunctionAllowed(name string) bool {
	switch strings.ToLower(name) {
	case "abs", "avg", "char", "coalesce", "count", "date", "datetime",
		"glob", "group_concat", "hex", "ifnull", "iif", "instr", "julianday",
		"length", "like", "likelihood", "likely", "lower", "ltrim", "max", "min",
		"nullif", "quote", "replace", "round", "rtrim", "strftime",
		"string_agg", "substr", "substring", "sum", "time", "total", "trim",
		"typeof", "unicode", "unixepoch", "unlikely", "upper":
		return true
	default:
		return false
	}
}

func onlySQLComments(tail string) bool {
	for {
		tail = strings.TrimSpace(tail)
		switch {
		case tail == "":
			return true
		case strings.HasPrefix(tail, "--"):
			end := strings.IndexByte(tail, '\n')
			if end < 0 {
				return true
			}
			tail = tail[end+1:]
		case strings.HasPrefix(tail, "/*"):
			end := strings.Index(tail[2:], "*/")
			if end < 0 {
				return false
			}
			tail = tail[end+4:]
		default:
			return false
		}
	}
}

type safeQueryError struct{ err error }

func (e safeQueryError) Error() string {
	var result strings.Builder
	for _, char := range e.err.Error() {
		if unicode.IsPrint(char) {
			result.WriteRune(char)
		} else {
			quoted := strconv.QuoteRune(char)
			result.WriteString(quoted[1 : len(quoted)-1])
		}
	}
	return result.String()
}

func (e safeQueryError) Unwrap() error { return e.err }
