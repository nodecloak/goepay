// Package transferimport parses untrusted legacy five-column payment drafts.
// It never creates a batch, reserves funds, persists recipients or calls a provider.
package transferimport

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"goepay/internal/ledger"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxFileBytes = 512 * 1024

type Row struct {
	SourceRow int    `json:"source_row"`
	Serial    string `json:"serial"`
	Account   string `json:"account"`
	Name      string `json:"name"`
	Amount    string `json:"amount"`
	Remark    string `json:"remark"`
}
type Draft struct {
	Rows       []Row
	TotalCents int64
}

// Error contains only a stable diagnostic and source position, never cell values.
type Error struct {
	Row  int
	Code string
}

func (e *Error) Error() string {
	if e.Row > 0 {
		return fmt.Sprintf("row %d: %s", e.Row, e.Code)
	}
	return e.Code
}
func invalid(row int, code string) error { return &Error{row, code} }

func Parse(format string, raw []byte) (Draft, error) {
	if len(raw) == 0 || len(raw) > MaxFileBytes {
		return Draft{}, invalid(0, "file_size_limit")
	}
	var table [][]string
	var positions []int
	var err error
	switch format {
	case "csv":
		table, positions, err = parseCSV(raw)
	case "xlsx":
		table, positions, err = parseXLSX(raw)
	default:
		return Draft{}, invalid(0, "format_unsupported_use_csv_or_xlsx")
	}
	if err != nil {
		return Draft{}, err
	}
	return validate(table, positions)
}
func parseCSV(raw []byte) ([][]string, []int, error) {
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(raw) {
		return nil, nil, invalid(0, "utf8_required")
	}
	r := csv.NewReader(bytes.NewReader(raw))
	r.FieldsPerRecord = 5
	var table [][]string
	var positions []int
	for {
		row, e := r.Read()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, nil, invalid(len(table)+1, "invalid_csv_five_columns")
		}
		line, _ := r.FieldPos(0)
		if len(table) >= 101 {
			return nil, nil, invalid(line, "maximum_100_rows")
		}
		table = append(table, row)
		positions = append(positions, line)
	}
	return table, positions, nil
}
func validate(table [][]string, positions []int) (Draft, error) {
	if len(table) == 0 {
		return Draft{}, invalid(0, "header_required")
	}
	want := []string{"序号", "收款账号", "收款人姓名", "转账金额（元）", "转账备注"}
	if len(table[0]) != 5 {
		return Draft{}, invalid(1, "header_required")
	}
	for i, v := range want {
		if strings.TrimSpace(table[0][i]) != v {
			return Draft{}, invalid(1, "header_required")
		}
	}
	out := Draft{Rows: []Row{}}
	seen := map[string]bool{}
	for i, row := range table[1:] {
		line := positions[i+1]
		if len(row) != 5 {
			return Draft{}, invalid(line, "five_columns_required")
		}
		for n := range row {
			row[n] = strings.TrimSpace(row[n])
		}
		if row[1] == "" && row[2] == "" && row[3] == "" && row[4] == "" {
			continue
		} // Original blank template includes only sequence numbers.
		if len(out.Rows) >= 100 {
			return Draft{}, invalid(line, "maximum_100_rows")
		}
		serial, e := strconv.ParseInt(row[0], 10, 32)
		if e != nil || serial <= 0 || strconv.FormatInt(serial, 10) != row[0] || seen[row[0]] {
			return Draft{}, invalid(line, "unique_positive_sequence_required")
		}
		seen[row[0]] = true
		for col, value := range row {
			max := 256
			if col == 2 {
				max = 128
			}
			if !utf8.ValidString(value) || len(value) > max || strings.IndexFunc(value, unicode.IsControl) >= 0 {
				return Draft{}, invalid(line, "invalid_text_length_or_control")
			}
			if strings.HasPrefix(value, "=") {
				return Draft{}, invalid(line, "formula_not_allowed")
			}
		}
		amount, e := ledger.Parse(row[3])
		if row[1] == "" {
			return Draft{}, invalid(line, "account_required")
		}
		if e != nil || amount <= 0 || amount > 100000000000 {
			return Draft{}, invalid(line, "positive_decimal_amount_required")
		}
		out.Rows = append(out.Rows, Row{line, row[0], row[1], row[2], ledger.Format(amount), row[4]})
		out.TotalCents += amount
	}
	if len(out.Rows) == 0 {
		return Draft{}, invalid(0, "empty_file_no_payment_rows")
	}
	return out, nil
}
