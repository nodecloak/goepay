package merchantpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"goepay/internal/ledger"
)

var ErrCombinePolicy = errors.New("invalid merchant combined payment policy")
var ErrCombinePolicyChanged = errors.New("merchant combined payment policy changed or expired")

type CombinePolicy struct {
	Enabled            bool
	MinCents, SubCents int64
	GroupID            int64
	ValidUntil         time.Time
	globalHash         [32]byte
}

func combineGlobalAmount(value string) int64 {
	n, err := ledger.Parse(value)
	if err != nil {
		return 0 // Existing global split configuration treats malformed money as zero.
	}
	return n
}

func ResolveCombinePolicy(global map[string]string, group json.RawMessage) (CombinePolicy, error) {
	out := CombinePolicy{Enabled: global["wxcombine_open"] == "1", MinCents: combineGlobalAmount(global["wxcombine_minmoney"]), SubCents: combineGlobalAmount(global["wxcombine_submoney"])}
	values := make(map[string]string)
	for _, key := range []string{"wxcombine_open", "wxcombine_minmoney", "wxcombine_submoney"} {
		if value, exists := global[key]; exists {
			values[key] = value
		}
	}
	encoded, _ := json.Marshal(values)
	out.globalHash = sha256.Sum256(encoded)
	var fields map[string]json.RawMessage
	if json.Unmarshal(group, &fields) != nil || fields == nil {
		return out, ErrCombinePolicy
	}
	for _, key := range []string{"wxcombine_open", "wxcombine_minmoney", "wxcombine_submoney"} {
		raw, exists := fields[key]
		if !exists {
			continue
		}
		// Both policies use exact JSON strings/numbers and reject bool/null.
		value, err := depositScalar(raw)
		if err != nil {
			return out, ErrCombinePolicy
		}
		if value == "" || value == "_" {
			continue
		}
		if key == "wxcombine_open" {
			if value != "0" && value != "1" {
				return out, ErrCombinePolicy
			}
			out.Enabled = value == "1"
			continue
		}
		n, err := ledger.Parse(value)
		if err != nil {
			return out, ErrCombinePolicy
		}
		if key == "wxcombine_minmoney" {
			out.MinCents = n
		} else {
			out.SubCents = n
		}
	}
	return out, nil
}

// Lock order is merchant, membership/groups, settings. The caller may then lock
// the order; it must not enter this reader while holding a new order lock first.
func ReadCombinePolicy(ctx context.Context, q Reader, mid int64, lock bool) (CombinePolicy, error) {
	var out CombinePolicy
	group, err := ReadEffectiveGroup(ctx, q, mid, lock)
	if err != nil {
		return out, err
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	rows, err := q.Query(ctx, `SELECT body->>'key',body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key' IN ('wxcombine_open','wxcombine_minmoney','wxcombine_submoney') ORDER BY id`+suffix)
	if err != nil {
		return out, err
	}
	values := make(map[string]string)
	for rows.Next() {
		var key string
		var value *string
		if err = rows.Scan(&key, &value); err != nil {
			rows.Close()
			return out, err
		}
		if _, exists := values[key]; exists || value == nil {
			rows.Close()
			return out, ErrCombinePolicy
		}
		values[key] = *value
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	out, err = ResolveCombinePolicy(values, group.Body)
	out.GroupID, out.ValidUntil = group.ID, group.ValidUntil
	return out, err
}

func (policy CombinePolicy) Recheck(ctx context.Context, q Reader, mid int64) error {
	if err := policy.checkDeadline(ctx, q); err != nil {
		return err
	}
	// Initial preparation already holds its merchant/group/setting rows. Do
	// not acquire newly inserted membership/group rows after the order lock;
	// compare a fresh MVCC read instead, including formerly absent globals.
	current, err := ReadCombinePolicy(ctx, q, mid, false)
	if err != nil {
		return err
	}
	if policy.globalHash != current.globalHash || policy.Enabled != current.Enabled || policy.MinCents != current.MinCents || policy.SubCents != current.SubCents || policy.GroupID != current.GroupID || !policy.ValidUntil.Equal(current.ValidUntil) {
		return ErrCombinePolicyChanged
	}
	return current.checkDeadline(ctx, q)
}

func (policy CombinePolicy) checkDeadline(ctx context.Context, q Reader) error {
	if !policy.ValidUntil.IsZero() {
		var now time.Time
		if err := q.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		if !now.Before(policy.ValidUntil) {
			return ErrCombinePolicyChanged
		}
	}
	return nil
}
