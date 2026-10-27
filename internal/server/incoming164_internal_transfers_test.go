package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

const incoming164A int64 = 9007199254740993
const incoming164B int64 = 9007199254740995
const incoming164C int64 = 9007199254740997

func incoming164Fixture(t *testing.T) (*Server, func(int64, string, string, string) *httptest.ResponseRecorder) {
	t.Helper()
	s, p := groupRoutingFixture152(t)
	s.ProviderFlow = nil
	s.Sessions = identity.NewSessions(false)
	ctx := context.Background()
	tokens := map[int64][2]string{}
	for _, id := range []int64{incoming164A, incoming164B, incoming164C} {
		if e := s.Store.CreateMerchant(ctx, payment.Merchant{ID: id, Name: "synthetic incoming164", APIKey: "synthetic-incoming164-only"}); e != nil {
			t.Fatal(e)
		}
		username := fmt.Sprintf("incoming164-%d", id)
		if _, e := p.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES($1,'merchant',$2,'synthetic','not-a-real-password-hash')", username, id); e != nil {
			t.Fatal(e)
		}
		token, csrf, e := s.Sessions.Create(identity.User{Role: "merchant", ID: id, Username: username})
		if e != nil {
			t.Fatal(e)
		}
		tokens[id] = [2]string{token, csrf}
	}
	for _, v := range []struct {
		id, from, to int64
		status       string
	}{
		{9007199254741001, incoming164A, incoming164B, "approved"},
		{9007199254741003, incoming164A, incoming164B, "rejected"},
		{9007199254741005, incoming164A, incoming164B, "pending"},
		{9007199254741007, incoming164B, incoming164A, "pending"},
	} {
		if _, e := p.Exec(ctx, "INSERT INTO ledger_requests(id,kind,merchant_id,recipient_id,external_no,amount,status,created_at,updated_at) VALUES($1,'internal-transfers',$2,$3,$4,123,$5,'2026-10-25T01:00:00Z','2026-10-26T22:00:00Z')", v.id, v.from, v.to, fmt.Sprintf("private-sender-reference-%d", v.id), v.status); e != nil {
			t.Fatal(e)
		}
		if v.status != "pending" {
			if _, e := p.Exec(ctx, "INSERT INTO ledger_request_events(request_id,actor,status,created_at) VALUES($1,'admin:71:private-admin-name',$2,'2026-10-25T02:00:00Z')", v.id, v.status); e != nil {
				t.Fatal(e)
			}
		}
	}
	// Other workflow participants must not gain recipient visibility.
	if _, e := p.Exec(ctx, "INSERT INTO ledger_requests(kind,merchant_id,recipient_id,external_no,amount,status) VALUES('profitsharing',$1,$2,'other-workflow',123,'pending')", incoming164A, incoming164B); e != nil {
		t.Fatal(e)
	}
	return s, func(id int64, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/console/v1/merchant/"+path, strings.NewReader(body))
		if id != 0 {
			r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: tokens[id][0]})
			r.Header.Set("X-CSRF-Token", tokens[id][1])
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
}

