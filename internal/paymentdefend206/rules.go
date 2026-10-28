package paymentdefend206

import (
	"context"
	"math"
	"math/big"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// maxWindowSeconds bounds PHP intval() of an unbounded digit string to a
// window PostgreSQL can represent; ten years covers every real order history.
const maxWindowSeconds = 315360000

func phpIntval(raw string) int64 {
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return math.MaxInt64 // validated digits that overflow clamp like PHP_INT_MAX
	}
	return n
}

// roundHalfUp2 is PHP round($x, 2) for a non-negative exact rational.
func roundHalfUp2(x *big.Rat) *big.Rat {
	scaled := new(big.Rat).Mul(x, big.NewRat(100, 1))
	scaled.Add(scaled, big.NewRat(1, 2))
	floor := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	return new(big.Rat).SetFrac(floor, big.NewInt(100))
}

// EvaluateRulesTx is the original pay_verify=1 rule evaluation (checkPayVerifyOpen):
//   - rate rule: when second/count/rate is set, the merchant's orders created in
//     the last `second` seconds (database clock) are counted; when total>=count
//     and round(success*100/total,2) < rate the trigger fires. "Success" is the
//     original status>0, mapped to this system's paid status. A zero total
//     follows the PHP 7 result of a division warning (false -> rate 0).
//   - IP rule: the last `ip` orders from clientIP within 3600s (all merchants,
//     as the original) are all unpaid -> fires.
//
// The caller has already applied the uid pre-filter.
func EvaluateRulesTx(ctx context.Context, tx pgx.Tx, s Settings, merchantID int64, clientIP string) (bool, error) {
	if tx == nil || merchantID <= 0 {
		return false, ErrInvalid
	}
	second := phpIntval(s["pay_verify_check_second"])
	count := phpIntval(s["pay_verify_check_count"])
	rate, err := ParseRate(s["pay_verify_check_rate"])
	if err != nil {
		return false, ErrConflict
	}
	if rate == nil {
		rate = new(big.Rat)
	}
	if second > 0 || count > 0 || rate.Sign() > 0 {
		window := second
		if window > maxWindowSeconds {
			window = maxWindowSeconds
		}
		var total, success int64
		if err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status='paid') FROM orders
 WHERE merchant_id=$1 AND created_at>=clock_timestamp()-make_interval(secs=>$2::double precision)`, merchantID, float64(window)).Scan(&total, &success); err != nil {
			return false, err
		}
		if total >= count {
			successRate := new(big.Rat)
			if total > 0 {
				successRate = roundHalfUp2(big.NewRat(success*100, total))
			}
			if successRate.Cmp(rate) < 0 {
				return true, nil
			}
		}
	}
	ipcheck := phpIntval(s["pay_verify_check_ip"])
	if ipcheck > 0 && clientIP != "" {
		var unpaid, seen int64
		if err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status<>'paid'),count(*) FROM(
 SELECT o.status FROM orders o JOIN order_reporting r ON r.order_id=o.id
 WHERE r.client_ip=$1 AND o.created_at>=clock_timestamp()-interval '3600 seconds'
 ORDER BY o.created_at DESC,o.id DESC LIMIT $2) recent`, clientIP, ipcheck).Scan(&unpaid, &seen); err != nil {
			return false, err
		}
		if unpaid >= ipcheck {
			return true, nil
		}
	}
	return false, nil
}
