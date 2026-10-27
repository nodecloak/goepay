package contract_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/enrollment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/server"
	"goepay/internal/simulation"
)

// The independent expected set starts from actual fixture journal IDs, not the
// new trade-reference predicate. Historical balances use each physical account.
func merchant167CheckLedger(t *testing.T, s *server.Server, p *pgxpool.Pool, owner int64, trade string, journals []int64) {
	t.Helper()
	ctx := context.Background()
	role := "merchant"
	if owner == 0 {
		role = "admin"
	}
	username := role
	if owner > 0 {
		username = fmt.Sprintf("merchant167_scope_%d", owner)
		if _, e := p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) SELECT $1,'merchant',$2,'Synthetic167',password_hash FROM console_users WHERE username='merchant' AND role='merchant' ON CONFLICT DO NOTHING`, username, owner); e != nil {
			t.Fatal(e)
		}
	}
	token, _, e := s.Sessions.Create(identity.User{ID: owner, Role: role, Username: username, AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	if owner == 0 {
		token, _, e = s.Sessions.Create(identity.User{ID: 1, Role: "admin", Username: "admin", AdminClass: "super"})
		if e != nil {
			t.Fatal(e)
		}
	}
	call := func(suffix string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/ledger?trade_no="+url.QueryEscape(trade)+suffix, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		return w
	}
	expected := map[string][2]string{}
	rows, e := p.Query(ctx, `SELECT e.id::text,(SELECT COALESCE(sum(x.amount),0)::text FROM ledger_entries x WHERE x.account_id=e.account_id AND x.id<e.id),(SELECT sum(x.amount)::text FROM ledger_entries x WHERE x.account_id=e.account_id AND x.id<=e.id) FROM ledger_entries e JOIN accounts a ON a.id=e.account_id WHERE e.journal_id=ANY($2::bigint[]) AND ($1::bigint=0 OR a.owner_id=$1)`, owner, journals)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var id, before, after string
		if e = rows.Scan(&id, &before, &after); e != nil {
			t.Fatal(e)
		}
		expected[id] = [2]string{before, after}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	var out struct {
		Data struct {
			Items []map[string]any
			Total int
		}
	}
	if e = json.Unmarshal(call("&limit=100").Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	if out.Data.Total != len(expected) || len(out.Data.Items) != len(expected) {
		t.Fatalf("order journal set differs owner=%d trade=%s got=%d/%d expected=%d", owner, trade, out.Data.Total, len(out.Data.Items), len(expected))
	}
	for _, v := range out.Data.Items {
		want, ok := expected[fmt.Sprint(v["id"])]
		if !ok || v["balance_before"] != want[0] || v["balance_after"] != want[1] {
			t.Fatal("unrelated account/journal or balance window changed", v, want)
		}
		if owner > 0 && v["merchant_id"] != strconv.FormatInt(owner, 10) {
			t.Fatal("foreign ledger owner", v)
		}
	}
	records, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(call("&format=csv").Body.String(), "\ufeff"))).ReadAll()
	if e != nil || len(records) != len(expected)+1 {
		t.Fatal("CSV scope mismatch", e, len(records), len(expected))
	}
	if len(expected) > 1 {
		if e = json.Unmarshal(call("&limit=1&page=2").Body.Bytes(), &out); e != nil || out.Data.Total != len(expected) || len(out.Data.Items) != 1 {
			t.Fatal("page/count predicate differs", e, out.Data)
		}
	}
}

func TestMerchant167ProfitAllocationReferenceShowsOnlyActualVisibleAccounts(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	orders := make([]payment.Order, 3)
	for i := range orders {
		mid := int64(1)
		if i == 2 {
			mid = 2
		}
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprintf("167-profit-%d", i), Amount: 10000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, "167-profit-paid:"+o.TradeNo); e != nil {
			t.Fatal(e)
		}
		orders[i] = o
	}
	var rule int64
	if e := p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,owner_id,body) VALUES('profitsharing-rules',1,'{"enabled":true,"rate_bps":"1000","recipient_id":"2"}') RETURNING id`).Scan(&rule); e != nil {
		t.Fatal(e)
	}
	request, e := s.Store.ExecuteProfitRule(ctx, rule, orders[0].TradeNo, "synthetic167")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Store.ReviewFundsRequest(ctx, request.ID, true, "synthetic167-admin"); e != nil {
		t.Fatal(e)
	}
	rows, e := p.Query(ctx, `SELECT id FROM journals WHERE event_key=ANY($1::text[])`, []string{"payment:" + orders[0].TradeNo, fmt.Sprintf("request:%d:reserve", request.ID), fmt.Sprintf("request:%d:review", request.ID)})
	if e != nil {
		t.Fatal(e)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) != 3 {
		t.Fatal("business fixture missing journals", ids)
	}
	for _, owner := range []int64{0, 1, 2} {
		merchant167CheckLedger(t, s, p, owner, orders[0].TradeNo, ids)
	}
	merchant167CheckLedger(t, s, p, 1, orders[2].TradeNo, nil)
	merchant167CheckLedger(t, s, p, 1, "no-such-synthetic-order", nil)
	merchant167CheckLedger(t, s, p, 1, "' OR true --", nil)
}

