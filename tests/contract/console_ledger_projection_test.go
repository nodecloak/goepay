package contract_test

import (
	"context"
	"encoding/csv"
	"fmt"
	"strings"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestConsoleLedgerBalancesPrecedeFiltersAndPagination(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	for _, row := range []struct {
		mid    int64
		no     string
		amount int64
	}{{1, "ledger-first", 1000}, {2, "other-tenant", 777}} {
		o, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: row.mid, OutTradeNo: row.no, Name: "Synthetic", Amount: row.amount})
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Store.MarkPaid(ctx, o.TradeNo, row.no); err != nil {
			t.Fatal(err)
		}
	}
	refund, err := s.Store.ReserveRefund(ctx, 1, "ledger-first", "ledger-projection-refund", 100)
	if err != nil {
		t.Fatal(err)
	}
	later, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "ledger-later", Name: "Synthetic", Amount: 50})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.MarkPaid(ctx, later.TradeNo, "ledger-later"); err != nil {
		t.Fatal(err)
	}
	token, csrf, err := s.Sessions.Create(identity.User{Username: "merchant", ID: 1, Role: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("refund:%d:reserve", refund.ID)
	assertRows := func(rows []any) {
		t.Helper()
		for _, raw := range rows {
			r := raw.(map[string]any)
			if r["merchant_id"] != "1" || r["account_id"] == nil {
				t.Error("missing account/tenant identity", r)
			}
			switch {
			case r["kind"] == "available" && r["event_key"] == key:
				if r["balance_before"] != "1000" || r["balance_after"] != "900" {
					t.Error("filtered refund lost original balances", r)
				}
			case r["kind"] == "reserved" && r["event_key"] == key:
				if r["balance_before"] != "0" || r["balance_after"] != "100" {
					t.Error("reserve mixed with available", r)
				}
			case r["event_key"] == "payment:"+later.TradeNo:
				if r["balance_before"] != "900" || r["balance_after"] != "950" {
					t.Error("later available balance wrong", r)
				}
			}
		}
	}
	for _, path := range []string{"/console/v1/merchant/ledger", "/console/v1/merchant/ledger?q=" + key, "/console/v1/merchant/ledger?q=" + key + "&limit=1&page=2"} {
		w := lookupHTTP(t, s.Handler(), "GET", path, "", token, "merchant", csrf)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if path == "/console/v1/merchant/ledger" && lookupData(t, w)["total"] != float64(4) {
			t.Fatal("ledger row count/tenant boundary changed", w.Body.String())
		}
		assertRows(lookupData(t, w)["items"].([]any))
	}
	w := lookupHTTP(t, s.Handler(), "GET", "/console/v1/merchant/ledger?q="+key+"&format=csv", "", token, "merchant", csrf)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
	if err != nil || len(records) != 3 {
		t.Fatal("filtered CSV", records, err)
	}
	var rows []any
	for _, record := range records[1:] {
		row := map[string]any{}
		for i, field := range records[0] {
			row[field] = record[i]
		}
		rows = append(rows, row)
	}
	assertRows(rows)
	if balance, err := s.Store.Balance(ctx, 1); err != nil || balance.Available != 950 || balance.Reserved != 100 {
		t.Fatal("read/export changed money", balance, err)
	}
}
