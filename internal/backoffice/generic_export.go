package backoffice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Generic resource CSV export, reusing the bounded merchant-directory pattern
// (one SELECT, no paging, row + byte ceiling, formula-injection neutralisation,
// BOM, audit row). Enabled only for a definition marked exportable. Columns are
// the schema's own non-secret, non-write-only fields plus the surfaced
// created_at — never a password / api_key / RSA / secret field, so the export
// cannot leak what the list projection already withholds. Honours the same
// toolbar filters and search the list GET uses.
const resourceExportMaxRows = 50000
const resourceExportMaxBytes = 8 << 20

var resourceExportSlots = make(chan struct{}, 2)
var errResourceExportTooLarge = errors.New("CSV exceeds 50000 rows or 8 MiB; narrow the filters")

type resourceExportBuffer struct{ bytes.Buffer }

func (b *resourceExportBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > resourceExportMaxBytes {
		return 0, errResourceExportTooLarge
	}
	return b.Buffer.Write(p)
}

// exportColumns are the schema fields safe to export: visible (not write-only),
// not a credential, not a password/text secret. created_at is appended when the
// definition surfaces it (blacklist/domains do).
func exportColumns(d definition) []Field {
	out := []Field{}
	for _, f := range d.fields {
		if f.WriteOnly || f.Type == "password" || isCredential(f.Name) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func isCredential(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "password") || strings.Contains(n, "secret") ||
		strings.Contains(n, "api_key") || strings.Contains(n, "private_key") ||
		strings.Contains(n, "token") || n == "rsa_public_key"
}

func exportCell(v any) string {
	var text string
	switch t := v.(type) {
	case nil:
		return ""
	case bool:
		if t {
			return "是"
		}
		return "否"
	default:
		text = fmt.Sprint(t)
	}
	trimmed := strings.TrimLeft(text, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + text
	}
	return text
}

// exportResourceCSV writes the resource rows matching the current list query.
// Reuses s.List (so scoping, filters and search are identical to the list GET),
// pulling pages up to the row ceiling rather than a second SQL path.
func (s *Service) exportResourceCSV(w http.ResponseWriter, r *http.Request, p Principal, resource string, q Query) {
	d, e := definitionFor(p, resource, false)
	if e != nil {
		writeError(w, e)
		return
	}
	if !d.exportable {
		write(w, 404, nil, "not found")
		return
	}
	select {
	case resourceExportSlots <- struct{}{}:
		defer func() { <-resourceExportSlots }()
	default:
		w.Header().Set("Retry-After", "5")
		write(w, 429, nil, "CSV exports busy")
		return
	}
	cols := exportColumns(d)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	b := &resourceExportBuffer{}
	_, _ = b.Write([]byte("\xef\xbb\xbf"))
	writer := csv.NewWriter(b)
	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = c.Label
	}
	e = writer.Write(header)
	count := 0
	q.Limit = 100
	for page := 1; e == nil; page++ {
		q.Page = page
		var res Page
		res, e = s.List(ctx, p, resource, q)
		if e != nil || len(res.Items) == 0 {
			break
		}
		for _, item := range res.Items {
			count++
			if count > resourceExportMaxRows {
				e = errResourceExportTooLarge
				break
			}
			record := make([]string, len(cols))
			for i, c := range cols {
				if c.Name == "id" {
					record[i] = strconv.FormatInt(item.ID, 10)
					continue
				}
				record[i] = exportCell(item.Fields[c.Name])
			}
			if e = writer.Write(record); e != nil {
				break
			}
		}
		if int64(page*q.Limit) >= res.Total {
			break
		}
	}
	writer.Flush()
	if e == nil {
		e = writer.Error()
	}
	if errors.Is(e, errResourceExportTooLarge) {
		write(w, 422, nil, e.Error())
		return
	}
	if e != nil {
		writeError(w, e)
		return
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(b.Bytes()))
	if _, e = s.pool.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,$4,0,'export-csv',jsonb_build_object('rows',$5::integer,'sha256',$6::text))", p.Role, p.SubjectID, p.Username, resource, count, digest); e != nil {
		writeError(w, e)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+resource+".csv\"")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Export-Rows", fmt.Sprint(count))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, bytes.NewReader(b.Bytes()))
}

// BatchDelete runs the resource's existing reversible Delete for each selected
// id, stopping on the first failure and reporting how many were removed. A
// resource whose definition forbids delete (noDelete) is refused outright, so a
// resource without a reversible delete never gets a batch delete. Each id goes
// through Delete, which for archive-style resources archives rather than hard
// deletes — never a hard delete here.
func (s *Service) BatchDelete(ctx context.Context, p Principal, r string, ids []int64) (int, error) {
	d, e := definitionFor(p, r, true)
	if e != nil {
		return 0, e
	}
	if d.noDelete {
		return 0, ErrForbidden
	}
	if len(ids) == 0 || len(ids) > 200 {
		return 0, ErrInvalid
	}
	done := 0
	for _, id := range ids {
		if e = s.Delete(ctx, p, r, id); e != nil {
			return done, e
		}
		done++
	}
	return done, nil
}

// serveBatchDelete handles POST /resources/<r>/batch-delete {"ids":[...]}.
func (s *Service) serveBatchDelete(w http.ResponseWriter, r *http.Request, p Principal, resource string) {
	var in struct {
		IDs []json.RawMessage `json:"ids"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if e := json.NewDecoder(r.Body).Decode(&in); e != nil {
		write(w, 400, nil, "invalid JSON")
		return
	}
	ids := make([]int64, 0, len(in.IDs))
	for _, m := range in.IDs {
		s := strings.Trim(string(m), `"`)
		id, e := strconv.ParseInt(s, 10, 64)
		if e != nil || id <= 0 {
			write(w, 400, nil, "invalid ID")
			return
		}
		ids = append(ids, id)
	}
	done, e := s.BatchDelete(r.Context(), p, resource, ids)
	if e != nil {
		writeError(w, e)
		return
	}
	write(w, 200, map[string]any{"deleted": done}, "")
}
