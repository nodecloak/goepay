package payment

import (
	"context"
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func localSettlementBankCertificate(t *testing.T) (adapter.AlipayConfig, *rsa.PrivateKey, string) {
	t.Helper()
	keys := make([]*rsa.PrivateKey, 3)
	for i := range keys {
		var e error
		keys[i], e = rsa.GenerateKey(rand.Reader, 2048)
		if e != nil {
			t.Fatal(e)
		}
	}
	now := time.Now()
	root := &x509.Certificate{SerialNumber: big.NewInt(41), Subject: pkix.Name{CommonName: "LocalBankRoot"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	rootDER, e := x509.CreateCertificate(rand.Reader, root, root, &keys[0].PublicKey, keys[0])
	if e != nil {
		t.Fatal(e)
	}
	root, e = x509.ParseCertificate(rootDER)
	if e != nil {
		t.Fatal(e)
	}
	pems := []string{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}))}
	for i := 1; i < 3; i++ {
		leaf := &x509.Certificate{SerialNumber: big.NewInt(int64(i + 1)), Subject: pkix.Name{CommonName: "LocalBankLeaf"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(30 * time.Minute), KeyUsage: x509.KeyUsageDigitalSignature}
		der, e := x509.CreateCertificate(rand.Reader, leaf, root, &keys[i].PublicKey, keys[0])
		if e != nil {
			t.Fatal(e)
		}
		pems = append(pems, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
	}
	return adapter.AlipayConfig{AppID: "local-settlement-bank-app", PrivateKey: keys[1], CertificateMode: &adapter.AlipayCertificateConfig{RootPEM: pems[0], ApplicationPEM: pems[1], PlatformPEM: pems[2]}, EnableBankTransfer: true, BankTransferScene: "企业退款", BankTransferReports: []adapter.AlipayTransferSceneReport{{InfoType: "退款原因", InfoContent: "本地合成测试"}}}, keys[2], fmt.Sprintf("%x", md5.Sum([]byte("CN=LocalBankRoot3")))
}

func TestSettlementBankTLSOriginalRecipientNetAndVerifiedReturn(t *testing.T) {
	for _, product := range []string{adapter.BankProductWechat, adapter.BankProductAlipay} {
		t.Run(product, func(t *testing.T) {
			s, g, _ := batchFixture(t)
			ctx := context.Background()
			transferSetting(t, s, "settlement_fee_rate_bps", "300")
			card, name := "6222020000000000", "Original Local Bank Recipient"
			var phase, writes, queries atomic.Int32
			var expectedTF string
			bankKey, e := rsa.GenerateKey(rand.Reader, 2048)
			if e != nil {
				t.Fatal(e)
			}
			aliCfg, platformKey, serial := localSettlementBankCertificate(t)
			apiKey := "0123456789abcdef0123456789abcdef"
			up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var claims int
				if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM settlement_payouts WHERE send_state='claimed'").Scan(&claims); e != nil || claims != 1 {
					t.Error("provider IO before durable claim", claims, e)
				}
				if product == adapter.BankProductWechat {
					params := map[string]string{}
					decoder := xml.NewDecoder(r.Body)
					for {
						token, e := decoder.Token()
						if e != nil {
							break
						}
						if start, ok := token.(xml.StartElement); ok && start.Name.Local != "xml" {
							var v string
							if e = decoder.DecodeElement(&v, &start); e != nil {
								t.Error(e)
							}
							params[start.Name.Local] = v
						}
					}
					if !adapter.VerifyWechatV2(params, apiKey, "MD5", params["sign"]) || params["mch_id"] != "original-local-mch" {
						t.Error("wrong request signature/payer")
					}
					out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "mch_id": "original-local-mch", "partner_trade_no": params["partner_trade_no"], "payment_no": "local-bank-original", "amount": "970", "cmms_amt": "2"}
					if r.URL.Path == "/mmpaysptrans/pay_bank" {
						writes.Add(1)
						for k, want := range map[string]string{"enc_bank_no": card, "enc_true_name": name} {
							cipher, e := base64.StdEncoding.DecodeString(params[k])
							if e != nil {
								t.Error(e)
							}
							plain, e := rsa.DecryptOAEP(sha1.New(), rand.Reader, bankKey, cipher, nil)
							if e != nil || string(plain) != want {
								t.Error("changed bank recipient", k, e)
							}
						}
						if params["amount"] != "970" || params["bank_code"] != "1002" {
							t.Error("wrong net/bank", params["amount"], params["bank_code"])
						}
					} else if r.URL.Path == "/mmpaysptrans/query_bank" {
						queries.Add(1)
						out["status"] = "SUCCESS"
						out["bank_no_md5"] = fmt.Sprintf("%X", md5.Sum([]byte(card)))
						out["true_name_md5"] = fmt.Sprintf("%X", md5.Sum([]byte(name)))
						out["pay_succ_time"] = "2026-10-23 12:00:00"
						if phase.Load() == 1 {
							out["amount"] = "971"
						}
						if phase.Load() >= 2 {
							out["status"] = "BANK_FAIL"
						}
						if phase.Load() == 2 {
							delete(out, "cmms_amt")
						}
					} else {
						t.Error("unexpected provider endpoint", r.URL.Path)
						w.WriteHeader(500)
						return
					}
					out["sign"], _ = adapter.SignWechatV2(out, apiKey, "MD5")
					var body strings.Builder
					body.WriteString("<xml>")
					for k, v := range out {
						body.WriteString("<" + k + ">")
						xml.EscapeText(&body, []byte(v))
						body.WriteString("</" + k + ">")
					}
					body.WriteString("</xml>")
					w.Write([]byte(body.String()))
					return
				}
				if e := r.ParseForm(); e != nil {
					t.Error(e)
					return
				}
				params := map[string]string{}
				for k, v := range r.PostForm {
					params[k] = v[0]
				}
				sign, e := adapter.SignAlipayRSA2(params, aliCfg.PrivateKey)
				if e != nil || sign != params["sign"] || params["app_id"] != aliCfg.AppID || params["app_cert_sn"] == "" {
					t.Error("wrong certificate request signature", e)
				}
				var biz map[string]any
				if e = json.Unmarshal([]byte(params["biz_content"]), &biz); e != nil {
					t.Error(e)
					return
				}
				if biz["out_biz_no"] != expectedTF || biz["product_code"] != "TRANS_BANKCARD_NO_PWD" {
					t.Error("wrong original bank product", biz)
				}
				out := map[string]any{"code": "10000", "out_biz_no": expectedTF, "order_id": "local-bank-original", "status": "SUCCESS", "trans_amount": "9.70", "pay_date": "2026-10-23 12:00:00", "order_fee": "0.12"}
				if params["method"] == "alipay.fund.trans.uni.transfer" {
					writes.Add(1)
					payee, _ := biz["payee_info"].(map[string]any)
					if biz["trans_amount"] != "9.70" || payee["identity"] != card || payee["name"] != name || payee["identity_type"] != "BANKCARD_ACCOUNT" {
						t.Error("wrong net or recipient", biz)
					}
					out["status"] = "DEALING"
					delete(out, "trans_amount")
				} else if params["method"] == "alipay.fund.trans.common.query" {
					queries.Add(1)
					if phase.Load() == 1 {
						out["trans_amount"] = "9.71"
					}
					if phase.Load() >= 2 {
						out["status"] = "REFUND"
					}
					if phase.Load() == 2 {
						delete(out, "trans_amount")
					}
				} else {
					t.Error("unexpected provider method", params["method"])
					w.WriteHeader(500)
					return
				}
				node, _ := json.Marshal(out)
				d := sha256.Sum256(node)
				sig, e := rsa.SignPKCS1v15(rand.Reader, platformKey, crypto.SHA256, d[:])
				if e != nil {
					t.Error(e)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(params["method"], ".", "_") + "_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(sig), "alipay_cert_sn": serial})
			}))
			defer up.Close()
			code := ""
			if product == adapter.BankProductWechat {
				p, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "local-wx-app", MerchantID: "original-local-mch", APIKey: apiKey, BankPublicKey: &bankKey.PublicKey, BaseURL: up.URL, HTTPClient: up.Client(), RefundClient: up.Client()})
				if e != nil {
					t.Fatal(e)
				}
				g.Providers[7] = p
				code = "1002"
			} else {
				aliCfg.GatewayURL = up.URL
				aliCfg.HTTPClient = up.Client()
				p, e := adapter.NewAlipay(aliCfg)
				if e != nil {
					t.Fatal(e)
				}
				g.Providers[7] = p
			}
			cfg, e := s.SetSettlementRecipient(ctx, 501, SettlementRecipientInput{Type: "bank", Account: card, Name: name, BankCode: code}, nil, SettlementRecipientActor{Role: "merchant", ID: 501, Name: "merchant"})
			if e != nil {
				t.Fatal(e)
			}
			v, e := s.RequestFunds(ctx, "settlements", 501, 0, "tls-bank-settlement", 1000, "merchant:501")
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.ReviewFundsRequest(ctx, v.ID, true, "admin:1"); e != nil {
				t.Fatal(e)
			}
			if _, e = s.SetSettlementRecipient(ctx, 501, SettlementRecipientInput{Type: "bank", Account: "6222029999999999", Name: "Replacement Recipient", BankCode: code}, &cfg.ConfigID, SettlementRecipientActor{Role: "merchant", ID: 501, Name: "merchant"}); e != nil {
				t.Fatal(e)
			}
			out, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil)
			if e != nil || writes.Load()+queries.Load() != 0 {
				t.Fatal(out, e)
			}
			expectedTF = fmt.Sprintf("TF%d", out.TransferID)
			out, e = g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil)
			if e != nil || out.State != "pending" || writes.Load() != 1 {
				t.Fatal(out, e)
			}
			g.AllowLive = false
			out, e = g.QuerySettlementPayout(ctx, v.ID, settlementAdmin)
			if e != nil || out.State != "succeeded" || out.Funding.ReservedAmount != 30 {
				t.Fatal(out, e)
			}
			phase.Store(1)
			if _, e = g.QuerySettlementPayout(ctx, v.ID, settlementAdmin); !errors.Is(e, adapter.ErrAuthentication) {
				t.Fatal("wrong signed amount accepted", e)
			}
			phase.Store(2)
			out, e = g.QuerySettlementPayout(ctx, v.ID, settlementAdmin)
			if !errors.Is(e, ErrBankReversalPending) || out.Funding.ReservedAmount != 30 {
				t.Fatal("missing bank return proof released fee", out, e)
			}
			phase.Store(3)
			out, e = g.QuerySettlementPayout(ctx, v.ID, settlementAdmin)
			if e != nil || out.State != "reversed" || out.Funding.State != "returned" {
				t.Fatal(out, e)
			}
			bal, e := s.Balance(ctx, 501)
			if e != nil || bal.Available != 1000 || bal.Reserved != 0 || writes.Load() != 1 || queries.Load() != 4 {
				t.Fatal(bal, e, writes.Load(), queries.Load())
			}
			report, e := s.CheckLedger(ctx)
			if e != nil || report.InvalidSettlementFundings != 0 || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
				t.Fatal(report, e)
			}
		})
	}
}
