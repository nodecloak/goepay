package payment

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type sharingV2Fixture79 struct {
	Store                              *Store
	Flow                               ProviderFlow
	Charges, SharingSends, ReturnSends atomic.Int32
	Pending                            atomic.Bool
	SharingDone                        atomic.Bool
	FinishSends                        atomic.Int32
	Remaining                          atomic.Int64
	mu                                 sync.Mutex
	declared                           map[string]bool
}

func newSharingV2Fixture79(t *testing.T, method string, recovery bool) *sharingV2Fixture79 {
	t.Helper()
	f := &sharingV2Fixture79{Store: testStore(t), declared: map[string]bool{}}
	ctx := context.Background()
	if e := f.Store.CreateMerchant(ctx, Merchant{ID: 751, Name: "sharing-owner", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	const key = "0123456789abcdef0123456789abcdef"
	up := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var env struct {
			Entries []struct {
				XMLName xml.Name
				Value   string `xml:",chardata"`
			} `xml:",any"`
		}
		if xml.Unmarshal(raw, &env) != nil {
			t.Error("invalid original XML")
			return
		}
		in := map[string]string{}
		for _, entry := range env.Entries {
			in[entry.XMLName.Local] = entry.Value
		}
		algorithm := "MD5"
		if strings.Contains(r.URL.Path, "profitsharing") {
			algorithm = "HMAC-SHA256"
		}
		if !adapter.VerifyWechatV2(in, key, algorithm, in["sign"]) || in["mch_id"] != "mch79" {
			t.Error("original payment/sharing signature or account lost")
			return
		}
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app79", "mch_id": "mch79", "nonce_str": "sharing79"}
		switch r.URL.Path {
		case "/pay/profitsharingaddreceiver", "/pay/profitsharingremovereceiver":
			out["receiver"] = in["receiver"]
		case "/pay/unifiedorder", "/pay/micropay":
			f.Charges.Add(1)
			f.mu.Lock()
			f.declared[in["out_trade_no"]] = in["profit_sharing"] == "Y"
			f.mu.Unlock()
			if r.URL.Path == "/pay/micropay" {
				if f.Pending.Load() {
					out["result_code"], out["err_code"] = "FAIL", "USERPAYING"
				} else {
					out["out_trade_no"], out["transaction_id"], out["trade_type"], out["total_fee"], out["openid"] = in["out_trade_no"], "WX_"+in["out_trade_no"], "MICROPAY", in["total_fee"], "buyer79"
				}
			} else {
				out["code_url"] = "weixin://wxpay/bizpayurl?pr=local79"
			}
		case "/secapi/pay/profitsharing":
			f.SharingSends.Add(1)
			if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
				t.Error("sharing missing actual mTLS")
			}
			fallthrough
		case "/pay/profitsharingquery":
			out["out_order_no"], out["transaction_id"], out["order_id"], out["status"] = in["out_order_no"], in["transaction_id"], "WX_"+in["out_order_no"], "PROCESSING"
			rows := []map[string]any{{"type": "MERCHANT_ID", "account": "receiver79", "amount": 200, "result": "PENDING"}}
			if strings.HasPrefix(in["out_order_no"], "PF") {
				rows[0]["account"], rows[0]["amount"] = "mch79", 1000
			}
			if f.SharingDone.Load() {
				out["status"] = "FINISHED"
				rows[0]["result"], rows[0]["detail_id"] = "SUCCESS", "DETAIL79"
			}
			b, _ := json.Marshal(rows)
			out["receivers"] = string(b)
		case "/secapi/pay/profitsharingfinish":
			f.FinishSends.Add(1)
			if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
				t.Error("finish missing actual client certificate")
			}
			out["out_order_no"], out["transaction_id"], out["order_id"] = in["out_order_no"], in["transaction_id"], "WX_"+in["out_order_no"]
		case "/pay/profitsharingorderamountquery":
			out["transaction_id"], out["unsplit_amount"] = in["transaction_id"], strconv.FormatInt(f.Remaining.Load(), 10)
		case "/secapi/pay/profitsharingreturn":
			f.ReturnSends.Add(1)
			delete(out, "result_code")
			out["out_order_no"], out["order_id"], out["out_return_no"], out["return_no"], out["return_account_type"], out["return_account"], out["return_amount"], out["result"] = in["out_order_no"], "WX_"+in["out_order_no"], in["out_return_no"], "WX_"+in["out_return_no"], in["return_account_type"], in["return_account"], in["return_amount"], "SUCCESS"
		default:
			t.Error("unexpected local V2 endpoint", r.URL.Path)
			return
		}
		out["sign"], _ = adapter.SignWechatV2(out, key, algorithm)
		var b bytes.Buffer
		b.WriteString("<xml>")
		for field, value := range out {
			fmt.Fprintf(&b, "<%s>", field)
			xml.EscapeText(&b, []byte(value))
			fmt.Fprintf(&b, "</%s>", field)
		}
		b.WriteString("</xml>")
		w.Write(b.Bytes())
	}))
	up.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequestClientCert}
	up.StartTLS()
	t.Cleanup(up.Close)
	transport := up.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.Certificates = []tls.Certificate{scanClientCertificate78(t)}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport}
	p, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "app79", MerchantID: "mch79", APIKey: key, BaseURL: up.URL, Methods: []string{"native", "scan"}, EnableChannelSharing: true, EnableScanCancellation: recovery, HTTPClient: client, RefundClient: client})
	if e != nil {
		t.Fatal(e)
	}
	f.Flow = ProviderFlow{Store: f.Store, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, ScanRecoveryEnabled: recovery, CallbackBaseURL: "https://local.example.invalid"}
	receiver, e := f.Flow.CreateChannelSharingReceiver(ctx, 751, 7, "v2-receiver79", adapter.SharingReceiver{Type: "MERCHANT_ID", Account: "receiver79", Name: strings.Repeat("名", 100), RelationType: "SUPPLIER"}, channelSharingActor)
	if e != nil {
		t.Fatal("V2 receiver/configuration binding", e)
	}
	if _, e = f.Flow.BindChannelSharingReceiver(ctx, 751, receiver.ID, "v2-bind79", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Flow.SetChannelSharingRule(ctx, 751, 7, ChannelSharingRuleInput{Enabled: true, Methods: []string{method}, Items: []ChannelSharingRuleItem{{ReceiverID: receiver.ID, Mode: "rate", RateBPS: 2000}}}, 0, channelSharingActor); e != nil {
		t.Fatal(e)
	}
	return f
}

