package contract_test

import (
	"context"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"testing"
)

type batchProjectionProvider struct{ transferHTTPProvider }

func (*batchProjectionProvider) ID() string            { return "alipay" }
func (*batchProjectionProvider) ApplicationID() string { return "projection-test-app" }

func TestTransferBatchListProjectsUnsentAndCancelled(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true}' WHERE id=1;INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "batch-projection-funds", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "batch-projection-funds"); e != nil {
		t.Fatal(e)
	}
	g := &payment.ProviderFlow{Store: s.Store, AllowLive: true, Providers: map[int64]adapter.Provider{7: &batchProjectionProvider{}}}
	s.ProviderFlow = g
	actor := payment.TransferBatchActor{Role: "merchant", ID: 1, Name: "merchant"}
	batch, e := g.PrepareTransferBatch(ctx, 1, "projection", []payment.TransferBatchInput{{ChannelID: 7, Request: adapter.TransferRequest{Type: "alipay", Account: "synthetic-account", Amount: 100}}}, actor)
	if e != nil {
		t.Fatal(e)
	}
	token, _, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	check := func(status string, want int) {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/merchant/transfers?status="+status, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out struct {
			Data struct {
				Items []map[string]any `json:"items"`
				Total int              `json:"total"`
			} `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil || w.Code != 200 || out.Data.Total != want {
			t.Fatalf("batch status filter %s: %d %s", status, w.Code, w.Body.String())
		}
		if want == 1 && out.Data.Items[0]["status"] != status {
			t.Fatalf("send barrier missing: %v", out.Data.Items[0]["status"])
		}
		if want == 1 {
			stored, e := s.Store.Transfer(ctx, 1, batch.Items[0].Transfer.TransferNo)
			if e != nil || stored.Status != status {
				t.Fatalf("single detail ignored send barrier: %s %v", stored.Status, e)
			}
		}
	}
	check("unsent", 1)
	check("pending", 0)
	if _, e = g.CancelUnsentTransferBatch(ctx, 1, batch.ID, actor); e != nil {
		t.Fatal(e)
	}
	check("cancelled", 1)
	check("failed", 0)
	check("unsent", 0)
}
