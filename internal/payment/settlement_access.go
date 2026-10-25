package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrSettlementMode = fmt.Errorf("%w: current settlement mode does not permit this new request", ErrState)

// Kept outside signed SettlementPolicy and historical plan scopes. New business
// permission checks must not change serialization of already authenticated facts.
type SettlementAccess struct {
	Mode             string `json:"mode"`
	ManualAllowed    bool   `json:"manual_allowed"`
	AutomaticAllowed bool   `json:"automatic_allowed"`
	Source           string `json:"source"`
	GroupID          int64  `json:"group_id,string"`
}

func settlementAccessMode(mode, source string, gid int64) (SettlementAccess, error) {
	if mode != "0" && mode != "1" && mode != "2" && mode != "3" {
		return SettlementAccess{}, ErrInvalid
	}
	return SettlementAccess{Mode: mode, ManualAllowed: mode == "2" || mode == "3", AutomaticAllowed: mode == "1" || mode == "3", Source: source, GroupID: gid}, nil
}
func readSettlementAccess(ctx context.Context, q referralReader, mid int64, threshold, lock bool) (SettlementAccess, error) {
	if lock {
		// Membership renewal and its expiry must be observed after any row wait.
		// The caller already holds the merchant row before reserving funds.
		var member int64
		e := q.QueryRow(ctx, "SELECT merchant_id FROM memberships WHERE merchant_id=$1 FOR SHARE", mid).Scan(&member)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return SettlementAccess{}, e
		}
	}
	var at time.Time
	if e := q.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&at); e != nil {
		return SettlementAccess{}, e
	}
	gid, _, enabled, e := membershipEffectiveGroup(ctx, q, mid, at)
	if e != nil {
		return SettlementAccess{}, e
	}
	if !enabled || gid < 0 {
		return SettlementAccess{}, ErrState
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	if gid > 0 {
		var raw json.RawMessage
		if e = q.QueryRow(ctx, `SELECT body->'settle_open' FROM backoffice_records WHERE resource='groups' AND id=$1`+suffix, gid).Scan(&raw); e != nil {
			return SettlementAccess{}, e
		}
		group, e := thresholdGroupScalar(raw)
		if e != nil {
			return SettlementAccess{}, e
		}
		switch group {
		case "", "0", "_":
		case "1", "2", "3":
			return settlementAccessMode(group, "group", gid)
		default:
			return SettlementAccess{}, ErrInvalid
		}
	}
	// Release's auto_settle_money branch is independent of the ordinary global
	// settlement switch; it only excludes a group configured manual-only (2).
	if threshold {
		return settlementAccessMode("3", "threshold_default", gid)
	}
	var raw json.RawMessage
	e = q.QueryRow(ctx, `SELECT body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='settle_open'`+suffix).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return settlementAccessMode("3", "legacy_default", gid)
	}
	if e != nil {
		return SettlementAccess{}, e
	}
	mode, e := thresholdGroupScalar(raw)
	if e != nil {
		return SettlementAccess{}, e
	}
	return settlementAccessMode(mode, "global", gid)
}

func requireSettlementPlanAutomaticAccess(ctx context.Context, tx pgx.Tx, p settlementPlanRecord) error {
	// Threshold plans keep their separate original eligibility and signed scope.
	if p.Input.Mode == settlementThresholdMode {
		return nil
	}
	allowed := false
	for _, mid := range p.Input.MerchantIDs {
		access, err := readSettlementAccess(ctx, tx, mid, false, true)
		if err != nil {
			return err
		}
		allowed = allowed || access.AutomaticAllowed
	}
	if !allowed {
		return ErrSettlementMode
	}
	return nil
}

func requireNewSettlementAccess(ctx context.Context, tx pgx.Tx, mid int64) error {
	_, threshold := ctx.Value(settlementThresholdRequestKey{}).(*settlementThresholdRequest)
	_, scheduled := ctx.Value(settlementPlanRequestWindowKey{}).(settlementPlanRequestWindow)
	access, e := readSettlementAccess(ctx, tx, mid, threshold, true)
	if e != nil {
		return e
	}
	if (threshold || scheduled) && !access.AutomaticAllowed || !threshold && !scheduled && !access.ManualAllowed {
		return ErrSettlementMode
	}
	return nil
}
