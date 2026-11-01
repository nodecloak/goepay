package merchantpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type GroupFeatureAccess struct {
	Enabled          bool
	GlobalConfigured bool
	ValidUntil       time.Time
}

// ReadGroupFeatureAccess uses the caller's transaction. A monetary caller must
// hold its merchant row first and request locks, so existing global/group rows
// cannot change between this decision and the durable new intent/send claim.
func ReadGroupFeatureAccess(ctx context.Context, q Reader, mid int64, key string, lock bool) (GroupFeatureAccess, error) {
	var out GroupFeatureAccess
	if !GroupFeatureKey(key) || q == nil || mid <= 0 {
		return out, ErrFeatureSwitch
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	var raw json.RawMessage
	err := q.QueryRow(ctx, `SELECT body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=$1 ORDER BY id LIMIT 1`+suffix, key).Scan(&raw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	out.Enabled = true
	if err == nil {
		out.GlobalConfigured = true
		value, parse := scalar(raw, true)
		if parse != nil || value != "0" && value != "1" {
			return GroupFeatureAccess{}, ErrFeatureSwitch
		}
		out.Enabled = value == "1"
	}
	group, err := ReadEffectiveGroup(ctx, q, mid, lock)
	if err != nil {
		return GroupFeatureAccess{}, err
	}
	override, err := GroupFeatureSwitch(group.Body, key)
	if err != nil {
		return GroupFeatureAccess{}, err
	}
	if override.Configured {
		out.Enabled = override.Enabled
	}
	out.ValidUntil = group.ValidUntil
	return out, nil
}
