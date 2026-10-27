package server

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSubchannel168BMerchantReadAndRollDoNotInvertDisableLockOrder(t *testing.T) {
	for _, mode := range []string{"projection", "policy"} {
		t.Run(mode, func(t *testing.T) {
			f, _ := subchannel168BLocal(t)
			child168BCreate(t, f, "lock-owner", true)
			token, csrf := child168BMerchantSession(t, f, 1)
			policy := f.childHTTP("GET", "/policy?merchant_id=1", "")
			var state struct{ Data map[string]any }
			json.Unmarshal(policy.Body.Bytes(), &state)
			body, _ := json.Marshal(map[string]any{"roll_type": 1, "expected_fingerprint": state.Data["fingerprint"]})
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			m, e := f.s.Store.Merchant(ctx, 1)
			if e != nil {
				t.Fatal(e)
			}
			m.Enabled = false
			locked := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			edit := make(chan error, 1)
			var editPID uint32
			go func() {
				edit <- f.s.Store.ReplaceMerchantAtomic(ctx, m, []byte(`{}`), func(ctx context.Context, tx pgx.Tx) error {
					editPID = tx.Conn().PgConn().PID()
					close(locked)
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
					_, e := tx.Exec(ctx, `UPDATE console_users SET auth_version=auth_version+1 WHERE role='merchant' AND subject_id=$1`, int64(1))
					return e
				})
			}()
			select {
			case <-locked:
			case <-ctx.Done():
				t.Fatal("merchant editor did not acquire lock")
			}
			request := make(chan int, 1)
			go func() {
				method, path, payload := "GET", "/console/v1/merchant/payment-subchannels", ""
				if mode == "policy" {
					method = "PATCH"
					path += "/policy"
					payload = string(body)
				}
				r := httptest.NewRequest(method, path, strings.NewReader(payload)).WithContext(ctx)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("X-CSRF-Token", csrf)
				r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
				w := httptest.NewRecorder()
				f.s.Handler().ServeHTTP(w, r)
				request <- w.Code
			}()
			for {
				var waiting bool
				e = f.p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::int=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock')`, int(editPID)).Scan(&waiting)
				if e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				select {
				case status := <-request:
					t.Fatal("request did not reach controlled merchant lock", status)
				case <-time.After(10 * time.Millisecond):
				case <-ctx.Done():
					t.Fatal("request never reached merchant lock")
				}
			}
			unblock()
			var editErr error
			var status int
			select {
			case editErr = <-edit:
			case <-ctx.Done():
				t.Fatal("merchant editor timed out")
			}
			select {
			case status = <-request:
			case <-ctx.Done():
				t.Fatal("child request timed out")
			}
			if editErr != nil || status != 403 {
				t.Fatalf("merchant disable and child %s inverted locks: edit=%v HTTP=%d (want committed revoke+403)", mode, editErr, status)
			}
		})
	}
}
