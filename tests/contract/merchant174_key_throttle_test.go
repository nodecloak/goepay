package contract_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMerchant174ExhaustedKeyLoginBudgetRejectsBeforeCredentialLockWait(t *testing.T) {
	f := signatureFixture(t, 2)
	request := func(ctx context.Context) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/console/v1/merchant/key-login", strings.NewReader(`{"merchant_id":"2","api_key":"synthetic-wrong-key","code":""}`)).WithContext(ctx)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	for i := 0; i < 10; i++ {
		if w := request(context.Background()); w.Code != 401 {
			t.Fatal("wrong key login did not consume ordinary account budget", i, w.Code)
		}
	}
	tx, e := f.p.Begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(context.Background(), `SELECT id FROM merchants WHERE id=2 FOR UPDATE`); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w := request(ctx)
	if w.Code != 429 || w.Header().Get("Retry-After") != "900" {
		t.Fatal("exhausted key login reached a credential lock instead of the original throttle", w.Code)
	}
}
