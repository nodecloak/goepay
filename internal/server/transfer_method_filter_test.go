package server

import (
	"net/url"
	"strings"
	"testing"
)

// 付款方式 filter on the merchant transfer list: only GOEPAY's rails bind to
// provider_transfer_attempts.method; anything else (including Pro-only 记账本调拨)
// is refused, never silently becoming an unfiltered list.
func TestConsoleListFiltersTransferMethod(t *testing.T) {
	for _, typ := range []string{"alipay", "wxpay", "qqpay", "bank"} {
		q := url.Values{"type": {typ}}
		clause, args, err := consoleListFilters(q, "merchant", "transfers", "t.merchant_id", 1, []any{int64(1), ""})
		if err != nil {
			t.Fatalf("rail %q must be accepted: %v", typ, err)
		}
		if !strings.Contains(clause, "p.method=$") {
			t.Fatalf("rail %q must bind p.method, got clause %q", typ, clause)
		}
		if args[len(args)-1] != typ {
			t.Fatalf("rail %q must be bound as the last arg, got %v", typ, args[len(args)-1])
		}
	}
	for _, bad := range []string{"fundbook", "记账本调拨", "sandbox", "unknown"} {
		q := url.Values{"type": {bad}}
		if _, _, err := consoleListFilters(q, "merchant", "transfers", "t.merchant_id", 1, []any{int64(1), ""}); err == nil {
			t.Fatalf("non-GOEPAY transfer type %q must be refused", bad)
		}
	}
	// An empty / absent type leaves the list unfiltered (全部方式).
	clause, _, err := consoleListFilters(url.Values{}, "merchant", "transfers", "t.merchant_id", 1, []any{int64(1), ""})
	if err != nil || strings.Contains(clause, "p.method") {
		t.Fatalf("absent type must not add a method clause: %q %v", clause, err)
	}
}
