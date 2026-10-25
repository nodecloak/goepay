package payment

import (
	"context"
	"encoding/json"
	"strconv"

	"goepay/internal/ledger"
)

type settlementThresholdSettingFact struct {
	ID    int64           `json:"id,string"`
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type settlementThresholdConfig struct {
	Enabled    bool                             `json:"enabled"`
	ChannelID  int64                            `json:"channel_id,string"`
	Policy     SettlementThresholdPolicy        `json:"policy"`
	SettleType string                           `json:"settle_type"`
	Facts      []settlementThresholdSettingFact `json:"facts"`
}

var settlementThresholdSettingKeys = []string{"auto_settle_money", "transfer_alipay", "settlement_fee_rate_bps", "settlement_fee_maximum_cents", "settle_type"}

// Only the readable Pro Transfer branch's relevant keys enter this snapshot.
// In particular a Settle fee minimum/retained amount cannot alter this quote.
// This read alone creates no authority, reservation, job or external request.
func readSettlementThresholdConfig(ctx context.Context, q referralReader, lock bool) (settlementThresholdConfig, error) {
	out := settlementThresholdConfig{SettleType: "0", Facts: []settlementThresholdSettingFact{}}
	if q == nil {
		return out, ErrInvalid
	}
	sql := `SELECT id,body->>'key',body->>'value',body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1::text[]) ORDER BY body->>'key',id`
	if lock {
		sql += " FOR SHARE"
	}
	rows, e := q.Query(ctx, sql, settlementThresholdSettingKeys)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var fact settlementThresholdSettingFact
		var value *string
		if e = rows.Scan(&fact.ID, &fact.Key, &value, &fact.Value); e != nil {
			return out, e
		}
		if fact.ID <= 0 || value == nil || seen[fact.Key] {
			return out, ErrInvalid
		}
		seen[fact.Key] = true
		out.Facts = append(out.Facts, fact)
		if fact.Key == "auto_settle_money" {
			if *value == "" {
				continue
			}
			out.Policy.ThresholdCents, e = ledger.Parse(*value)
			if e != nil || out.Policy.ThresholdCents < 0 || out.Policy.ThresholdCents > 100000000000 {
				return out, ErrInvalid
			}
			continue
		}
		if fact.Key == "settle_type" {
			if *value != "0" && *value != "1" {
				return out, ErrInvalid
			}
			out.SettleType = *value
			continue
		}
		v, e := strconv.ParseInt(*value, 10, 64)
		if e != nil || v < 0 || strconv.FormatInt(v, 10) != *value {
			return out, ErrInvalid
		}
		switch fact.Key {
		case "transfer_alipay":
			out.ChannelID = v
		case "settlement_fee_rate_bps":
			if v > 10000 {
				return out, ErrInvalid
			}
			out.Policy.FeeRateBPS = v
		case "settlement_fee_maximum_cents":
			if v > 100000000000 {
				return out, ErrInvalid
			}
			out.Policy.FeeMaximumCents = v
		default:
			return out, ErrInvalid
		}
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	out.Enabled = out.Policy.ThresholdCents > 0 && out.ChannelID > 0
	return out, nil
}
