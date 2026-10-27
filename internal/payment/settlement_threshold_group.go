package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/ledger"
)

type settlementThresholdMerchantPolicy struct {
	MerchantID       int64                     `json:"merchant_id,string"`
	MerchantRevision int64                     `json:"merchant_revision,string"`
	GroupID          int64                     `json:"group_id,string"`
	GroupBody        json.RawMessage           `json:"group_body"`
	GroupDisabled    bool                      `json:"group_disabled"`
	FeeOverrideBPS   *int64                    `json:"fee_override_bps"`
	Policy           SettlementThresholdPolicy `json:"policy"`
}

func thresholdGroupScalar(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if e := d.Decode(&value); e != nil {
		return "", ErrInvalid
	}
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case json.Number:
		return v.String(), nil
	default:
		return "", ErrInvalid
	}
}

// Reuse the existing membership-aware effective-group resolution. An expired
// purchase must not leave its earlier group rate in a new payment authority.
func readSettlementThresholdMerchantPolicy(ctx context.Context, q referralReader, mid int64, global SettlementThresholdPolicy, at time.Time) (settlementThresholdMerchantPolicy, error) {
	out := settlementThresholdMerchantPolicy{MerchantID: mid, Policy: global}
	if q == nil || mid <= 0 || !validSettlementScheduleTime(at) || global.validate() != nil {
		return out, ErrInvalid
	}
	var enabled bool
	var e error
	out.GroupID, out.MerchantRevision, enabled, e = membershipEffectiveGroup(ctx, q, mid, at)
	if e != nil {
		return out, e
	}
	if !enabled {
		return out, ErrState
	}
	if out.GroupID < 0 || out.MerchantRevision <= 0 {
		return out, ErrConflict
	}
	if e = q.QueryRow(ctx, "SELECT body FROM backoffice_records WHERE resource='groups' AND id=$1", out.GroupID).Scan(&out.GroupBody); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return out, ErrConflict
		}
		return out, e
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(out.GroupBody, &body) != nil || body == nil {
		return out, ErrInvalid
	}
	if enabled, exists := body["enabled"]; exists && string(enabled) != "true" {
		out.GroupDisabled = true
		return out, nil
	}
	open, e := thresholdGroupScalar(body["settle_open"])
	if e != nil {
		return out, e
	}
	switch open {
	case "", "_", "0", "1", "3":
	case "2":
		out.GroupDisabled = true
		return out, nil
	default:
		return out, ErrInvalid
	}
	rate, e := thresholdGroupScalar(body["settle_rate"])
	if e != nil {
		return out, e
	}
	if rate == "" {
		return out, nil
	}
	bps, e := ledger.Parse(rate)
	if e != nil || bps < 0 || bps > 10000 {
		return out, ErrInvalid
	}
	out.FeeOverrideBPS = &bps
	out.Policy.FeeRateBPS = bps
	return out, nil
}
