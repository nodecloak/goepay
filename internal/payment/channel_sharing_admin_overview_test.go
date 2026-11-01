package payment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

type adminSharingOrderReader interface {
	ListAdminChannelSharingOrders(context.Context, ChannelSharingOrderQuery, TransferBatchActor) (ChannelSharingOrderListPage, error)
	AdminChannelSharingOrderStatistics(context.Context, ChannelSharingOrderQuery, TransferBatchActor) (ChannelSharingOrderStats, error)
}

func addOverviewMerchant(t *testing.T, f *sharingExecutionFixture, mid int64, no string) ChannelSharingRuleView {
	t.Helper()
	ctx := context.Background()
	actor := TransferBatchActor{Role: "merchant", ID: mid, Name: no}
	if err := f.Store.CreateMerchant(ctx, Merchant{ID: mid, Name: no, APIKey: "synthetic-only"}); err != nil {
		t.Fatal(err)
	}
	r, err := f.Flow.CreateChannelSharingReceiver(ctx, mid, 7, no+"-receiver", adapter.SharingReceiver{Type: "MERCHANT_ID", Account: no + "-private-account", Name: "Synthetic Owner", RelationType: "SUPPLIER"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Flow.BindChannelSharingReceiver(ctx, mid, r.ID, no+"-bind", actor); err != nil {
		t.Fatal(err)
	}
	rule, err := f.Flow.SetChannelSharingRule(ctx, mid, 7, ChannelSharingRuleInput{Enabled: true, Methods: []string{"native"}, Items: []ChannelSharingRuleItem{{ReceiverID: r.ID, Mode: "rate", RateBPS: 2000}}}, 0, actor)
	if err != nil {
		t.Fatal(err)
	}
	o, err := f.Store.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: no, Type: "wxpay", Amount: 100, Name: "Synthetic overview item"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Flow.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); err != nil {
		t.Fatal(err)
	}
	if err = f.Store.markPaid(ctx, o.TradeNo, "paid-"+no, true, PaidReceipt{RealMoney: 100, ChannelID: 7, ProviderTradeNo: "WX_" + o.TradeNo}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Flow.SubmitChannelSharing(ctx, mid, o.TradeNo, "same-sharing-number", actor); err != nil {
		t.Fatal(err)
	}
	return rule
}

func TestAdminSharingOrderOverviewKeepsOriginalMerchantAndScopedMethods(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	first := f.paid(t, "overview-first", false)
	if _, err := f.Flow.SubmitChannelSharing(ctx, 751, first.TradeNo, "same-sharing-number", channelSharingActor); err != nil {
		t.Fatal(err)
	}
	addOverviewMerchant(t, f, 752, "overview-second")
	reader, ok := any(f.Store).(adminSharingOrderReader)
	if !ok {
		t.Fatal("administrator cross-merchant sharing overview is missing")
	}
	admin := TransferBatchActor{Role: "admin", Class: "operator", ID: 1, Name: "synthetic-operator"}
	beforeSends, beforeQueries := f.Sends.Load(), f.Queries.Load()
	all, err := reader.ListAdminChannelSharingOrders(ctx, ChannelSharingOrderQuery{Limit: 1}, admin)
	if err != nil || all.Total != 2 || len(all.Items) != 1 {
		t.Fatalf("global page: %+v %v", all, err)
	}
	next, err := reader.ListAdminChannelSharingOrders(ctx, ChannelSharingOrderQuery{Limit: 1, Offset: 1}, admin)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID == all.Items[0].ID {
		t.Fatalf("next page: %+v %v", next, err)
	}
	encoded, _ := json.Marshal(all.Items[0])
	var body map[string]any
	json.Unmarshal(encoded, &body)
	if body["merchant_id"] != "752" || body["subchannel_id"] != "0" {
		t.Fatalf("original owner and no invented child: %s", encoded)
	}
	scoped, err := reader.ListAdminChannelSharingOrders(ctx, ChannelSharingOrderQuery{MerchantID: 751, Limit: 20}, admin)
	if err != nil || scoped.Total != 1 || scoped.Items[0].ID != next.Items[0].ID {
		t.Fatalf("merchant filter: %+v %v", scoped, err)
	}
	stats, err := reader.AdminChannelSharingOrderStatistics(ctx, ChannelSharingOrderQuery{}, admin)
	if err != nil || stats.Count != 2 || stats.ShareAmount != 40 {
		t.Fatalf("global aggregate: %+v %v", stats, err)
	}
	for _, actor := range []TransferBatchActor{channelSharingActor, {Role: "admin", Class: "invalid", ID: 1, Name: "invalid"}, {Role: "admin", Class: "super", ID: 0, Name: "invalid"}} {
		if _, err = reader.ListAdminChannelSharingOrders(ctx, ChannelSharingOrderQuery{Limit: 20}, actor); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unauthorized aggregate: %v", err)
		}
	}
	if _, err = f.Store.ListChannelSharingOrdersFiltered(ctx, ChannelSharingOrderQuery{Limit: 20}, admin); !errors.Is(err, ErrInvalid) {
		t.Fatalf("existing scoped API was widened: %v", err)
	}
	if _, err = f.Store.ChannelSharingOrderStatistics(ctx, ChannelSharingOrderQuery{}, admin); !errors.Is(err, ErrInvalid) {
		t.Fatalf("existing scoped aggregate was widened: %v", err)
	}
	raw, _ := json.Marshal(all)
	if strings.Contains(string(raw), "private-account") {
		t.Fatal("private receiver material in order overview")
	}
	if beforeSends != f.Sends.Load() || beforeQueries != f.Queries.Load() {
		t.Fatal("overview performed upstream IO")
	}
}

func TestAdminSharingRuleOverviewUsesCurrentVersionsAndMaskedReceivers(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	f.paid(t, "rule-overview-first", false)
	second := addOverviewMerchant(t, f, 752, "rule-overview-second")
	first, err := f.Store.ChannelSharingRule(ctx, 751, 7, channelSharingActor)
	if err != nil {
		t.Fatal(err)
	}
	disabled := first.ChannelSharingRuleInput
	disabled.Enabled = false
	if _, err = f.Flow.SetChannelSharingRule(ctx, 751, 7, disabled, first.ID, channelSharingActor); err != nil {
		t.Fatal(err)
	}
	reader, ok := any(f.Store).(interface {
		ListAdminChannelSharingRules(context.Context, ChannelSharingRuleOverviewQuery, TransferBatchActor) (ChannelSharingRuleOverviewPage, error)
	})
	if !ok {
		t.Fatal("administrator current-rule overview is missing")
	}
	actor := TransferBatchActor{Role: "admin", Class: "operator", ID: 1, Name: "synthetic-operator"}
	page, err := reader.ListAdminChannelSharingRules(ctx, ChannelSharingRuleOverviewQuery{Limit: 20}, actor)
	if err != nil || page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("current versions: %+v %v", page, err)
	}
	if page.Items[0].Rule.MerchantID != 751 || page.Items[0].Rule.Revision != 2 || page.Items[0].Rule.Enabled {
		t.Fatalf("disabled current rule missing: %+v", page.Items[0])
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), "private-receiver-1234") || strings.Contains(string(raw), "private-account") || strings.Contains(string(raw), "Synthetic Owner") || strings.Contains(string(raw), "cipher") {
		t.Fatalf("private receiver leaked: %s", raw)
	}
	if len(page.Items[0].Receivers) != 1 || page.Items[0].Receivers[0].AccountMasked == "" {
		t.Fatal("masked recipients missing")
	}
	enabled := true
	for _, q := range []ChannelSharingRuleOverviewQuery{
		{MerchantID: 752, Limit: 20}, {RuleID: second.ID, Limit: 20}, {ReceiverID: second.Items[0].ReceiverID, Limit: 20}, {Enabled: &enabled, Limit: 20},
	} {
		one, e := reader.ListAdminChannelSharingRules(ctx, q, actor)
		if e != nil || one.Total != 1 || one.Items[0].Rule.ID != second.ID {
			t.Fatalf("rule filter %+v: %+v %v", q, one, e)
		}
	}
	for _, q := range []ChannelSharingRuleOverviewQuery{{Limit: 0}, {Limit: 101}, {Limit: 20, Offset: -1}, {Limit: 20, MerchantID: -1}, {Limit: 20, ChannelID: -1}, {Limit: 20, ReceiverID: -1}, {Limit: 20, RuleID: -1}} {
		if _, e := reader.ListAdminChannelSharingRules(ctx, q, actor); !errors.Is(e, ErrInvalid) {
			t.Fatalf("invalid rule query accepted: %+v %v", q, e)
		}
	}
	if _, e := reader.ListAdminChannelSharingRules(ctx, ChannelSharingRuleOverviewQuery{Limit: 20}, channelSharingActor); !errors.Is(e, ErrInvalid) {
		t.Fatalf("merchant accessed overview: %v", e)
	}
}

