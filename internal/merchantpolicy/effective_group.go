package merchantpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrGroupPolicy = errors.New("effective merchant group unavailable")

type EffectiveGroup struct {
	ID   int64
	Body json.RawMessage
	// New intent callers recheck after downstream waits. Historical sources
	// continue using their explicit original time, outside this current reader.
	ValidUntil time.Time
}

// ReadEffectiveGroup is a current policy read. Monetary callers supply their
// transaction and lock=true; read-only quotes use a coherent read transaction.
// The default group is the real reserved row 0, separate from channel zero.
func ReadEffectiveGroup(ctx context.Context, q Reader, mid int64, lock bool) (EffectiveGroup, error) {
	var out EffectiveGroup
	if q == nil || mid <= 0 {
		return out, ErrGroupPolicy
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	var base, member int64
	var enabled, unresolved bool
	if e := q.QueryRow(ctx, `SELECT base_group_id,enabled,group_baseline_unresolved FROM merchants WHERE id=$1`+suffix, mid).Scan(&base, &enabled, &unresolved); e != nil {
		return out, e
	}
	if !enabled || unresolved || base < 0 {
		return out, ErrGroupPolicy
	}
	var expires time.Time
	e := q.QueryRow(ctx, `SELECT group_id,expires_at FROM memberships WHERE merchant_id=$1`+suffix, mid).Scan(&member, &expires)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	ids := []int64{base}
	if member > 0 && member != base {
		ids = append(ids, member)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	rows, e := q.Query(ctx, `SELECT id,body FROM backoffice_records WHERE resource='groups' AND id=ANY($1::bigint[]) ORDER BY id`+suffix, ids)
	if e != nil {
		return out, e
	}
	bodies := map[int64]json.RawMessage{}
	for rows.Next() {
		var id int64
		var body []byte
		if e = rows.Scan(&id, &body); e != nil {
			rows.Close()
			return out, e
		}
		bodies[id] = append(json.RawMessage(nil), body...)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return out, e
	}
	var at time.Time
	if e = q.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return out, e
	}
	out.ID = base
	if member > 0 && at.Before(expires) {
		out.ID = member
		out.ValidUntil = expires
	}
	body, exists := bodies[out.ID]
	if !exists {
		return out, ErrGroupPolicy
	}
	var fields struct {
		Enabled *bool `json:"enabled"`
	}
	if json.Unmarshal(body, &fields) != nil || fields.Enabled != nil && !*fields.Enabled {
		return out, ErrGroupPolicy
	}
	out.Body = body
	return out, nil
}
