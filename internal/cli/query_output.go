package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"
)

const maxQueryOutputBytes = 32 << 20

type queryBuffer struct{ data bytes.Buffer }

func (b *queryBuffer) Write(p []byte) (int, error) {
	if len(p) > maxQueryOutputBytes-b.data.Len() {
		return 0, errors.New("query exceeds the output byte limit")
	}
	return b.data.Write(p)
}

func writeQueryResult(out io.Writer, result queryResult, format string) error {
	var buf queryBuffer
	var err error
	switch format {
	case "table":
		err = writeQueryTable(&buf, result)
	case "json":
		err = writeQueryJSON(&buf, result)
	case "csv":
		err = writeQueryCSV(&buf, result)
	default:
		return fmt.Errorf("unsupported query format %q", format)
	}
	if err != nil {
		return fmt.Errorf("format query output: %w", err)
	}
	n, err := out.Write(buf.data.Bytes())
	if err == nil && n != buf.data.Len() {
		err = io.ErrShortWrite
	}
	if err != nil {
		return fmt.Errorf("write query output: %w", err)
	}
	return nil
}

func writeQueryTable(out io.Writer, result queryResult) error {
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	written := 0
	writeLine := func(values []any) error {
		for i, value := range values {
			cell, err := queryText(value, true)
			if err != nil {
				return err
			}
			if len(cell) > maxQueryOutputBytes-written-1 {
				return errors.New("query exceeds the output byte limit")
			}
			written += len(cell) + 1
			if i > 0 {
				if _, err := io.WriteString(table, "\t"); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(table, cell); err != nil {
				return err
			}
		}
		_, err := io.WriteString(table, "\n")
		return err
	}
	header := make([]any, len(result.Columns))
	for i, name := range result.Columns {
		header[i] = name
	}
	if err := writeLine(header); err != nil {
		return err
	}
	for _, row := range result.Rows {
		if err := writeLine(row); err != nil {
			return err
		}
	}
	return table.Flush()
}

func writeQueryJSON(out io.Writer, result queryResult) error {
	write := func(s string) error {
		_, err := io.WriteString(out, s)
		return err
	}
	value := func(v any) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	}
	if err := write(`{"schema_version":`); err != nil {
		return err
	}
	if err := value(result.SchemaVersion); err != nil {
		return err
	}
	if err := write(`,"columns":[`); err != nil {
		return err
	}
	for i, name := range result.Columns {
		if i > 0 {
			if err := write(","); err != nil {
				return err
			}
		}
		if err := value(name); err != nil {
			return err
		}
	}
	if err := write(`],"rows":[`); err != nil {
		return err
	}
	for i, row := range result.Rows {
		if i > 0 {
			if err := write(","); err != nil {
				return err
			}
		}
		if err := write("["); err != nil {
			return err
		}
		for j, cell := range row {
			if j > 0 {
				if err := write(","); err != nil {
					return err
				}
			}
			if err := value(cell); err != nil {
				return err
			}
		}
		if err := write("]"); err != nil {
			return err
		}
	}
	return write("]}\n")
}

func writeQueryCSV(out io.Writer, result queryResult) error {
	writer := csv.NewWriter(out)
	writeRecord := func(fields []string) error {
		if len(fields) == 1 && fields[0] == "" {
			// CSV readers skip the bare newline emitted by csv.Writer here.
			writer.Flush()
			if err := writer.Error(); err != nil {
				return err
			}
			_, err := io.WriteString(out, "\"\"\n")
			return err
		}
		return writer.Write(fields)
	}
	if err := writeRecord(result.Columns); err != nil {
		return err
	}
	for _, row := range result.Rows {
		fields := make([]string, len(row))
		for i, value := range row {
			text, err := queryText(value, false)
			if err != nil {
				return err
			}
			fields[i] = text
		}
		if err := writeRecord(fields); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func queryText(value any, table bool) (string, error) {
	switch value := value.(type) {
	case nil:
		if table {
			return "NULL", nil
		}
		return "", nil
	case int64:
		return strconv.FormatInt(value, 10), nil
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64), nil
	case string:
		if table {
			if value == "" {
				return `""`, nil
			}
			if value == "NULL" {
				return `"NULL"`, nil
			}
			quoted := strconv.Quote(value)
			return quoted[1 : len(quoted)-1], nil
		}
		return value, nil
	case []byte:
		encoded := base64.StdEncoding.EncodeToString(value)
		if table && encoded == "NULL" {
			return `"NULL"`, nil
		}
		return encoded, nil
	default:
		return "", fmt.Errorf("unsupported query output type %T", value)
	}
}