func TestChannelSharingV2Single79DeclarationSettlementAndOriginalReturn(t *testing.T) {
	f := newSharingV2Fixture79(t, "native", false)
	ctx := context.Background()
	o, e := f.Store.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "v2-native79", Type: "wxpay", Name: "local79", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Flow.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	declared := f.declared[o.TradeNo]
	f.mu.Unlock()
	if !declared {
		t.Fatal("original V2 payment omitted profit_sharing=Y")
	}
	d, e := f.Store.ChannelSharingPayment(ctx, 751, o.TradeNo, channelSharingActor)
	if e != nil || !d.Declared || d.Product != "wxpay_v2_single_direct" {
		t.Fatal(d, e)
	}
	if e = f.Store.markPaid(ctx, o.TradeNo, "v2-paid79", true, PaidReceipt{ChannelID: 7, ProviderTradeNo: "WX_" + o.TradeNo}); e != nil {
		t.Fatal(e)
	}
	first, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "v2-share79", channelSharingActor)
	if e != nil || first.Completed {
		t.Fatal(first, e)
	}
	f.SharingDone.Store(true)
	f.Remaining.Store(1)
	if _, e = f.Flow.QueryChannelSharing(ctx, 751, "v2-share79", channelSharingActor); !errors.Is(e, ErrState) {
		t.Fatal("nonzero upstream amount released local funds", e)
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 0 {
		t.Fatal(b, e)
	}
	f.Remaining.Store(0)
	final, e := f.Flow.QueryChannelSharing(ctx, 751, "v2-share79", channelSharingActor)
	if e != nil || !final.Completed || !final.FundsReleased {
		t.Fatal(final, e)
	}
	ret, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, "v2-share79", d.Items[0].ReceiverID, "v2-return79", 100, channelSharingActor)
	if e != nil || ret.State != "succeeded" {
		t.Fatal(ret, e)
	}
	if _, e = f.Flow.SubmitChannelSharingReturn(ctx, 751, "v2-share79", d.Items[0].ReceiverID, "v2-return79", 100, channelSharingActor); e != nil {
		t.Fatal(e)
	}
	b, e = f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 900 || f.Charges.Load() != 1 || f.SharingSends.Load() != 1 || f.ReturnSends.Load() != 1 {
		t.Fatal("original V2 funds or send-once violated", b, e)
	}
	report, e := f.Store.CheckLedger(ctx)
	if e != nil || report != (LedgerCheck{}) {
		t.Fatal(report, e)
	}
}

