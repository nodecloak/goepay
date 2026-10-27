package migration

import (
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"goepay/internal/ledger"
)

// These are sanitized historical assertions, not executable provider requests.
type CombinedOrderSnapshot204 struct {
	TradeNo      string                `json:"trade_no"`
	MerchantID   int64                 `json:"merchant_id"`
	SourcePlugin string                `json:"source_plugin,omitempty"`
	Currency     string                `json:"currency"`
	ChargedMoney string                `json:"charged_money"`
	Settle       int                   `json:"settle"`
	SubOrders    []SubOrderSnapshot204 `json:"sub_orders"`
}
type SubOrderSnapshot204 struct {
	SubTradeNo  string  `json:"sub_trade_no"`
	APITradeNo  string  `json:"api_trade_no"`
	Money       string  `json:"money"`
	RefundMoney *string `json:"refundmoney"`
	Status      int     `json:"status"`
	Settle      int     `json:"settle"`
}

func combinedText204(value string, max int, empty bool) bool {
	return (empty || value != "") && utf8.ValidString(value) && len(value) <= max && strings.IndexFunc(value, unicode.IsControl) < 0
}

func validateCombinedOrders204(plan Plan, orders map[string]Order, issue func(string, string)) {
	groups := map[string]bool{}
	children := map[string]bool{}
	for i, g := range plan.CombinedOrders {
		path := fmt.Sprintf("combined_orders[%d]", i)
		parent, exists := orders[g.TradeNo]
		if !exists || g.MerchantID <= 0 || parent.MerchantID != g.MerchantID {
			issue(path, "unknown_or_foreign_parent")
		}
		if groups[g.TradeNo] {
			issue(path, "duplicate_combined_parent")
		}
		groups[g.TradeNo] = true
		if !combinedText204(g.SourcePlugin, 64, true) || g.Currency != "CNY" || g.Settle < 0 || g.Settle > 3 {
			issue(path, "invalid_combined_source")
		}
		if (g.SourcePlugin == "wxpayn" || g.SourcePlugin == "wxpaynp") && parent.Type != "wxpay" && parent.Type != "qqpay" || g.SourcePlugin == "alipayd" && parent.Type != "alipay" {
			issue(path, "inconsistent_historical_plugin")
		}
		if len(g.SubOrders) < 1 || len(g.SubOrders) > 50 || g.SourcePlugin == "alipayd" && len(g.SubOrders) > 6 {
			issue(path, "unbounded_suborders")
		}
		charge, err := ledger.Parse(g.ChargedMoney)
		if err != nil || charge <= 0 {
			issue(path+".charged_money", "invalid_money")
		}
		var total int64
		for j, c := range g.SubOrders {
			childPath := fmt.Sprintf("%s.sub_orders[%d]", path, j)
			if !combinedText204(c.SubTradeNo, 25, false) || !combinedText204(c.APITradeNo, 150, true) {
				issue(childPath, "invalid_suborder_identity")
			}
			if children[c.SubTradeNo] {
				issue(childPath, "duplicate_suborder")
			}
			children[c.SubTradeNo] = true
			if _, cycle := orders[c.SubTradeNo]; cycle {
				issue(childPath, "ordinary_order_as_suborder")
			}
			money, e := ledger.Parse(c.Money)
			if e != nil || money <= 0 {
				issue(childPath+".money", "invalid_money")
			} else if money > math.MaxInt64-total {
				issue(childPath, "money_overflow")
			} else {
				total += money
			}
			if c.RefundMoney != nil {
				refund, e := ledger.Parse(*c.RefundMoney)
				if e != nil || refund < 0 || refund > money {
					issue(childPath+".refundmoney", "invalid_refund_amount")
				}
			}
			if c.Status < 0 || c.Status > 4 || c.Settle < 0 || c.Settle > 1 {
				issue(childPath, "invalid_historical_state")
			}
		}
		if err == nil && total != charge {
			issue(path, "suborder_total_mismatch")
		}
	}
}
