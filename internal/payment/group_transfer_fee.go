package payment

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/grouppolicy"
	"goepay/internal/ledger"
	"goepay/internal/merchantpolicy"
)

type MerchantTransferFeePolicy struct {
	Rate          string `json:"rate"`
	RateBPS       int64  `json:"rate_bps,string"`
	GroupOverride bool   `json:"group_override"`
}

type merchantTransferFeeSelection struct {
	MerchantTransferFeePolicy
	ValidUntil       time.Time
	FallbackGlobal   bool
	GlobalConfigured bool
}

func groupTransferString(fields map[string]json.RawMessage, key string) (string, error) {
	raw, exists := fields[key]
	if !exists || string(raw) == "null" {
		return "", nil // The reference's null/empty group values inherit.
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", ErrInvalid
	}
	return value, nil
}

func readMerchantTransferFeeSelection(ctx context.Context, q referralReader, mid int64, lock bool) (merchantTransferFeeSelection, error) {
	var out merchantTransferFeeSelection
	if q == nil || mid <= 0 {
		return out, ErrInvalid
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	var profile []byte
	if err := q.QueryRow(ctx, `SELECT profile FROM merchants WHERE id=$1`+suffix, mid).Scan(&profile); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, ErrNotFound
		}
		return out, err
	}
	var merchant map[string]json.RawMessage
	if json.Unmarshal(profile, &merchant) != nil {
		return out, ErrInvalid
	}
	var group merchantpolicy.EffectiveGroup
	var err error
	if lock {
		group, err = merchantpolicy.ReadEffectiveGroup(ctx, q, mid, true)
	} else {
		// Capability/funds-confirmation projections may describe a disabled
		// merchant; every new reservation still uses the strict locked reader.
		group, err = merchantpolicy.ReadEffectiveGroupForSetup(ctx, q, mid, false)
	}
	if err != nil {
		return out, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(group.Body, &fields) != nil {
		return out, ErrInvalid
	}
	value, err := groupTransferString(fields, "transfer_rate")
	if err != nil {
		return out, err
	}
	override, err := grouppolicy.ParseTransferFeeOverride(value)
	if err != nil {
		return out, ErrInvalid
	}
	out.ValidUntil = group.ValidUntil
	out.GroupOverride = override.Override
	if !override.Override {
		legacy := "0"
		if raw, exists := merchant["transfer_rate"]; exists && string(raw) != "null" {
			if json.Unmarshal(raw, &legacy) != nil {
				return out, ErrInvalid
			}
		}
		out.Rate = legacy // Keep existing capabilities text as well as its fee.
		if legacy == "" {
			legacy = "0"
		}
		out.RateBPS, err = ledger.Parse(legacy)
		if err != nil || out.RateBPS < 0 || out.RateBPS > 10000 {
			return out, ErrInvalid
		}
		return out, nil
	}
	out.Rate, out.RateBPS = value, override.RateBPS
	if !override.UseSettlement {
		return out, nil
	}
	settlement, err := groupTransferString(fields, "settle_rate")
	if err != nil {
		return out, err
	}
	if settlement != "" {
		out.Rate = settlement
		out.RateBPS, err = ledger.Parse(settlement)
		if err != nil || out.RateBPS < 0 || out.RateBPS > 10000 {
			return out, ErrInvalid
		}
		return out, nil
	}
	out.FallbackGlobal = true
	out.RateBPS, out.GlobalConfigured, err = readTransferSettlementFallback(ctx, q, lock)
	if err != nil {
		return out, err
	}
	out.Rate = ledger.Format(out.RateBPS)
	return out, nil
}

func readTransferSettlementFallback(ctx context.Context, q fundsReader, lock bool) (int64, bool, error) {
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	var raw string
	err := q.QueryRow(ctx, `SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='settlement_fee_rate_bps' ORDER BY id LIMIT 1`+suffix).Scan(&raw)
	configured := err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		raw, err = "0", nil
	}
	if err != nil {
		return 0, false, err
	}
	rate, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || rate < 0 || rate > 10000 {
		return 0, false, ErrInvalid
	}
	return rate, configured, nil
}

func (selected *merchantTransferFeeSelection) checkDeadline(ctx context.Context, q fundsReader) error {
	if err := checkGroupTransferDeadline(ctx, q, selected.ValidUntil); err != nil {
		return err
	}
	if selected.FallbackGlobal && !selected.GlobalConfigured {
		rate, configured, err := readTransferSettlementFallback(ctx, q, true)
		if err != nil {
			return err
		}
		if rate != selected.RateBPS {
			return ErrConflict
		}
		selected.GlobalConfigured = configured
	}
	return checkGroupTransferDeadline(ctx, q, selected.ValidUntil)
}

func checkGroupTransferDeadline(ctx context.Context, q fundsReader, until time.Time) error {
	if until.IsZero() {
		return nil
	}
	var now time.Time
	if err := q.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if !now.Before(until) {
		return ErrConflict
	}
	return nil
}

// Funds confirmations must pass their current transaction, never a separate
// pool snapshot. Callers omit new signed facts when GroupOverride is false.
func (s *Store) MerchantTransferFeeSnapshot(ctx context.Context, q referralReader, mid int64) (MerchantTransferFeePolicy, error) {
	selected, err := readMerchantTransferFeeSelection(ctx, q, mid, false)
	return selected.MerchantTransferFeePolicy, err
}

func (s *Store) MerchantTransferFee(ctx context.Context, mid int64) (MerchantTransferFeePolicy, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return MerchantTransferFeePolicy{}, err
	}
	defer tx.Rollback(ctx)
	policy, err := s.MerchantTransferFeeSnapshot(ctx, tx, mid)
	if err != nil {
		return policy, err
	}
	return policy, tx.Commit(ctx)
}