func TestChannelSharingV2Single79IndependentFinishKeepsReserveUntilExplicitQuery(t *testing.T) {
	f := newSharingV2Fixture79(t, "native", false)
	ctx := context.Background()
	o, e := f.Store.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "v2-finish79", Type: "wxpay", Name: "local79", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Flow.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e != nil {
		t.Fatal(e)
	}
	if e = f.Store.markPaid(ctx, o.TradeNo, "v2-finish-paid79", true, PaidReceipt{ChannelID: 7, ProviderTradeNo: "WX_" + o.TradeNo}); e != nil {
		t.Fatal(e)
	}
	first, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, o.TradeNo, "v2-finish79", channelSharingActor)
	if e != nil || first.State != "pending" || first.FundsReleased {
		t.Fatal("acceptance released V2 funding", first, e)
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 0 {
		t.Fatal(b, e)
	}
	if _, e = f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "v2-forbidden-sharing79", channelSharingActor); e == nil {
		t.Fatal("original funding was claimed by both PF and PS")
	}
	f.SharingDone.Store(true)
	final, e := f.Flow.QueryChannelSharingFinish(ctx, 751, "v2-finish79", channelSharingActor)
	if e != nil || final.State != "succeeded" || !final.FundsReleased {
		t.Fatal(final, e)
	}
	if _, e = f.Flow.SubmitChannelSharingFinish(ctx, 751, o.TradeNo, "v2-finish79", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	b, e = f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 1000 || f.FinishSends.Load() != 1 || f.SharingSends.Load() != 0 {
		t.Fatal("V2 finish reused funds or sent twice", b, e)
	}
	report, e := f.Store.CheckLedger(ctx)
	if e != nil || report != (LedgerCheck{}) {
		t.Fatal(report, e)
	}
}

func TestChannelSharingV2Single79BarcodeDeclarationPreservesRecoveryOrigin(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		t.Run(strconv.FormatBool(recovery), func(t *testing.T) {
			f := newSharingV2Fixture79(t, "scan", recovery)
			ctx := context.Background()
			f.Pending.Store(recovery)
			o, e := f.Store.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "v2-barcode79", Type: "wxpay", Name: "local79", Amount: 1000})
			if e != nil {
				t.Fatal(e)
			}
			_, e = f.Flow.SubmitScan(ctx, o.TradeNo, 7, "127.0.0.1", "120000000000000079")
			if !recovery && e != nil || recovery && e == nil {
				t.Fatal(e)
			}
			f.mu.Lock()
			declared := f.declared[o.TradeNo]
			f.mu.Unlock()
			if !declared {
				t.Fatal("barcode omitted original Pro profit_sharing=Y")
			}
			d, e := f.Store.ChannelSharingPayment(ctx, 751, o.TradeNo, channelSharingActor)
			if e != nil || !d.Declared || d.Method != "scan" {
				t.Fatal(d, e)
			}
			var origins, eligibility, jobs int
			if e = f.Store.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM scan_recovery_origins),(SELECT count(*) FROM scan_recovery_eligibility),(SELECT count(*) FROM jobs WHERE kind='payment.scan-recover')").Scan(&origins, &eligibility, &jobs); e != nil {
				t.Fatal(e)
			}
			want := 0
			if recovery {
				want = 1
			}
			if origins != want || eligibility != want || jobs != want {
				t.Fatal("declaration/recovery not atomic", origins, eligibility, jobs)
			}
			if _, e = f.Flow.SubmitScan(ctx, o.TradeNo, 7, "127.0.0.1", "120000000000000079"); e != nil && !errors.Is(e, ErrScanPending) {
				t.Fatal("original barcode replay", e)
			}
			if f.Charges.Load() != 1 {
				t.Fatal("barcode resent")
			}
		})
	}
}

func TestChannelSharingV2Single79MigrationConstraintNames(t *testing.T) {
	s := testStore(t)
	rows, e := s.pool.Query(context.Background(), `SELECT c.relname,k.conname,pg_get_constraintdef(k.oid) FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND k.contype='c' AND c.relname IN ('channel_sharing_receivers','channel_sharing_rules','provider_attempt_sharing') AND pg_get_constraintdef(k.oid) LIKE '%product%' ORDER BY 1,2`)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var table, name, definition string
		if e = rows.Scan(&table, &name, &definition); e != nil {
			t.Fatal(e)
		}
		count++
		if !strings.Contains(definition, "wxpay_v2_single_direct") {
			t.Error("V2 product absent", table, name)
		}
	}
	if e = rows.Err(); e != nil || count != 4 {
		t.Fatal("product constraints", count, e)
	}
}
