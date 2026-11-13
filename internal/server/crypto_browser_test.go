package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/cryptopay"
)

func TestCryptoActualBrowserConsoleCheckoutAndPolling(t *testing.T) {
	if os.Getenv("GOEPAY_CRYPTO_BROWSER") != "1" {
		t.Skip("GOEPAY_CRYPTO_BROWSER=1 requires local Playwright and synthetic PostgreSQL")
	}
	f := email197Local(t)
	f.s.Sandbox = true
	f.s.Crypto = &cryptopay.Service{Pool: f.p, RequireChannels: true, TokenKey: make([]byte, 32), Networks: map[string]cryptopay.NetworkConfig{"tron": {Enabled: true, FeeBPS: 50}, "ethereum": {Enabled: true, FeeBPS: 80}}}
	if _, err := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('pay-types','{"type":"usdt","enabled":true}'); UPDATE backoffice_records SET body=jsonb_set(body,'{info}','{"usdt":{"channel":"-5","type":"channel"}}') WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	ctxProof := cryptopay.WithAuthority(t.Context(), func(context.Context, pgx.Tx) error { return nil })
	for _, c := range []struct {
		network, key string
		fee          int64
	}{{"tron", "a", 50}, {"ethereum", "b", 80}} {
		if _, err := f.s.Crypto.SaveChannel(ctxProof, cryptopay.SaveChannelInput{ActorID: 168, RequestID: strings.Repeat(c.key, 32), Name: "USDT " + c.network, Network: c.network, Enabled: true, FeeBPS: c.fee, MinAmount: "0", MaxAmount: "0", DayLimit: "0", ReservedIDs: []int64{7, 8}}); err != nil {
			t.Fatal(err)
		}
	}
	for network, addresses := range map[string][]string{"tron": {"TJRabPrwbZy45sbavfcjinPJC18kjpRTv8", "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE"}, "ethereum": {"0x1111111111111111111111111111111111111111", "0x2222222222222222222222222222222222222222"}} {
		for _, address := range addresses {
			if e := f.s.Crypto.AddAddress(t.Context(), network, address, "deposit"); e != nil {
				t.Fatal(e)
			}
		}
	}
	root, e := filepath.Abs(filepath.Join("..", ".."))
	if e != nil {
		t.Fatal(e)
	}
	assets := http.FileServer(http.Dir(filepath.Join(root, "web", "dist")))
	handler := f.s.Handler()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__crypto_browser/recipient" {
			if r.Method != "POST" || r.Header.Get("X-Synthetic-Control") != f.token {
				http.Error(w, "forbidden", 403)
				return
			}
			recipient, e := f.s.Crypto.RequestRecipient(r.Context(), 1, "tron", "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE")
			if e == nil {
				_, e = f.s.Crypto.ApproveRecipient(r.Context(), recipient.ID, 168)
			}
			if e == nil {
				_, e = f.p.Exec(r.Context(), "UPDATE crypto_recipients SET usable_at=clock_timestamp()-interval '1 second' WHERE id=$1", recipient.ID)
			}
			if e != nil {
				http.Error(w, "fixture failed", 500)
				return
			}
			w.WriteHeader(204)
			return
		}
		if r.URL.Path == "/__crypto_browser/deposit" {
			if r.Method != "POST" || r.Header.Get("X-Synthetic-Control") != f.token {
				http.Error(w, "forbidden", 403)
				return
			}
			var in struct {
				ID string `json:"id"`
			}
			if json.NewDecoder(r.Body).Decode(&in) != nil {
				http.Error(w, "invalid", 400)
				return
			}
			v, e := f.s.Crypto.GetInvoice(r.Context(), 1, in.ID)
			if e != nil {
				http.Error(w, "missing", 404)
				return
			}
			e = f.s.Crypto.RecordDeposit(r.Context(), cryptopay.Deposit{Network: v.Network, TxID: "browser-deposit-" + v.ID, BlockHash: "local-browser-block", BlockHeight: 1, Address: v.Address, Amount: v.Amount, OccurredAt: v.ExpiresAt.Add(-time.Minute)})
			if e != nil {
				http.Error(w, "deposit failed", 500)
				return
			}
			w.WriteHeader(204)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/console/") || strings.HasPrefix(r.URL.Path, "/crypto/") {
			handler.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			assets.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(root, "web", "dist", "index.html"))
	}))
	defer api.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "web", "e2e", "crypto-local-browser.mjs"))
	cmd.Dir = filepath.Join(root, "web")
	evidence := os.Getenv("GOEPAY_CRYPTO_BROWSER_EVIDENCE")
	if evidence == "" {
		evidence = filepath.Join(root, ".tools", "usdt", "browser")
	}
	cmd.Env = append(os.Environ(), "CRYPTO_BROWSER_ORIGIN="+api.URL, "CRYPTO_BROWSER_SESSION="+f.token, "CRYPTO_BROWSER_EVIDENCE="+evidence)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("local crypto browser: %v\n%s", e, out)
	}
	var paid, orders, journals int
	if e = f.p.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM crypto_invoices WHERE state='paid'),(SELECT count(*) FROM orders),(SELECT count(*) FROM journals)").Scan(&paid, &orders, &journals); e != nil || paid != 4 || orders != 0 || journals != 0 {
		t.Fatal("browser asset isolation", paid, orders, journals, e)
	}
	var requests int
	if e = f.p.QueryRow(t.Context(), "SELECT count(*) FROM crypto_withdrawals WHERE merchant_id=1 AND kind='settlement' AND state='requested'").Scan(&requests); e != nil || requests != 1 {
		t.Fatal("refresh created duplicate financial request", requests, e)
	}
}
