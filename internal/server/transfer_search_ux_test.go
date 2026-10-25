package server

import (
	"net/url"
	"strings"
	"testing"
)

func TestTransferSearchUXFixedOriginalPublicFieldsAndExactMoney(t *testing.T) {
	for _, v := range []struct {
		field, input, expression string
		value                    any
	}{
		{"biz_no", "1000000000000000001", "t.biz_no=$2::text", "1000000000000000001"},
		{"out_biz_no", "original_%", "t.transfer_no=$2::text", "original_%"},
		{"pay_order_no", "ali-order-151", "COALESCE(p.provider_transfer_no,'')=$2::text", "ali-order-151"},
		{"money", "90071992547409.93", "t.amount=$2::bigint", int64(9007199254740993)},
	} {
		clause, value, e := consoleOrderSearch(url.Values{"search_field": {v.field}, "q": {v.input}}, "transfers", "t.transfer_no")
		if e != nil || clause != v.expression || value != v.value {
			t.Fatalf("original field %s did not map precisely: %q %v %v", v.field, clause, value, e)
		}
	}
	for _, field := range []string{"biz_no", "out_biz_no", "pay_order_no", "money"} {
		clause, value, e := consoleOrderSearch(url.Values{"search_field": {field}}, "transfers", "t.transfer_no")
		if e != nil || clause != "$2::text=''" || value != "" {
			t.Fatal("empty original search should not filter")
		}
	}
}

func TestTransferSearchUXUnknownPrivateAndInvalidParametersRejected(t *testing.T) {
	for _, q := range []url.Values{
		{"search_field": {"unknown"}, "q": {"x"}}, {"search_field": {"account"}, "q": {"private"}}, {"search_field": {"username"}, "q": {"private"}},
		{"search_field": {"money"}, "q": {"1.001"}}, {"search_field": {"money"}, "q": {"-1"}},
		{"search_field": {"money"}, "q": {"92233720368547758.08"}},
		{"search_field": {"biz_no", "out_biz_no"}, "q": {"x"}}, {"q": {"x", "y"}},
		{"search_field": {"biz_no"}, "q": {"bad\x00"}}, {"q": {"bad\n"}}, {"q": {strings.Repeat("中", 86)}},
	} {
		if _, _, e := consoleOrderSearch(q, "transfers", "t.transfer_no"); e == nil {
			t.Fatal("invalid original transfer search accepted")
		}
	}
}

func TestTransferSearchUXQueryDoesNotSilentlyAcceptUnknownOrDuplicateScopes(t *testing.T) {
	for _, q := range []url.Values{
		{"unknown": {"x"}}, {"account": {"private"}}, {"type": {"alipay"}},
		{"page": {"1", "2"}}, {"limit": {"10", "20"}}, {"page": {"01"}}, {"limit": {""}},
		{"merchant_id": {"01"}}, {"merchant_id": {"+1"}}, {"channel_id": {"07"}},
		{"merchant_id": {"1", "2"}}, {"search_field": {"biz_no", "money"}},
	} {
		if _, _, e := consoleListFilters(q, "admin", "transfers", "t.merchant_id", 0, []any{int64(0), ""}); e == nil {
			t.Fatal("invalid or unknown scope accepted")
		}
	}
	if _, _, e := consoleListFilters(url.Values{"page": {"2"}, "limit": {"10"}, "merchant_id": {"9223372036854775807"}, "channel_id": {"7"}, "search_field": {"money"}, "q": {"1.03"}}, "admin", "transfers", "t.merchant_id", 0, []any{int64(0), int64(103)}); e != nil {
		t.Fatal(e)
	}
}
