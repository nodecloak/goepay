package payment

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"goepay/internal/ledger"
)

// Order limits are evaluated only when creating a new original order. The
// caller has already returned an exact historical replay and locked its merchant.
func validateOriginalOrderAmount(ctx context.Context, tx pgx.Tx, group string, merchant, amount int64) error {
	values := map[string]string{}
	rows, e := tx.Query(ctx, `SELECT body->>'key',COALESCE(body->>'value','') FROM backoffice_records WHERE resource='settings' AND body->>'key' IN ('pay_minmoney','pay_maxmoney') FOR SHARE`)
	if e != nil {
		return e
	}
	for rows.Next() {
		var key, value string
		if e = rows.Scan(&key, &value); e != nil {
			rows.Close()
			return e
		}
		if _, duplicate := values[key]; duplicate {
			rows.Close()
			return ErrState
		}
		values[key] = value
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	bounds := map[string]int64{"pay_minmoney": 0, "pay_maxmoney": 0}
	for key, raw := range values {
		if raw == "" {
			continue
		}
		n, e := ledger.Parse(raw)
		if e != nil {
			return ErrState
		}
		bounds[key] = n
	}
	if group != "" {
		gid, e := strconv.ParseInt(group, 10, 64)
		if e != nil || gid < 1 {
			return ErrState
		}
		var raw []byte
		if e = tx.QueryRow(ctx, `SELECT body FROM backoffice_records WHERE resource='groups' AND id=$1 FOR SHARE`, gid).Scan(&raw); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return ErrState
			}
			return e
		}
		var config map[string]json.RawMessage
		if json.Unmarshal(raw, &config) != nil {
			return ErrState
		}
		if enabled, ok := config["enabled"]; ok && string(enabled) != "true" {
			return ErrState
		}
		for key := range bounds {
			if value, ok := config[key]; ok && string(value) != "null" {
				var override string
				if json.Unmarshal(value, &override) != nil {
					return ErrState
				}
				if override == "" {
					continue
				}
				n, e := ledger.Parse(override)
				if e != nil {
					return ErrState
				}
				bounds[key] = n
			}
		}
	}
	if bounds["pay_maxmoney"] > 0 && bounds["pay_minmoney"] > bounds["pay_maxmoney"] {
		return ErrState
	}
	if amount < bounds["pay_minmoney"] || bounds["pay_maxmoney"] > 0 && amount > bounds["pay_maxmoney"] {
		return ErrInvalid
	}
	// A merchant's own bounds further restrict its effective group. Zero or an
	// absent value adds no restriction; a malformed stored policy fails closed.
	var profile []byte
	if e = tx.QueryRow(ctx, `SELECT profile FROM merchants WHERE id=$1`, merchant).Scan(&profile); e != nil {
		return e
	}
	var own map[string]json.RawMessage
	if json.Unmarshal(profile, &own) != nil {
		return ErrState
	}
	for _, key := range []string{"pay_minmoney", "pay_maxmoney"} {
		if value, ok := own[key]; ok && string(value) != "null" {
			var raw string
			if json.Unmarshal(value, &raw) != nil {
				return ErrState
			}
			if raw == "" {
				continue
			}
			n, e := ledger.Parse(raw)
			if e != nil {
				return ErrState
			}
			if key == "pay_minmoney" && amount < n || key == "pay_maxmoney" && n > 0 && amount > n {
				return ErrInvalid
			}
		}
	}
	return nil
}