func incoming164Page(t *testing.T, w *httptest.ResponseRecorder) (int, []map[string]any) {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("HTTP status=%d body=%s", w.Code, w.Body.String())
	}
	var v struct {
		Data struct {
			Total int              `json:"total"`
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	return v.Data.Total, v.Data.Items
}

func TestIncoming164RecipientVisibilityCountPaginationAndSafeDTO(t *testing.T) {
	_, request := incoming164Fixture(t)
	total, items := incoming164Page(t, request(incoming164B, "GET", "workflows/internal-transfers?limit=2&page=1", ""))
	if total != 4 || len(items) != 2 {
		t.Fatalf("recipient must see three incoming and one outgoing: total=%d items=%d", total, len(items))
	}
	if items[0]["id"] != "9007199254741007" || items[0]["direction"] != "out" || items[0]["peer_merchant_id"] != "9007199254740993" {
		t.Fatalf("outgoing direction or big IDs wrong: %#v", items[0])
	}
	second, tail := incoming164Page(t, request(incoming164B, "GET", "workflows/internal-transfers?limit=2&page=2", ""))
	if second != 4 || len(tail) != 2 || tail[1]["id"] != "9007199254741001" {
		t.Fatal("count/order/page disagree", second, tail)
	}
	rawCompletion, validCompletion := tail[1]["completed_at"].(string)
	completion, completionErr := time.Parse(time.RFC3339, rawCompletion)
	// The wire permits RFC3339 offsets. Compare the exact terminal instant,
	// independently of host time.Local, rather than requiring a +08 spelling.
	expectedCompletion := time.Date(2026, time.October, 25, 2, 0, 0, 0, time.UTC)
	if !validCompletion || completionErr != nil || !completion.Equal(expectedCompletion) || tail[1]["direction"] != "in" || tail[1]["amount"] != "1.23" || tail[1]["remark"] != nil {
		t.Fatal("original completion or safe DTO missing", tail[1])
	}
	for _, row := range append(items, tail...) {
		for _, key := range []string{"external_no", "actor", "merchant_profile", "merchant_id", "recipient_id", "username"} {
			if _, ok := row[key]; ok {
				t.Fatal("private field returned", key)
			}
		}
		if row["status"] == "pending" && row["completed_at"] != nil {
			t.Fatal("pending invented completion", row)
		}
	}
	none, rows := incoming164Page(t, request(incoming164C, "GET", "workflows/internal-transfers", ""))
	if none != 0 || len(rows) != 0 {
		t.Fatal("third merchant read another transaction")
	}
	_, sender := incoming164Page(t, request(incoming164A, "GET", "workflows/internal-transfers", ""))
	if len(sender) != 4 {
		t.Fatal("sender lost own transactions")
	}
	for _, needle := range []string{"private-admin-name", "private-sender-reference", "password", "api_key"} {
		if strings.Contains(fmt.Sprint(items, tail), needle) {
			t.Fatal("sensitive data leaked")
		}
	}
}

func TestIncoming164DirectionStatusFiltersAndReadDoesNotGrantWrite(t *testing.T) {
	s, request := incoming164Fixture(t)
	for _, v := range []struct {
		query string
		total int
	}{{"direction=in", 3}, {"direction=out", 1}, {"direction=in&status=approved", 1}, {"direction=in&status=rejected", 1}, {"direction=in&status=pending", 1}} {
		total, items := incoming164Page(t, request(incoming164B, "GET", "workflows/internal-transfers?"+v.query, ""))
		if total != v.total || len(items) != v.total {
			t.Fatal(v.query, total, len(items))
		}
	}
	for _, q := range []string{"direction=invalid", "status=succeeded", "direction=in&direction=out", "status=approved&status=rejected", "merchant_id=9007199254740993", "uid=9007199254740993"} {
		w := request(incoming164B, "GET", "workflows/internal-transfers?"+q, "")
		if w.Code < 400 {
			t.Fatal("invalid or cross-tenant filter accepted", q, w.Code)
		}
	}
	for _, path := range []string{"workflows/profitsharing?direction=in", "workflows/settlements?direction=in"} {
		if w := request(incoming164B, "GET", path, ""); w.Code < 400 {
			t.Fatal("internal-only filter escaped workflow", path)
		}
	}
	total, rows := incoming164Page(t, request(incoming164B, "GET", "workflows/profitsharing", ""))
	if total != 0 || len(rows) != 0 {
		t.Fatal("recipient access widened another workflow")
	}
	var before string
	snapshot := func() string {
		t.Helper()
		var v string
		e := s.Pool.QueryRow(context.Background(), "SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM ledger_requests r),(SELECT count(*) FROM ledger_request_events),(SELECT count(*) FROM journals),(SELECT count(*) FROM ledger_entries),(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM accounts a))::text").Scan(&v)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	before = snapshot()
	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		for _, suffix := range []string{"/approve", "/reject", "/cancel", ""} {
			w := request(incoming164B, method, "workflows/internal-transfers/9007199254741005"+suffix, "{}")
			if w.Code < 400 {
				t.Fatal("recipient obtained write authority", method, suffix, w.Code)
			}
		}
	}
	w := request(incoming164B, "POST", "workflows/internal-transfers", `{"merchant_id":"9007199254740993","recipient_id":"9007199254740995","amount":"1.23","external_no":"forged-origin"}`)
	if w.Code != 403 {
		t.Fatal("recipient could impersonate sender", w.Code)
	}
	if snapshot() != before {
		t.Fatal("denied writes mutated original state")
	}
	if request(0, "GET", "workflows/internal-transfers", "").Code != 401 {
		t.Fatal("anonymous access")
	}
}

func TestIncoming164HistoricalCompletionRequiresMatchingTerminalEvent(t *testing.T) {
	s, request := incoming164Fixture(t)
	ctx := context.Background()
	// A terminal historical record with a later updated_at is not evidence of
	// its original completion. An unrelated pending event must not fill the gap.
	if _, e := s.Pool.Exec(ctx, `INSERT INTO ledger_requests(id,kind,merchant_id,recipient_id,external_no,amount,status,created_at,updated_at)
 VALUES(9007199254741009,'internal-transfers',$1,$2,'historical-no-completion',123,'approved','2026-10-24T01:00:00Z','2026-10-26T22:00:00Z')`, incoming164A, incoming164B); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Pool.Exec(ctx, `INSERT INTO ledger_request_events(request_id,actor,status,created_at) VALUES(9007199254741009,'private-actor','pending','2026-10-24T01:00:00Z')`); e != nil {
		t.Fatal(e)
	}
	total, rows := incoming164Page(t, request(incoming164B, "GET", "workflows/internal-transfers?status=approved", ""))
	if total != 2 || len(rows) != 2 || rows[0]["id"] != "9007199254741009" || rows[0]["completed_at"] != nil || rows[0]["remark"] != nil {
		t.Fatal("invented historical completion or remark", total, rows)
	}
	total, rows = incoming164Page(t, request(incoming164B, "GET", "workflows/internal-transfers?limit=2&page=4", ""))
	if total != 5 || len(rows) != 0 {
		t.Fatal("empty page disagrees with scoped total", total, rows)
	}
}
