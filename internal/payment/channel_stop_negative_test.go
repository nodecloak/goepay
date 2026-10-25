package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

type unverifiedFaultProvider76 struct{ *autoScopeProvider73 }

type mismatchedFaultProvider76 struct {
	*adapter.Alipay
	mode string
}

func (*mismatchedFaultProvider76) TransferAccountIdentity() string { return "seller:2088000000000073" }
func (p *mismatchedFaultProvider76) Transfer(ctx context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	if p.mode == "wrong-tf" {
		r.TransferNo = "TF99999999999999"
	}
	if p.mode == "wrong-amount" {
		r.Amount++
	}
	return p.Alipay.Transfer(ctx, r)
}

func (p *unverifiedFaultProvider76) Transfer(context.Context, adapter.TransferRequest) (adapter.TransferResult, error) {
	p.transfers.Add(1)
	return adapter.TransferResult{}, &adapter.ProviderError{Code: "40004:PAYER_BALANCE_NOT_ENOUGH"}
}

func TestChannelStop76PayeeForgedTransportAndCallerMadeErrorsCannotStopChannel(t *testing.T) {
	for _, mode := range []string{"payee", "forged", "http503", "caller-made", "wrong-tf", "wrong-amount", "wrong-payer"} {
		t.Run(mode, func(t *testing.T) {
			s, g, fake, _, _, id := autoReady73(t)
			ctx := context.Background()
			g.ChannelAutoStopEnabled = true
			var writes atomic.Int32
			if mode == "caller-made" {
				g.Providers[7] = &unverifiedFaultProvider76{fake}
			} else {
				merchant, e := rsa.GenerateKey(rand.Reader, 2048)
				if e != nil {
					t.Fatal(e)
				}
				platform, e := rsa.GenerateKey(rand.Reader, 2048)
				if e != nil {
					t.Fatal(e)
				}
				up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					writes.Add(1)
					if e := r.ParseForm(); e != nil {
						t.Error(e)
						return
					}
					params := map[string]string{}
					for k, v := range r.PostForm {
						if len(v) != 1 {
							t.Error("ambiguous gateway request")
						}
						params[k] = v[0]
					}
					signature, e := adapter.SignAlipayRSA2(params, merchant)
					if e != nil || signature != params["sign"] || params["method"] != "alipay.fund.trans.toaccount.transfer" {
						t.Error("wrong original signed submit", e)
					}
					if mode == "http503" {
						w.WriteHeader(503)
						return
					}
					code := "PAYER_BALANCE_NOT_ENOUGH"
					if mode == "payee" {
						code = "PAYEE_NOT_EXIST"
					}
					raw, _ := json.Marshal(map[string]any{"code": "40004", "sub_code": code})
					d := sha256.Sum256(raw)
					sig, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, d[:])
					if mode == "forged" {
						sig[0] ^= 1
					}
					json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(params["method"], ".", "_") + "_response": json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(sig)})
				}))
				defer up.Close()
				seller := "2088000000000073"
				if mode == "wrong-payer" {
					seller = "2088000000001073"
				}
				ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "batch-test-app", SellerID: seller, PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client()})
				if e != nil {
					t.Fatal(e)
				}
				g.Providers[7] = ali
				if strings.HasPrefix(mode, "wrong-") {
					g.Providers[7] = &mismatchedFaultProvider76{ali, mode}
				}
			}
			p, e := g.ExecuteSettlementAuto(ctx, id, nil)
			if e == nil {
				t.Fatal("test did not produce original provider failure", p)
			}
			if strings.HasPrefix(mode, "wrong-") {
				if _, verified := adapter.VerifiedTransferFailure(e); !verified || !errors.Is(e, ErrConflict) {
					t.Fatal("wrong-source test did not reach authenticated receipt rejection", e, verified)
				}
			}
			status, readErr := s.ChannelWriteStatus(ctx, 7)
			if readErr != nil || status.Blocked || status.EventID != 0 {
				t.Fatal("untrusted or payee failure stopped channel", status, readErr)
			}
			var sources, notices int
			if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM channel_stop_events),(SELECT count(*) FROM jobs WHERE kind='admin.channel-stopped')`).Scan(&sources, &notices); e != nil || sources != 0 || notices != 0 {
				t.Fatal("invented stop/notice", sources, notices, e)
			}
			balance, e := s.Balance(ctx, 7395)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "payee" {
				if p.State != "failed" || balance.Available != 2000 || balance.Reserved != 0 {
					t.Fatal("original definite payee rejection rules changed", p.State, balance)
				}
			} else if p.State != "unknown" || balance.Available != 1000 || balance.Reserved != 1000 {
				t.Fatal("uncertain failure released original gross", p.State, balance)
			}
			if mode == "caller-made" {
				if fake.transfers.Load() != 1 {
					t.Fatal("wrong fake submit count", fake.transfers.Load())
				}
			} else if writes.Load() != 1 {
				t.Fatal("failure was retried", writes.Load())
			}
		})
	}
}
