package server

import (
	"net/url"
	"strings"
	"testing"
)

func TestSubchannel168BOrderFilterUsesActualOwnedRouteFKWithExactID(t *testing.T) {
	where, args, e := consoleListFilters(url.Values{"subchannel": {"9007199254740993"}}, "merchant", "orders", "o.merchant_id", 1, []any{int64(1)})
	if e != nil || !strings.Contains(where, "payment_subchannel_routes") || !strings.Contains(where, "merchant_id=o.merchant_id") || !strings.Contains(where, "out_trade_no=o.out_trade_no") || len(args) != 2 || args[1] != int64(9007199254740993) {
		t.Fatal("original subchannel order navigation is not an exact route FK predicate", where, args, e)
	}
	for _, q := range []url.Values{{"subchannel": {"7", "8"}}, {"subchannel": {"7"}, "subchannel_id": {"8"}}, {"subchannel": {"07"}}, {"subchannel": {"7 OR 1=1"}}} {
		if _, _, e = consoleListFilters(q, "merchant", "orders", "o.merchant_id", 1, nil); e == nil {
			t.Fatal("ambiguous or unsafe child filter accepted", q)
		}
	}
	if _, _, e = consoleListFilters(url.Values{"subchannel": {"7"}}, "merchant", "transfers", "p.merchant_id", 1, nil); e == nil {
		t.Fatal("payment child ID crossed into payout workflow")
	}
}
