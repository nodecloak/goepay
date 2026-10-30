package payment

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// CurrentMembership is the merchant's 当前会员等级 header (epay_pro group/grouplist
// pe.my_group / my_gid / endtime / can_renew). GroupName is the effective group
// (purchased membership if live, else the baseline group). EndTime is the
// purchased membership's expiry when one is active, "永久" for a permanent
// membership, and "" when the merchant is on the baseline group with no
// purchase. CanRenew is true when a non-permanent purchased membership is
// active and its group is still on sale — the 续期 button then reopens the buy
// dialog preselected to MyGID.
type CurrentMembership struct {
	MyGroup  string `json:"my_group"`
	MyGID    string `json:"my_gid"`
	EndTime  string `json:"endtime"`
	CanRenew bool   `json:"can_renew"`
}

// CurrentMembershipHeader resolves the merchant's current membership for the
// 购买会员 page header. It reads the same memberships/groups tables the purchase
// flow uses, never mutating anything. A missing merchant yields ErrNotFound.
func (s *Store) CurrentMembershipHeader(ctx context.Context, mid int64) (CurrentMembership, error) {
	var out CurrentMembership
	if mid <= 0 {
		return out, ErrInvalid
	}
	var at time.Time
	if e := s.pool.QueryRow(ctx, "SELECT pg_catalog.clock_timestamp()").Scan(&at); e != nil {
		return out, e
	}
	group, _, enabled, e := membershipEffectiveGroup(ctx, s.pool, mid, at)
	if e != nil {
		return out, e
	}
	if !enabled {
		return out, ErrState
	}
	// Name of the effective group (the baseline or the purchased one).
	var name string
	if e = s.pool.QueryRow(ctx, "SELECT COALESCE(body->>'name','') FROM backoffice_records WHERE resource='groups' AND id=$1", group).Scan(&name); e != nil {
		if !errors.Is(e, pgx.ErrNoRows) {
			return out, e
		}
	}
	out.MyGroup = name
	// Active purchased membership (if any) decides end time and renewal.
	var purchasedGroup int64
	var expires time.Time
	e = s.pool.QueryRow(ctx, "SELECT group_id,expires_at FROM memberships WHERE merchant_id=$1 AND expires_at>$2", mid, at).Scan(&purchasedGroup, &expires)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	if expires.Equal(permanentMembershipExpiry) {
		out.EndTime = "永久"
		return out, nil
	}
	out.EndTime = expires.In(membershipSaleZone).Format("2006-01-02 15:04:05")
	// 续期 is offered only when the purchased group is still on sale: group_buy
	// enabled, the group row enabled and sale_enabled (the catalog's own gate).
	var global string
	if e = s.pool.QueryRow(ctx, `SELECT COALESCE((SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='group_buy'),'true')`).Scan(&global); e != nil {
		return out, e
	}
	if global != "true" {
		return out, nil
	}
	var saleable bool
	if e = s.pool.QueryRow(ctx,
		`SELECT COALESCE((SELECT body->>'enabled'='true' AND COALESCE(body->>'sale_enabled','true')<>'false' FROM backoffice_records WHERE resource='groups' AND id=$1),false)`,
		purchasedGroup).Scan(&saleable); e != nil {
		return out, e
	}
	if saleable {
		out.MyGID = strconv.FormatInt(purchasedGroup, 10)
		out.CanRenew = true
	}
	return out, nil
}
