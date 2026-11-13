package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestQueryOutputFormats(t *testing.T) {
	result := queryResult{
		SchemaVersion: 1,
		Columns:       []string{"same", "same", "empty", "control\x1b", "blob"},
		Rows: [][]any{
			{nil, int64(0), "", "hello\n\x1b[31m", []byte{0xfa}},
			{"NULL", int64(-1), "text", "ok", []byte{}},
		},
	}
	t.Run("json", func(t *testing.T) {
		var out bytes.Buffer
		if err := writeQueryResult(&out, result, "json"); err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			SchemaVersion int      `json:"schema_version"`
			Columns       []string `json:"columns"`
			Rows          [][]any  `json:"rows"`
		}
		if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}
		want := [][]any{{nil, float64(0), "", "hello\n\x1b[31m", "+g=="},
			{"NULL", float64(-1), "text", "ok", ""}}
		if decoded.SchemaVersion != 1 || !reflect.DeepEqual(decoded.Columns, result.Columns) ||
			!reflect.DeepEqual(decoded.Rows, want) {
			t.Fatalf("unexpected JSON output: %#v", decoded)
		}
	})
	t.Run("table", func(t *testing.T) {
		var out bytes.Buffer
		if err := writeQueryResult(&out, result, "table"); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		if strings.ContainsRune(text, '\x1b') || strings.Count(text, "\n") != 3 ||
			!strings.Contains(text, `control\x1b`) || !strings.Contains(text, `hello\n\x1b[31m`) ||
			!strings.Contains(text, `NULL`) || !strings.Contains(text, `"NULL"`) || !strings.Contains(text, `""`) {
			t.Fatalf("unsafe or ambiguous table output: %q", text)
		}
	})
	t.Run("csv", func(t *testing.T) {
		var out bytes.Buffer
		if err := writeQueryResult(&out, result, "csv"); err != nil {
			t.Fatal(err)
		}
		records, err := csv.NewReader(&out).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		want := [][]string{
			{"same", "same", "empty", "control\x1b", "blob"},
			{"", "0", "", "hello\n\x1b[31m", "+g=="},
			{"NULL", "-1", "text", "ok", ""},
		}
		if !reflect.DeepEqual(records, want) {
			t.Fatalf("CSV %#v, want %#v", records, want)
		}
	})
}

type shortQueryWriter struct{}

func TestQueryCSVSingleEmptyField(t *testing.T) {
	for _, column := range []string{"x", ""} {
		t.Run("column="+column, func(t *testing.T) {
			result := queryResult{
				SchemaVersion: 1,
				Columns:       []string{column},
				Rows:          [][]any{{nil}, {""}, {[]byte{}}, {"text"}, {nil}},
			}
			var out bytes.Buffer
			if err := writeQueryResult(&out, result, "csv"); err != nil {
				t.Fatal(err)
			}
			records, err := csv.NewReader(&out).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			want := [][]string{{column}, {""}, {""}, {""}, {"text"}, {""}}
			if !reflect.DeepEqual(records, want) {
				t.Fatalf("CSV %#v, want %#v", records, want)
			}
		})
	}
}

func (shortQueryWriter) Write(data []byte) (int, error) { return len(data) / 2, nil }

func TestQueryOutputFailures(t *testing.T) {
	result := queryResult{SchemaVersion: 1, Columns: []string{"x"}, Rows: [][]any{{int64(1)}}}
	if err := writeQueryResult(failingWriter{}, result, "json"); !errors.Is(err, errWrite) {
		t.Fatalf("writer error: %v", err)
	}
	if err := writeQueryResult(shortQueryWriter{}, result, "json"); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write: %v", err)
	}
	result.Rows[0][0] = make(chan int)
	var out bytes.Buffer
	if err := writeQueryResult(&out, result, "json"); err == nil || out.Len() != 0 {
		t.Fatalf("encoding error %v, output %q", err, &out)
	}

	large := strings.Repeat("\x00", maxQueryValueBytes)
	result.Columns = []string{"a", "b", "c", "d", "e", "f"}
	result.Rows = [][]any{{large, large, large, large, large, large}}
	if err := writeQueryResult(&out, result, "json"); err == nil ||
		!strings.Contains(err.Error(), "output byte limit") || out.Len() != 0 {
		t.Fatalf("output limit error %v, output length %d", err, out.Len())
	}
}
