package server

import (
	"net/url"
	"strings"
	"testing"
)

func TestProOrderSearchFixedFieldsAndExactMoney(t *testing.T) {
	for _, tc := range []struct {
		field, raw, fragment string
		value                any
	}{
		{"trade_no", "a%_b", "o.trade_no=$2::text", "a%_b"},
		{"out_trade_no", "' OR 1=1--", "o.out_trade_no=$2::text", "' OR 1=1--"},
		{"api_trade_no", "original-upstream", "console_receipt.provider_trade_no", "original-upstream"},
		{"name", "%_product", "strpos(lower(o.name),lower($2::text))>0", "%_product"},
		{"param", "A&B", "strpos(lower(o.param),lower($2::text))>0", "A&B"},
		{"money", "0.01", "o.amount=$2::bigint", int64(1)},
		{"realmoney", "92233720368547758.07", "COALESCE((to_jsonb(o)->>'actual_charge_amount104')::bigint,o.amount)=$2::bigint", int64(9223372036854775807)},
		{"getmoney", "0.00", "o.merchant_amount=$2::bigint", int64(0)},
		{"buyer", "buyer-secret", "console_authorization.buyer_id", "buyer-secret"},
		{"ip", "2001:0db8:0:0:0:0:0:1", "console_report.client_ip=$2::text", "2001:db8::1"},
		{"domain", "pay.Example.com", "strpos(lower(COALESCE(NULLIF(substring(o.notify_url", "pay.Example.com"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			clause, value, e := consoleOrderSearch(url.Values{"search_field": {tc.field}, "q": {tc.raw}}, "orders", "fallback")
			if e != nil || !strings.Contains(clause, tc.fragment) || value != tc.value {
				t.Fatalf("search=%q value=%#v error=%v", clause, value, e)
			}
			if strings.Contains(clause, tc.raw) {
				t.Fatal("untrusted search value embedded into SQL")
			}
		})
	}
	clause, value, e := consoleOrderSearch(url.Values{"q": {"old generic"}}, "orders", "fixed_legacy_expression")
	if e != nil || !strings.Contains(clause, "fixed_legacy_expression") || value != "old generic" {
		t.Fatal(clause, value, e)
	}
}

func TestProOrderSearchRejectsAmbiguousUnsupportedAndInvalidValues(t *testing.T) {
	for _, q := range []url.Values{
		{"search_field": {"money"}, "q": {"1.001"}},
		{"search_field": {"money"}, "q": {"1e2"}},
		{"search_field": {"money"}, "q": {"-1"}},
		{"search_field": {"money"}, "q": {"92233720368547758.08"}},
		{"search_field": {"ip"}, "q": {"not-ip"}},
		{"search_field": {"mobile"}, "q": {"123"}},
		{"search_field": {"name; DROP TABLE orders"}, "q": {"x"}},
		{"search_field": {"name", "money"}, "q": {"x"}},
		{"q": {"x", "y"}},
		{"q": {strings.Repeat("x", 257)}},
	} {
		if _, _, e := consoleOrderSearch(q, "orders", "fixed"); e == nil {
			t.Errorf("accepted invalid query: %#v", q)
		}
	}
	if _, _, e := consoleOrderSearch(url.Values{"search_field": {"name"}, "q": {"x"}}, "refunds", "fixed"); e == nil {
		t.Fatal("order field accepted for another resource")
	}
	if _, _, e := consoleOrderSearch(url.Values{"search_field": {"money"}}, "orders", "fixed"); e != nil {
		t.Fatal("empty input should leave selected valid field inactive", e)
	}
}

func TestProNotificationFilterRequiresLatestDisplayedState(t *testing.T) {
	for _, state := range []string{"none", "pending", "running", "succeeded", "dead"} {
		clause, args, e := consoleListFilters(url.Values{"notification_state": {state}}, "merchant", "orders", "o.merchant_id", 7, []any{int64(7), ""})
		if e != nil || !strings.Contains(clause, "COALESCE(console_notification.state,'none')=$3") || len(args) != 3 || args[2] != state {
			t.Fatal(state, clause, args, e)
		}
	}
	for _, q := range []url.Values{{"notification_state": {"paid"}}, {"notification_state": {"pending", "dead"}}} {
		if _, _, e := consoleListFilters(q, "admin", "orders", "o.merchant_id", 0, nil); e == nil {
			t.Fatal("ambiguous/invalid notification state accepted")
		}
	}
	if _, _, e := consoleListFilters(url.Values{"notification_state": {"dead"}}, "merchant", "refunds", "f.merchant_id", 7, nil); e == nil {
		t.Fatal("notification field accepted outside orders")
	}
}