func TestAdminSharingOverviewCSVFiltersAndBeijingTime(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	first := f.paid(t, "overview-csv", false)
	if _, err := f.Flow.SubmitChannelSharing(ctx, 751, first.TradeNo, "csv-sharing", channelSharingActor); err != nil {
		t.Fatal(err)
	}
	actor := TransferBatchActor{Role: "admin", Class: "operator", ID: 1, Name: "synthetic-operator"}
	q := ChannelSharingOrderQuery{MerchantID: 751, Field: "external_no", Keyword: "csv-sharing"}
	builder, ok := any(q).(interface {
		AdminChannelSharingOrderCSVQuery(TransferBatchActor) (string, []any, bool)
	})
	if !ok {
		t.Fatal("administrator scoped overview export is missing")
	}
	query, args, valid := builder.AdminChannelSharingOrderCSVQuery(actor)
	if !valid || !strings.Contains(query, "用户ID") || !strings.Contains(query, "子通道ID") {
		t.Fatalf("overview CSV columns: %s", query)
	}
	rows, err := f.Store.pool.Query(ctx, query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("filtered CSV row absent")
	}
	values, err := rows.Values()
	if err != nil {
		t.Fatal(err)
	}
	fields := rows.FieldDescriptions()
	found := map[string]any{}
	for i, v := range values {
		found[fields[i].Name] = v
	}
	list, err := f.Store.ListChannelSharingOrdersFiltered(ctx, ChannelSharingOrderQuery{MerchantID: 751, Limit: 20}, actor)
	if err != nil {
		t.Fatal(err)
	}
	want := list.Items[0].CreatedAt.Add(8 * 60 * 60 * 1e9).UTC().Format("2006-01-02 15:04:05")
	if found["时间"] != want || found["用户ID"] != int64(751) {
		t.Fatalf("CSV values: %+v want time=%s", found, want)
	}
	if rows.Next() {
		t.Fatal("CSV ignored applied filter")
	}
	if _, _, ok = builder.AdminChannelSharingOrderCSVQuery(channelSharingActor); ok {
		t.Fatal("merchant built administrator CSV")
	}
}
