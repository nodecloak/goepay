package payment

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Pro 分账记录 server-side list: the join supplies 订单金额/支付方式/分账规则,
// offset paging returns a total, filters narrow by field/keyword/status/date,
// the statistics aggregate matches the filter, and the CSV query is built.
func TestChannelSharingOrderFilteredListJoinsAggregatesAndFilters(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	o := f.paid(t, "sharing-list-order", false)
	if _, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "list-order-ext", channelSharingActor); e != nil {
		t.Fatal(e)
	}

	// Base list: one row, joined columns populated (order 100, rate 2000bps=20%).
	page, e := f.Store.ListChannelSharingOrdersFiltered(ctx, ChannelSharingOrderQuery{MerchantID: 751, Limit: 20}, channelSharingActor)
	if e != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatal(page, e)
	}
	row := page.Items[0]
	if row.ExternalNo != "list-order-ext" {
		t.Fatal("external_no", row.ExternalNo)
	}
	if row.OrderAmount != 100 {
		t.Fatal("订单金额 join missing", row.OrderAmount)
	}
	if row.Provider != "wxpay" {
		t.Fatal("支付方式 join missing", row.Provider)
	}
	if row.RuleID <= 0 {
		t.Fatal("分账规则 id join missing", row.RuleID)
	}
	if row.ShareAmount != 20 {
		t.Fatal("分账金额 sum wrong", row.ShareAmount)
	}

	// Merchant boundary is enforced.
	if _, e = f.Store.ListChannelSharingOrdersFiltered(ctx, ChannelSharingOrderQuery{MerchantID: 752, Limit: 20}, channelSharingActor); !errors.Is(e, ErrInvalid) {
		t.Fatal("list crossed merchant boundary", e)
	}

	// Field + keyword filter: exact external_no match, and a miss.
	hit, e := f.Store.ListChannelSharingOrdersFiltered(ctx, ChannelSharingOrderQuery{MerchantID: 751, Field: "external_no", Keyword: "list-order-ext", Limit: 20}, channelSharingActor)
	if e != nil || hit.Total != 1 {
		t.Fatal("external_no filter", hit, e)
	}
	miss, e := f.Store.ListChannelSharingOrdersFiltered(ctx, ChannelSharingOrderQuery{MerchantID: 751, Field: "external_no", Keyword: "nope", Limit: 20}, channelSharingActor)
	if e != nil || miss.Total != 0 || len(miss.Items) != 0 {
		t.Fatal("external_no miss should be empty", miss, e)
	}

	// share_amount filter: "0.20" yuan == 20 cents matches.
	share, e := f.Store.ListChannelSharingOrdersFiltered(ctx, ChannelSharingOrderQuery{MerchantID: 751, Field: "share_amount", Keyword: "0.20", Limit: 20}, channelSharingActor)
	if e != nil || share.Total != 1 {
		t.Fatal("share_amount filter", share, e)
	}

	// Status filter: pending state present; a terminal state is absent.
	pend, e := f.Store.ListChannelSharingOrdersFiltered(ctx, ChannelSharingOrderQuery{MerchantID: 751, States: []string{"pending"}, Limit: 20}, channelSharingActor)
	if e != nil || pend.Total != 1 {
		t.Fatal("pending status filter", pend, e)
	}
	none, e := f.Store.ListChannelSharingOrdersFiltered(ctx, ChannelSharingOrderQuery{MerchantID: 751, States: []string{"cancelled"}, Limit: 20}, channelSharingActor)
	if e != nil || none.Total != 0 {
		t.Fatal("cancelled status filter should be empty", none, e)
	}

	// Date range: today includes it; a past-only window excludes it.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	within, e := f.Store.ListChannelSharingOrdersFiltered(ctx, ChannelSharingOrderQuery{MerchantID: 751, From: today.Add(-24 * time.Hour), To: today.Add(48 * time.Hour), Limit: 20}, channelSharingActor)
	if e != nil || within.Total != 1 {
		t.Fatal("date range include", within, e)
	}
	past, e := f.Store.ListChannelSharingOrdersFiltered(ctx, ChannelSharingOrderQuery{MerchantID: 751, To: today.Add(-24 * time.Hour), Limit: 20}, channelSharingActor)
	if e != nil || past.Total != 0 {
		t.Fatal("date range exclude", past, e)
	}

	// Invalid inputs rejected, not coerced.
	for _, bad := range []ChannelSharingOrderQuery{
		{MerchantID: 751, Limit: 0},
		{MerchantID: 751, Limit: 101},
		{MerchantID: 751, Limit: 20, Field: "nope", Keyword: "x"},
		{MerchantID: 751, Limit: 20, States: []string{"bogus"}},
		{MerchantID: 751, Limit: 20, Offset: -1},
	} {
		if _, e = f.Store.ListChannelSharingOrdersFiltered(ctx, bad, channelSharingActor); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid query accepted", bad, e)
		}
	}

	// Statistics aggregate over the same filter.
	stats, e := f.Store.ChannelSharingOrderStatistics(ctx, ChannelSharingOrderQuery{MerchantID: 751}, channelSharingActor)
	if e != nil || stats.Count != 1 || stats.ShareAmount != 20 {
		t.Fatal("statistics", stats, e)
	}

	// CSV query builds with Pro headers.
	query, args, ok := ChannelSharingOrderQuery{MerchantID: 751}.ChannelSharingOrderCSVQuery(channelSharingActor)
	if !ok || !strings.Contains(query, "系统订单号") || !strings.Contains(query, "分账金额") || len(args) == 0 {
		t.Fatal("csv query", ok, query)
	}
}

func TestParseMinorAmount(t *testing.T) {
	cases := map[string]struct {
		cents int64
		ok    bool
	}{
		"0":      {0, true},
		"12":     {1200, true},
		"12.3":   {1230, true},
		"12.34":  {1234, true},
		"0.05":   {5, true},
		"":       {0, false},
		"12.345": {0, false},
		"-1":     {0, false},
		"abc":    {0, false},
		"1,000":  {0, false},
	}
	for in, want := range cases {
		got, ok := parseMinorAmount(in)
		if ok != want.ok || (ok && got != want.cents) {
			t.Fatalf("parseMinorAmount(%q)=%d,%v want %d,%v", in, got, ok, want.cents, want.ok)
		}
	}
}
