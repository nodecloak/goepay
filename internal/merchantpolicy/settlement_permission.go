package merchantpolicy

import (
	"context"
	"encoding/json"
	"errors"
)

var ErrSettlementPermission = errors.New("invalid merchant settlement permission")

type SettlementPermission struct {
	Allowed  bool
	Revision int64
}

// The original settle field wins. The previous Go settle_status spelling is a
// fallback only; an absent permission keeps historical Go defaults enabled.
func ResolveSettlementPermission(profile json.RawMessage) (bool, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(profile, &fields) != nil || fields == nil {
		return false, ErrSettlementPermission
	}
	raw, ok := fields["settle"]
	if !ok {
		raw, ok = fields["settle_status"]
	}
	if !ok {
		return true, nil
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		text = string(raw)
	}
	switch text {
	case "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, ErrSettlementPermission
	}
}

func ReadSettlementPermission(ctx context.Context, q Reader, mid int64, lock bool) (SettlementPermission, error) {
	var out SettlementPermission
	if q == nil || mid <= 0 {
		return out, ErrSettlementPermission
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	var raw []byte
	if e := q.QueryRow(ctx, "SELECT profile,settlement_permission_revision FROM merchants WHERE id=$1"+suffix, mid).Scan(&raw, &out.Revision); e != nil {
		return out, e
	}
	if out.Revision <= 0 {
		return out, ErrSettlementPermission
	}
	var e error
	out.Allowed, e = ResolveSettlementPermission(raw)
	return out, e
}
