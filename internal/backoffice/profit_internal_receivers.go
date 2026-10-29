package backoffice

import (
	"fmt"
	"strconv"
)

// internalReceiver is one leg of a 站内分账 rule (a4 N3). The original
// pre_psinternal carried an `info` list of recipient+rate legs; GOEPAY stores
// them as a `receivers` array in the rule body while keeping the top-level
// `rate_bps` equal to their sum so every existing reader and the cross-rule
// Σ<=100% guard stay correct.
type internalReceiver struct {
	recipient int64
	rate      int64
}

// normalizeInternalReceivers reads either the multi-receiver `receivers` array
// or a legacy single `recipient_id`+`rate_bps` body, validates it, and REWRITES
// v in place so the stored body always has a canonical `receivers` list plus a
// top-level `rate_bps` equal to the sum. Legacy single-receiver bodies also keep
// their `recipient_id` so older code paths continue to resolve one recipient.
func normalizeInternalReceivers(ownerID int64, v map[string]any) ([]internalReceiver, error) {
	var out []internalReceiver
	seen := map[int64]bool{}
	add := func(recipient, rate int64) error {
		if recipient <= 0 || recipient == ownerID || rate < 0 || rate > 10000 {
			return ErrInvalid
		}
		if seen[recipient] {
			return ErrInvalid
		}
		seen[recipient] = true
		out = append(out, internalReceiver{recipient: recipient, rate: rate})
		return nil
	}

	if raw, ok := v["receivers"]; ok && raw != nil {
		list, ok := raw.([]any)
		if !ok || len(list) < 1 || len(list) > 10 {
			return nil, ErrInvalid
		}
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, ErrInvalid
			}
			recipient, e := strconv.ParseInt(fmt.Sprint(m["recipient_id"]), 10, 64)
			if e != nil {
				return nil, ErrInvalid
			}
			rate, e := strconv.ParseInt(fmt.Sprint(m["rate_bps"]), 10, 64)
			if e != nil {
				return nil, ErrInvalid
			}
			if e = add(recipient, rate); e != nil {
				return nil, e
			}
		}
	} else {
		recipient, e := strconv.ParseInt(fmt.Sprint(v["recipient_id"]), 10, 64)
		if e != nil {
			return nil, ErrInvalid
		}
		rate, e := strconv.ParseInt(fmt.Sprint(v["rate_bps"]), 10, 64)
		if e != nil {
			return nil, ErrInvalid
		}
		if e = add(recipient, rate); e != nil {
			return nil, e
		}
	}

	// Rewrite the body to the canonical shape: a receivers list + summed rate.
	var total int64
	canonical := make([]any, 0, len(out))
	for _, rcv := range out {
		total += rcv.rate
		canonical = append(canonical, map[string]any{
			"recipient_id": strconv.FormatInt(rcv.recipient, 10),
			"rate_bps":     strconv.FormatInt(rcv.rate, 10),
		})
	}
	v["receivers"] = canonical
	v["rate_bps"] = strconv.FormatInt(total, 10)
	// Preserve a single recipient_id for a one-leg rule (legacy readers); drop it
	// for a multi-leg rule so no single-recipient reader silently uses leg one.
	if len(out) == 1 {
		v["recipient_id"] = strconv.FormatInt(out[0].recipient, 10)
	} else {
		delete(v, "recipient_id")
	}
	return out, nil
}
