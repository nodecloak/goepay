package payment

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/grouppolicy"
	"goepay/internal/merchantpolicy"
)

type merchantRedpackFeature struct {
	Enabled    bool
	ValidUntil time.Time
}

func readMerchantRedpackFeature(ctx context.Context, q referralReader, mid int64, lock bool) (merchantRedpackFeature, error) {
	var out merchantRedpackFeature
	if q == nil || mid <= 0 {
		return out, ErrInvalid
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	var global string
	err := q.QueryRow(ctx, `SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='user_transfer_red' ORDER BY id LIMIT 1`+suffix).Scan(&global)
	if errors.Is(err, pgx.ErrNoRows) {
		global, err = "0", nil
	}
	if err != nil {
		return out, err
	}
	if global != "0" && global != "1" {
		return out, ErrInvalid
	}
	group, err := merchantpolicy.ReadEffectiveGroup(ctx, q, mid, lock)
	if err != nil {
		return out, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(group.Body, &fields) != nil {
		return out, ErrInvalid
	}
	value, err := groupTransferString(fields, "user_transfer_red")
	if err != nil {
		return out, err
	}
	override, err := grouppolicy.ParseRedpackFeatureOverride(value)
	if err != nil {
		return out, ErrInvalid
	}
	out.Enabled, out.ValidUntil = global == "1", group.ValidUntil
	if override.Override {
		out.Enabled = override.Enabled
	}
	return out, nil
}

func (s *Store) MerchantRedpackFeature(ctx context.Context, mid int64) (bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	feature, err := readMerchantRedpackFeature(ctx, tx, mid, false)
	if err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return feature.Enabled, nil
}
