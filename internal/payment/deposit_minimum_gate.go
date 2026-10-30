package payment

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"goepay/internal/ledger"
)

// ErrDepositBelowMinimum refuses a payment order while the merchant holds less
// 保证金 than the platform minimum. It wraps ErrState, so every caller that
// already treats a refused merchant as a state error keeps doing so.
var ErrDepositBelowMinimum = fmt.Errorf("%w: 保证金低于平台最低要求，暂不能发起支付", ErrState)

// depositMinimumGateTx is epay_pro's 当前平台要求最低缴纳保证金 N 元，否则无法调用
// 接口发起支付: when user_deposit_min is above zero and the deposit feature is not
// switched off (user_deposit "0"), a payment order is refused until the
// merchant's held deposit reaches the minimum. Recharge orders are not gated,
// so a merchant can always top up and then move funds into the deposit. An
// unset, empty or malformed minimum leaves payments unconditional, which is
// today's behaviour. The first settings row by id wins, as for user_deposit_day.
func depositMinimumGateTx(ctx context.Context, tx pgx.Tx, merchantID int64) error {
	rows, e := tx.Query(ctx, `SELECT body->>'key',COALESCE(body->>'value','') FROM backoffice_records WHERE resource='settings' AND body->>'key' IN ('user_deposit','user_deposit_min') ORDER BY id`)
	if e != nil {
		return e
	}
	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if e = rows.Scan(&key, &value); e != nil {
			rows.Close()
			return e
		}
		if _, seen := values[key]; !seen {
			values[key] = value
		}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	if values["user_deposit"] == "0" || values["user_deposit_min"] == "" {
		return nil
	}
	minimum, e := ledger.Parse(values["user_deposit_min"])
	if e != nil || minimum <= 0 {
		return nil
	}
	var held int64
	if e = tx.QueryRow(ctx, "SELECT COALESCE(sum(balance),0) FROM accounts WHERE owner_id=$1 AND kind='deposit'", merchantID).Scan(&held); e != nil {
		return e
	}
	if held < minimum {
		return ErrDepositBelowMinimum
	}
	return nil
}
