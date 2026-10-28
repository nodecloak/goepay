package payment

import "goepay/internal/ledger"

// Combine207 is the original WeChat combined-order configuration
// (admin/set.php 微信开启合单支付 / 合单支付最小金额 / 合单支付子单最大金额):
// wxcombine_open, wxcombine_minmoney and wxcombine_submoney, in yuan text.
type Combine207 struct {
	Open, MinMoney, SubMoney string
}

const (
	combineMinSubOrders207 = 3
	combineMaxSubOrders207 = 50
)

// yuanCents207 reads an administrator yuan amount as exact cents. Empty or
// malformed text is 0, like PHP intval(floatval(x)*100) on non-numeric
// input; unlike PHP it never loses a cent to float truncation (0.29 -> 29).
func yuanCents207(raw string) int64 {
	if raw == "" {
		return 0
	}
	v, err := ledger.Parse(raw)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// SplitCombine207 mirrors combinepay_submoneys (includes/functions.php): with
// combining on and a minimum set, an amount >= minimum is split into 3..50
// sub-orders, starting at 3 and adding one while the even share exceeds the
// sub-order cap; the remainder adds one cent to the first sub-orders. ok=false
// means an ordinary (single) order. Every sub-order is at least one cent and
// the shares always sum to total.
func SplitCombine207(c Combine207, total int64) ([]int64, bool) {
	if c.Open != "1" || total <= 0 {
		return nil, false
	}
	minimum := yuanCents207(c.MinMoney)
	if minimum <= 0 || total < minimum || total < combineMinSubOrders207 {
		return nil, false
	}
	capacity := yuanCents207(c.SubMoney)
	n := int64(combineMinSubOrders207)
	share := total / n
	for share > capacity && n < combineMaxSubOrders207 {
		n++
		share = total / n
	}
	out := make([]int64, n)
	for i := range out {
		out[i] = share
	}
	for i := int64(0); i < total%n; i++ {
		out[i]++
	}
	return out, true
}
