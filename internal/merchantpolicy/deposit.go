package merchantpolicy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"goepay/internal/ledger"
)

var ErrDepositPolicy = errors.New("invalid merchant deposit policy")
var ErrDepositPolicyChanged = errors.New("merchant deposit policy changed or expired")

type DepositPolicy struct {
	Enabled    bool
	MinCents   int64
	Days       int
	GroupID    int64
	ValidUntil time.Time
	// Absence is part of the snapshot: the first explicit global value must
	// invalidate a pending intent even when it happens to resolve to a default.
	globalHash [32]byte
}

func depositScalar(raw json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", ErrDepositPolicy
	}
	switch v := value.(type) {
	case string:
		return v, nil
	case json.Number:
		return v.String(), nil
	default:
		return "", ErrDepositPolicy
	}
}

// Retain the global reader's old default-ON and malformed-numeric defaults.
// Group overrides are validated independently and never inherit on bad input.
func legacyDepositMinimum(value string) int64 {
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 2 {
		return 0
	}
	for _, part := range []string{parts[0], fraction} {
		for _, c := range part {
			if c < '0' || c > '9' {
				return 0
			}
		}
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	n, err := strconv.ParseInt(parts[0]+fraction, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func ResolveDepositPolicy(global map[string]string, group json.RawMessage) (DepositPolicy, error) {
	out := DepositPolicy{Enabled: global["user_deposit"] != "0", MinCents: legacyDepositMinimum(global["user_deposit_min"])}
	values := make(map[string]string)
	for _, key := range []string{"user_deposit", "user_deposit_min", "user_deposit_day"} {
		if value, exists := global[key]; exists {
			values[key] = value
		}
	}
	encoded, _ := json.Marshal(values)
	out.globalHash = sha256.Sum256(encoded)
	if n, err := strconv.Atoi(global["user_deposit_day"]); err == nil && n > 0 {
		out.Days = n
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(group, &fields) != nil || fields == nil {
		return out, ErrDepositPolicy
	}
	for _, key := range []string{"user_deposit", "user_deposit_min", "user_deposit_day"} {
		raw, exists := fields[key]
		if !exists {
			continue
		}
		value, err := depositScalar(raw)
		if err != nil {
			return out, err
		}
		if value == "" || value == "_" {
			continue
		}
		switch key {
		case "user_deposit":
			if value != "0" && value != "1" {
				return out, ErrDepositPolicy
			}
			out.Enabled = value == "1"
		case "user_deposit_min":
			n, err := ledger.Parse(value)
			if err != nil {
				return out, ErrDepositPolicy
			}
			out.MinCents = n
		case "user_deposit_day":
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 || n > 3660 || strconv.Itoa(n) != value {
				return out, ErrDepositPolicy
			}
			out.Days = n
		}
	}
	return out, nil
}

func ReadDepositPolicy(ctx context.Context, q Reader, mid int64, lock bool) (DepositPolicy, error) {
	return readDepositPolicy(ctx, q, mid, lock, false)
}

// Historical summaries may describe an inactive merchant. This does not grant
// permission for new deposits or orders; those always use ReadDepositPolicy.
func ReadDepositPolicyForSummary(ctx context.Context, q Reader, mid int64) (DepositPolicy, error) {
	return readDepositPolicy(ctx, q, mid, false, true)
}

func readDepositPolicy(ctx context.Context, q Reader, mid int64, lock, summary bool) (DepositPolicy, error) {
	var out DepositPolicy
	if q == nil || mid <= 0 {
		return out, ErrDepositPolicy
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	rows, err := q.Query(ctx, `SELECT body->>'key',COALESCE(body->>'value','') FROM backoffice_records WHERE resource='settings' AND body->>'key' IN ('user_deposit','user_deposit_min','user_deposit_day') ORDER BY id`+suffix)
	if err != nil {
		return out, err
	}
	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			rows.Close()
			return out, err
		}
		if _, seen := values[key]; !seen {
			values[key] = value
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	var group EffectiveGroup
	if summary {
		group, err = ReadEffectiveGroupForSetup(ctx, q, mid, lock)
	} else {
		group, err = ReadEffectiveGroup(ctx, q, mid, lock)
	}
	if err != nil {
		return out, err
	}
	out, err = ResolveDepositPolicy(values, group.Body)
	if err != nil {
		return out, err
	}
	out.GroupID, out.ValidUntil = group.ID, group.ValidUntil
	return out, nil
}

// Existing rows are locked during an intent, but missing global settings have
// no row to lock. Re-read after authorization/ledger waits and before commit so
// a first insertion cannot silently change the policy used by that intent.
func (policy DepositPolicy) Recheck(ctx context.Context, q Reader, mid int64) error {
	if err := policy.CheckDeadline(ctx, q); err != nil {
		return err
	}
	current, err := ReadDepositPolicy(ctx, q, mid, true)
	if err != nil {
		return err
	}
	if policy.globalHash != current.globalHash || policy.Enabled != current.Enabled || policy.MinCents != current.MinCents || policy.Days != current.Days || policy.GroupID != current.GroupID || !policy.ValidUntil.Equal(current.ValidUntil) {
		return ErrDepositPolicyChanged
	}
	return current.CheckDeadline(ctx, q)
}

func (policy DepositPolicy) CheckDeadline(ctx context.Context, q Reader) error {
	if policy.ValidUntil.IsZero() {
		return nil
	}
	var now time.Time
	if err := q.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if !now.Before(policy.ValidUntil) {
		return ErrDepositPolicyChanged
	}
	return nil
}
