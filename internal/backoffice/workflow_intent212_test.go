package backoffice

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/payment"
	"net/http/httptest"
	"strings"
	"testing"
)

// These requests are rejected before any database access. They exercise the
// real public dispatcher, so an absent lookup or a broadened tenant predicate
// cannot satisfy the status contract.
func TestWorkflowIntent212LookupValidatesExactOrigin(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		principal   Principal
		want        int
	}{
		{"merchant cannot look up another payer", "external_no=original&merchant_id=1001", Principal{Role: "merchant", SubjectID: 1000}, 403},
		{"admin requires an explicit payer", "external_no=original", Principal{Role: "admin", SubjectID: 1, AdminClass: "super"}, 400},
		{"reject absent request number", "merchant_id=1000", Principal{Role: "merchant", SubjectID: 1000}, 400},
		{"reject duplicate request number", "external_no=a&external_no=b", Principal{Role: "merchant", SubjectID: 1000}, 400},
		{"reject unrecognized filter", "external_no=a&all=true", Principal{Role: "merchant", SubjectID: 1000}, 400},
		{"reject control characters", "external_no=a%0Ab", Principal{Role: "merchant", SubjectID: 1000}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{}
			r := httptest.NewRequest("GET", "/console/v1/"+tc.principal.Role+"/workflows/settlements/lookup?"+tc.query, nil)
			w := httptest.NewRecorder()
			s.ServeWorkflows(w, r, tc.principal, []string{"settlements", "lookup"})
			if w.Code != tc.want {
				t.Fatalf("lookup status=%d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestWorkflowIntent212LookupReadsOnlyOriginalPayerAndKind(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	order, e := s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 101, OutTradeNo: "fund-lookup212", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.store.MarkPaid(ctx, order.TradeNo, "local-paid212"); e != nil {
		t.Fatal(e)
	}
	original, e := s.store.RequestFunds(ctx, "internal-transfers", 101, 102, "only-payer-101", 100, "merchant:101")
	if e != nil {
		t.Fatal(e)
	}
	before, e := s.store.Balance(ctx, 101)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name, kind, query string
		p                 Principal
		want              int
	}{
		{"originating merchant", "internal-transfers", "external_no=only-payer-101", Principal{Role: "merchant", SubjectID: 101}, 200},
		{"recipient is not originating merchant", "internal-transfers", "external_no=only-payer-101", Principal{Role: "merchant", SubjectID: 102}, 404},
		{"kind cannot be broadened", "settlements", "external_no=only-payer-101", Principal{Role: "merchant", SubjectID: 101}, 404},
		{"explicit administrator payer", "internal-transfers", "external_no=only-payer-101&merchant_id=101", Principal{Role: "admin", SubjectID: 1, AdminClass: "super"}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/lookup?"+tc.query, nil)
			s.ServeWorkflows(w, r, tc.p, []string{tc.kind, "lookup"})
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
			if tc.want == 200 {
				var out struct {
					Data map[string]any `json:"data"`
				}
				if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
					t.Fatal(e)
				}
				if out.Data["merchant_id"] != "101" || out.Data["recipient_id"] != "102" || out.Data["amount_cents"] != "100" || out.Data["external_no"] != "only-payer-101" || out.Data["kind"] != "internal-transfers" {
					t.Fatalf("wrong original intent: %+v", out.Data)
				}
				if out.Data["id"] == nil || original.ID <= 0 {
					t.Fatal("original record absent")
				}
			}
		})
	}
	after, e := s.store.Balance(ctx, 101)
	if e != nil || before != after {
		t.Fatalf("lookup changed balances: before=%+v after=%+v error=%v", before, after, e)
	}
}

func TestWorkflowIntent212AbsentRefusalProofDoesNotMaskAnExistingRequest(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	o, e := s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 101, OutTradeNo: "refusal212", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.store.MarkPaid(ctx, o.TradeNo, "refusal212-paid"); e != nil {
		t.Fatal(e)
	}
	configured, e := s.store.SetSettlementRecipient(ctx, 101, payment.SettlementRecipientInput{Type: "alipay", Account: "local@example.invalid", Name: "Local Fixture"}, nil, payment.SettlementRecipientActor{Role: "merchant", ID: 101, Name: "local"})
	if e != nil {
		t.Fatal(e)
	}
	request := func(kind, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/workflows/"+kind, strings.NewReader(body))
		s.ServeWorkflows(w, r, Principal{Role: "merchant", SubjectID: 101}, []string{kind})
		return w
	}
	check := func(w *httptest.ResponseRecorder, marker bool) {
		t.Helper()
		if w.Code != 409 {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var out struct {
			Data map[string]any `json:"data"`
		}
		if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		if (out.Data["request_not_recorded"] == true) != marker {
			t.Fatalf("wrong not-recorded proof: %s", w.Body.String())
		}
	}
	check(request("internal-transfers", `{"amount":"20.00","external_no":"correctable212","recipient_id":"102"}`), true)
	created := request("internal-transfers", `{"amount":"1.00","external_no":"correctable212","recipient_id":"102"}`)
	if created.Code != 201 {
		t.Fatalf("correction status=%d body=%s", created.Code, created.Body.String())
	}
	check(request("internal-transfers", `{"amount":"2.00","external_no":"correctable212","recipient_id":"102"}`), false)
	quoted := fmt.Sprintf(`{"amount":"1.00","external_no":"stalequote212","expected_fee_cents":"1","expected_net_cents":"99","recipient_config_id":"%d"}`, configured.ConfigID)
	check(request("settlements", quoted), true)
	var n int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM ledger_requests WHERE external_no='stalequote212'").Scan(&n); e != nil || n != 0 {
		t.Fatalf("refused quote recorded a request: %d %v", n, e)
	}
}
