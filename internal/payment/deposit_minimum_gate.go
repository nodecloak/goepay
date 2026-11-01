package payment

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"goepay/internal/merchantpolicy"
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
// unset, empty or malformed global minimum keeps its legacy default. Effective
// group overrides are strict, and the returned deadline is checked at commit.
func depositMinimumGateTx(ctx context.Context, tx pgx.Tx, merchantID int64) (merchantpolicy.DepositPolicy, error) {
	policy, e := readMerchantDepositPolicy(ctx, tx, merchantID)
	if e != nil {
		return policy, e
	}
	if !policy.Enabled || policy.MinCents <= 0 {
		return policy, nil
	}
	var held int64
	if e = tx.QueryRow(ctx, "SELECT COALESCE(sum(balance),0) FROM accounts WHERE owner_id=$1 AND kind='deposit'", merchantID).Scan(&held); e != nil {
		return policy, e
	}
	if held < policy.MinCents {
		return policy, ErrDepositBelowMinimum
	}
	return policy, nil
}