func TestMerchant167ReferralJournalFKsRespectPromoterVisibilityAndDailyRefundHistory(t *testing.T) {
	for _, mode := range []string{"immediate", "daily"} {
		t.Run(mode, func(t *testing.T) {
			s, p, _ := fixture(t)
			ctx := context.Background()
			issued, e := s.Store.RotateReferralToken(ctx, 1, 0, payment.TransferBatchActor{Role: "merchant", ID: 1, Name: "synthetic167"})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"registration_open","value":"true"}')`); e != nil {
				t.Fatal(e)
			}
			var code string
			registration, e := enrollment.New(p, s.Store, make([]byte, 32), enrollment.SenderFunc(func(_ context.Context, _, _, v string) error { code = v; return nil }))
			if e != nil {
				t.Fatal(e)
			}
			challenge, e := registration.Issue(ctx, enrollment.VerificationRequest{Purpose: "register", Email: "167-referral@example.test", Source: "192.0.2.167"})
			if e != nil {
				t.Fatal(e)
			}
			registered, e := registration.Register(ctx, enrollment.RegisterInput{ChallengeID: challenge.ID, Code: code, Email: "167-referral@example.test", Source: "192.0.2.167", Username: "synthetic167referral", Name: "Synthetic167", Password: "synthetic167-password", PromotionToken: issued.Token})
			if e != nil {
				t.Fatal(e)
			}
			var gid int64
			if e = p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"167 fee","enabled":true,"rate_bps":"1000"}') RETURNING id`).Scan(&gid); e != nil {
				t.Fatal(e)
			}
			if _, e = p.Exec(ctx, `UPDATE merchants SET profile=jsonb_build_object('group_id',$2::bigint::text) WHERE id=$1`, registered.MerchantID, gid); e != nil {
				t.Fatal(e)
			}
			if _, e = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}'),('settings','{"key":"refund_fee_payer","value":"platform"}')`); e != nil {
				t.Fatal(e)
			}
			enabled, rate := true, int64(5000)
			if _, e = s.Store.SetReferralPolicy(ctx, "global", 0, 0, payment.ReferralPolicyInput{Enabled: &enabled, RateBPS: &rate, Mode: mode, Basis: "fee", RefundPolicy: "any_successful_refund_voids_reward"}, payment.TransferBatchActor{Role: "admin", ID: 1, Class: "super", Name: "synthetic167"}); e != nil {
				t.Fatal(e)
			}
			env, e := simulation.New(bytes.Repeat([]byte{167}, 32), "http://127.0.0.1:8167")
			if e != nil {
				t.Fatal(e)
			}
			s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: env.Providers(), AllowLive: true, LocalSimulation: true, CallbackBaseURL: "http://127.0.0.1:8167"}
			env.Notify = func(ctx context.Context, id int64, trade string, raw []byte) error {
				return s.ProviderFlow.HandleNotification(ctx, id, trade, nil, raw)
			}
			o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: registered.MerchantID, OutTradeNo: "167-referral", Type: "wxpay", Amount: 10000})
			if e != nil {
				t.Fatal(e)
			}
			attempt, e := s.ProviderFlow.Submit(ctx, o.TradeNo, 2, "native", "127.0.0.1", "")
			if e != nil {
				t.Fatal(e)
			}
			u, _ := url.Parse(attempt.Material)
			w := httptest.NewRecorder()
			env.ServeHTTP(w, httptest.NewRequest("POST", u.RequestURI(), nil))
			if w.Code != 200 {
				t.Fatal("synthetic paid receipt", w.Code, w.Body.String())
			}
			r, e := s.Store.ReserveRefund(ctx, registered.MerchantID, o.OutTradeNo, "167-referral-return", 9800)
			if e != nil {
				t.Fatal(e)
			}
			if e = s.Store.ResolveRefund(ctx, r.ID, "failed"); e != nil {
				t.Fatal(e)
			}
			// Every journal was created by the original payment/refund engine in this
			// fresh fixture. No free-form event string is used to manufacture linkage.
			rows, e := p.Query(ctx, `SELECT id FROM journals ORDER BY id`)
			if e != nil {
				t.Fatal(e)
			}
			var ids []int64
			for rows.Next() {
				var id int64
				if e = rows.Scan(&id); e != nil {
					t.Fatal(e)
				}
				ids = append(ids, id)
			}
			rows.Close()
			var links int
			if e = p.QueryRow(ctx, `SELECT (SELECT count(*) FROM referral_rewards WHERE order_id=$1)+(SELECT count(*) FROM referral_daily_liabilities WHERE order_id=$1)`, o.ID).Scan(&links); e != nil || links != 1 {
				t.Fatal("missing original referral source", links, e)
			}
			for _, owner := range []int64{0, 1, registered.MerchantID} {
				merchant167CheckLedger(t, s, p, owner, o.TradeNo, ids)
			}
			merchant167CheckLedger(t, s, p, 2, o.TradeNo, nil)
			merchant167CheckLedger(t, s, p, 2, "no-such-synthetic-order", nil)
		})
	}
}
