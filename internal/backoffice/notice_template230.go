package backoffice

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"strings"
)

// Fields may be saved incrementally, but one nonempty template parameter
// cannot belong to two source fields. Serialize validation of the whole scene.
func validateNoticeTemplate230(ctx context.Context, tx pgx.Tx, resource string, id int64, body map[string]any) error {
	if resource != "settings" {
		return nil
	}
	key, _ := body["key"].(string)
	if !strings.HasPrefix(key, "wxnotice_tpl_") {
		return nil
	}
	rest := strings.TrimPrefix(key, "wxnotice_tpl_")
	scene := strings.Split(rest, "_")[0]
	prefix := "wxnotice_tpl_" + scene
	if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,230))", prefix); e != nil {
		return e
	}
	rows, e := tx.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND id<>$1 AND (body->>'key'=$2 OR starts_with(body->>'key',$2||'_'))`, id, prefix)
	if e != nil {
		return e
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var k, v string
		var raw []byte
		if e = rows.Scan(&k, &raw); e != nil {
			return e
		}
		if json.Unmarshal(raw, &v) != nil {
			return ErrInvalid
		}
		if _, ok := values[k]; ok {
			return ErrConflict
		}
		values[k] = v
	}
	if e = rows.Err(); e != nil {
		return e
	}
	if _, exists := values[key]; exists {
		return ErrConflict
	}
	value, _ := body["value"].(string)
	values[key] = value
	used := map[string]bool{}
	for k, v := range values {
		if k == prefix || v == "" {
			continue
		}
		if used[v] {
			return ErrInvalid
		}
		used[v] = true
	}
	return nil
}
