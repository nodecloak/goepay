// Package merchantpolicy contains shared merchant business permissions. It does
// not authorize provider network access, money movement, or settlement workers.
package merchantpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrInvalid = errors.New("invalid merchant transfer permission")

type Reader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type TransferAccess struct {
	Enabled         bool   `json:"enabled"`
	PlatformEnabled bool   `json:"platform_enabled"`
	Source          string `json:"source"`
	GroupID         int64  `json:"group_id,string"`
}

func scalar(raw []byte, legacyNumber bool) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var v string
	if json.Unmarshal(raw, &v) == nil {
		return v, nil
	}
	if legacyNumber && (string(raw) == "0" || string(raw) == "1") {
		return string(raw), nil
	}
	if !legacyNumber && (string(raw) == "true" || string(raw) == "false") {
		return string(raw), nil
	}
	return "", ErrInvalid
}
func resolve(values map[string]string, group string, gid int64) (TransferAccess, error) {
	a := TransferAccess{Source: "default", GroupID: gid}
	for k, v := range values {
		if k == "transfer_enabled" {
			if v != "true" && v != "false" {
				return a, ErrInvalid
			}
		} else if k == "user_transfer" {
			if v != "0" && v != "1" {
				return a, ErrInvalid
			}
		} else {
			return a, ErrInvalid
		}
	}
	if v, ok := values["transfer_enabled"]; ok {
		a.PlatformEnabled = v == "true"
		a.Source = "global"
	} else if v, ok := values["user_transfer"]; ok {
		a.PlatformEnabled = v == "1"
		a.Source = "legacy_global"
	}
	a.Enabled = a.PlatformEnabled
	switch group {
	case "", "_", "0":
	case "1":
		a.Enabled = true
		a.Source = "group"
	default:
		return a, ErrInvalid
	}
	return a, nil
}

// ReadTransferAccess is for ordinary manual/API transfers and daily visibility.
// For a new funds reservation, the caller must already hold the merchant's
// non-key row lock. Existing same-ID intents are resolved before calling this.
func ReadTransferAccess(ctx context.Context, q Reader, mid int64, lock bool) (TransferAccess, error) {
	return readTransferAccess(ctx, q, mid, lock, nil)
}

// ReadTransferAccessAt preserves a daily report's authenticated observation
// time. It is read-only and cannot be used to authorize a new reservation.
func ReadTransferAccessAt(ctx context.Context, q Reader, mid int64, at time.Time) (TransferAccess, error) {
	return readTransferAccess(ctx, q, mid, false, &at)
}
func readTransferAccess(ctx context.Context, q Reader, mid int64, lock bool, at *time.Time) (TransferAccess, error) {
	if mid <= 0 {
		return TransferAccess{}, ErrInvalid
	}
	permission, e := ReadSettlementPermission(ctx, q, mid, lock)
	if e != nil {
		return TransferAccess{}, e
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
		var member int64
		e := q.QueryRow(ctx, `SELECT merchant_id FROM memberships WHERE merchant_id=$1 FOR SHARE`, mid).Scan(&member)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return TransferAccess{}, e
		}
	}
	rows, e := q.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key' IN('transfer_enabled','user_transfer') ORDER BY id`+suffix)
	if e != nil {
		return TransferAccess{}, e
	}
	values := map[string]string{}
	for rows.Next() {
		var k string
		var raw []byte
		if e = rows.Scan(&k, &raw); e != nil {
			break
		}
		if _, ok := values[k]; ok {
			e = ErrInvalid
			break
		}
		var v string
		v, e = scalar(raw, false)
		if e != nil {
			break
		}
		values[k] = v
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return TransferAccess{}, e
	}
	effective := func() (*int64, error) {
		var gid *int64
		e := q.QueryRow(ctx, `SELECT CASE WHEN v.expires_at>COALESCE($2::timestamptz,clock_timestamp()) THEN v.group_id WHEN m.group_baseline_unresolved THEN NULL::bigint ELSE m.base_group_id END FROM merchants m LEFT JOIN memberships v ON v.merchant_id=m.id WHERE m.id=$1`, mid, at).Scan(&gid)
		return gid, e
	}
	gid, e := effective()
	if e != nil {
		return TransferAccess{}, e
	}
	if gid == nil || *gid < 0 {
		return TransferAccess{}, ErrInvalid
	}
	// A membership can expire while waiting for its group row lock. It can only
	// transition once because the merchant/membership rows are held by the caller.
	for attempt := 0; attempt < 2; attempt++ {
		group := ""
		disabled := false
		if *gid >= 0 {
			var raw []byte
			var enabled *string
			e = q.QueryRow(ctx, `SELECT body->'user_transfer',body->>'enabled' FROM backoffice_records WHERE resource='groups' AND id=$1`+suffix, *gid).Scan(&raw, &enabled)
			if e != nil {
				return TransferAccess{}, e
			}
			if enabled != nil {
				if *enabled != "true" && *enabled != "false" {
					return TransferAccess{}, ErrInvalid
				}
				disabled = *enabled == "false"
			}
			group, e = scalar(raw, true)
			if e != nil {
				return TransferAccess{}, e
			}
		}
		if lock {
			current, err := effective()
			if err != nil {
				return TransferAccess{}, err
			}
			if current == nil || *current < 0 {
				return TransferAccess{}, ErrInvalid
			}
			if *current != *gid {
				gid = current
				continue
			}
		}
		access, err := resolve(values, group, *gid)
		if err != nil {
			return TransferAccess{}, err
		}
		if disabled {
			access.Enabled = false
			access.Source = "group_disabled"
		}
		if !permission.Allowed {
			access.Enabled = false
			access.Source = "merchant_settle"
		}
		return access, nil
	}
	return TransferAccess{}, fmt.Errorf("%w: membership changed during permission read", ErrInvalid)
}
