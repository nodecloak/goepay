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

type merchantSettlementCycle struct {
	grouppolicy.SettlementCycle
	ValidUntil time.Time
}

// Monetary callers lock the global setting, merchant, membership and possible
// group rows before waiting for outgoing accounts. Quotes use their read tx.
func readMerchantSettlementCycle(ctx context.Context, q referralReader, mid int64, lock bool) (merchantSettlementCycle, error) {
	var out merchantSettlementCycle
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	var global string
	if err := q.QueryRow(ctx, `SELECT COALESCE((SELECT COALESCE(body->>'value','') FROM backoffice_records WHERE resource='settings' AND body->>'key'='settle_type'`+suffix+`),'0')`).Scan(&global); err != nil {
		return out, err
	}
	if global != "0" && global != "1" {
		return out, ErrInvalid
	}
	var group merchantpolicy.EffectiveGroup
	var err error
	if lock {
		group, err = merchantpolicy.ReadEffectiveGroup(ctx, q, mid, true)
	} else {
		// A balance/capability projection is not permission to reserve money.
		// New outgoing callers first use the strict locked cycle read above.
		group, err = merchantpolicy.ReadEffectiveGroupForSetup(ctx, q, mid, false)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, ErrNotFound
		}
		if errors.Is(err, merchantpolicy.ErrGroupPolicy) {
			return out, ErrState
		}
		return out, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(group.Body, &fields) != nil {
		return out, ErrState
	}
	value, err := thresholdGroupScalar(fields["settle_type"])
	if err != nil {
		return out, ErrInvalid
	}
	cycle, err := grouppolicy.ParseSettlementCycle(value)
	if err != nil {
		return out, ErrInvalid
	}
	if !cycle.Override && global == "1" {
		cycle.Mode = 1
	}
	return merchantSettlementCycle{SettlementCycle: cycle, ValidUntil: group.ValidUntil}, nil
}

func (cycle merchantSettlementCycle) checkDeadline(ctx context.Context, q fundsReader) error {
	if cycle.ValidUntil.IsZero() {
		return nil
	}
	var now time.Time
	if err := q.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if !now.Before(cycle.ValidUntil) {
		return ErrConflict
	}
	return nil
}
