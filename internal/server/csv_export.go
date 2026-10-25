package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"errors"
	"fmt"
	"goepay/internal/identity"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
)

const csvExportMaxRows = 50000
const csvExportMaxBytes = 8 << 20

var csvExportSlots = make(chan struct{}, 2)
var errCSVExportTooLarge = errors.New("CSV exceeds 50000 rows or 8 MiB; narrow the filters")

func csvRequested(r *http.Request) (bool, error) {
	values, exists := r.URL.Query()["format"]
	if !exists {
		return false, nil
	}
	if len(values) != 1 || values[0] != "csv" {
		return false, errors.New("format must be csv")
	}
	return true, nil
}

type csvBoundedBuffer struct{ bytes.Buffer }

func (b *csvBoundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > csvExportMaxBytes {
		return 0, errCSVExportTooLarge
	}
	return b.Buffer.Write(p)
}

func csvExportCell(value any) string {
	var text string
	switch v := value.(type) {
	case nil:
		return ""
	case time.Time:
		text = v.In(time.FixedZone("UTC+08:00", 8*3600)).Format(time.RFC3339Nano)
	case []byte:
		text = string(v)
	default:
		text = fmt.Sprint(v)
	}
	// CSV quoting alone does not stop spreadsheet formula execution. Inspect the
	// first non-whitespace/control rune while preserving the original text.
	start := strings.TrimLeftFunc(text, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
	if start != "" && strings.ContainsRune("=+-@", rune(start[0])) {
		return "'" + text
	}
	return text
}

// query is assembled only from the endpoint's fixed SQL expressions, never from
// the format parameter. One SELECT supplies the entire snapshot, without paging.
func (s *Server) exportCSV(w http.ResponseWriter, r *http.Request, session identity.Session, resource, query string, args []any) {
	select {
	case csvExportSlots <- struct{}{}:
		defer func() { <-csvExportSlots }()
	default:
		w.Header().Set("Retry-After", "5")
		response(w, 429, -1, "CSV exports busy", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	rows, e := s.Pool.Query(ctx, query+fmt.Sprintf(" LIMIT %d", csvExportMaxRows+1), args...)
	if e != nil {
		s.fail(w, e)
		return
	}
	defer rows.Close()
	b := &csvBoundedBuffer{}
	_, _ = b.Write([]byte("\xef\xbb\xbf"))
	writer := csv.NewWriter(b)
	columns := make([]string, len(rows.FieldDescriptions()))
	for i, f := range rows.FieldDescriptions() {
		columns[i] = f.Name
	}
	e = writer.Write(columns)
	count := 0
	for e == nil && rows.Next() {
		count++
		if count > csvExportMaxRows {
			e = errCSVExportTooLarge
			break
		}
		var values []any
		values, e = rows.Values()
		if e != nil {
			break
		}
		record := make([]string, len(values))
		for i, v := range values {
			record[i] = csvExportCell(v)
		}
		e = writer.Write(record)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	writer.Flush()
	if e == nil {
		e = writer.Error()
	}
	if errors.Is(e, errCSVExportTooLarge) {
		response(w, 422, -1, e.Error(), nil)
		return
	}
	if e != nil {
		s.fail(w, e)
		return
	}
	s.finishCSVExport(ctx, w, session, resource, b, count)
}

// Shared finalization keeps the same audit-before-response rule for typed
// reports that build their columns without exposing arbitrary SQL identifiers.
func (s *Server) finishCSVExport(ctx context.Context, w http.ResponseWriter, session identity.Session, resource string, b *csvBoundedBuffer, count int) {
	digest := fmt.Sprintf("%x", sha256.Sum256(b.Bytes()))
	var issuer any
	if session.Issuer != nil {
		issuer = fmt.Sprint(session.Issuer.ID)
	}
	_, e := s.Pool.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,$4,0,'export-csv',jsonb_build_object('rows',$5::integer,'sha256',$6::text,'delegated_by',$7::text))", session.User.Role, session.User.ID, session.User.Username, resource, count, digest, issuer)
	if e != nil {
		s.fail(w, e)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.csv\"", strings.ReplaceAll(resource, "/", "-")))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Export-Rows", fmt.Sprint(count))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, bytes.NewReader(b.Bytes()))
}
