package payment

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"goepay/internal/ledger"
)

// Per-merchant settlement reserve (原 pre_user.remain_money / 留存).
//
// epay_pro app/cron/Settle.php applies each merchant's own remain_money to the
// settleable amount before the minimum/fee check. A value containing '%' keeps
// that percentage of the balance (money*(1-rate/100)); any other value is a
// fixed yuan amount subtracted from the balance. GOEPAY keeps this as the per-
// merchant retained reserve fed into the scheduled-settlement quote, on top of
// the global settlement_retained_cents floor. The threshold path (原 Transfer.php)
// deliberately does NOT apply it — it clamps with the maximum fee only.
//
// The resolved value is integer cents. The original rounded yuan to two
// decimals (round(money*(1-rate/100),2) vs round(money-remain,2)); the cents
// forms below reproduce those boundaries: retainedByBPS is the same half-up
// percentage retain the global policy uses, and a fixed yuan string parses to
// exact cents.

// resolveRemainMoney maps a merchant's remain_money string + current eligible
// cents to the reserve the original would have kept. An empty/zero/invalid
// setting keeps nothing (0). A percentage outside (0,100) is treated as a fixed
// amount exactly as the original's else-branch did.
func resolveRemainMoney(remain string, eligible int64) (int64, error) {
	remain = strings.TrimSpace(remain)
	if remain == "" || eligible <= 0 {
		return 0, nil
	}
	if strings.Contains(remain, "%") {
		rate := strings.TrimSpace(strings.TrimSuffix(remain, "%"))
		// A percentage is yuan-free: e.g. "10" or "12.5" percent. Convert to
		// basis points with the same two-decimal precision the original allowed.
		bps, e := ledger.Parse(rate)
		if e != nil {
			return 0, ErrInvalid
		}
		if bps > 0 && bps < 10000 {
			return retainedByBPS(eligible, bps)
		}
		// 0%, 100% or out of range: the original's else-branch subtracted the
		// numeric part as a fixed yuan amount (money - remain_money).
		return clampReserve(bps, eligible), nil
	}
	cents, e := ledger.Parse(remain)
	if e != nil || cents < 0 {
		return 0, ErrInvalid
	}
	return clampReserve(cents, eligible), nil
}

// clampReserve never reserves more than the eligible balance; the scheduled
// quote then yields zero gross and the item is skipped (insufficient_funds),
// matching the original money<settle_money continue.
func clampReserve(reserve, eligible int64) int64 {
	if reserve < 0 {
		return 0
	}
	if reserve > eligible {
		return eligible
	}
	return reserve
}

// merchantRemainMoney reads the remain_money profile string for one merchant
// under the caller's lock. Missing/non-string yields "" (keep nothing).
func merchantRemainMoney(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, mid int64) (string, error) {
	var raw []byte
	if e := q.QueryRow(ctx, "SELECT COALESCE(profile->>'remain_money','') FROM merchants WHERE id=$1", mid).Scan(&raw); e != nil {
		return "", e
	}
	return string(raw), nil
}

// settlementRetainedFor resolves the reserve applied to a merchant's scheduled
// settlement: the larger of the global floor and the per-merchant remain_money.
// The scheduled quote requires retained >= policy.RetainedCents, so the max
// keeps that invariant while honouring a merchant's own reserve.
func settlementRetainedFor(ctx context.Context, tx pgx.Tx, mid, globalRetained, eligible int64) (int64, error) {
	remain, e := merchantRemainMoney(ctx, tx, mid)
	if e != nil {
		return 0, e
	}
	perMerchant, e := resolveRemainMoney(remain, eligible)
	if e != nil {
		return 0, e
	}
	if perMerchant > globalRetained {
		return perMerchant, nil
	}
	return globalRetained, nil
}
