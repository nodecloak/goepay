package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type sharingFinishFixture struct {
	*sharingExecutionFixture
	Order           Order
	Phase           atomic.Int32 // 0 pending,1 success,2 wrong mch,3 wrong amount,4 empty terminal,5 lost acceptance,6 closed,7 wrong trade
	Sends, Queries  atomic.Int32
	ReceiverDeletes atomic.Int32
	mu              sync.Mutex
	requests        map[string]string
}

func newSharingFinishFixture(t *testing.T) *sharingFinishFixture {
	t.Helper()
	base := newSharingExecutionFixture(t)
	o := base.paid(t, "original-finish-payment", false)
	return sharingFinishFixtureForOrder(t, base, o)
}
func sharingFinishFixtureForOrder(t *testing.T, base *sharingExecutionFixture, o Order) *sharingFinishFixture {
	t.Helper()
	f := &sharingFinishFixture{sharingExecutionFixture: base, Order: o, requests: map[string]string{}}
	merchant, _ := rsa.GenerateKey(rand.Reader, 2048)
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		no, trade := "", ""
		var out map[string]any
		status := 200
		if r.Method == "POST" && r.URL.Path == "/v3/profitsharing/receivers/delete" {
			f.ReceiverDeletes.Add(1)
			if e := json.NewDecoder(r.Body).Decode(&out); e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
		} else if r.Method == "POST" && r.URL.Path == "/v3/profitsharing/orders/unfreeze" {
			f.Sends.Add(1)
			var in map[string]string
			if e := json.NewDecoder(r.Body).Decode(&in); e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
			no, trade = in["out_order_no"], in["transaction_id"]
			if len(no) != 32 || !strings.HasPrefix(no, "PF") || trade != "WX_"+o.TradeNo {
				t.Error("PF lost original durable identity", no, trade)
			}
			f.mu.Lock()
			f.requests[no] = trade
			f.mu.Unlock()
			if f.Phase.Load() == 5 {
				w.WriteHeader(503)
				return
			}
			out = map[string]any{"out_order_no": no, "transaction_id": trade, "order_id": "WX_" + no, "state": "FINISHED"} // acceptance is never financial completion.
			if f.Phase.Load() == 8 {
				status = 400
				out = map[string]any{"code": "PARAM_ERROR", "message": "local synthetic rejection"}
			}
		} else if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v3/profitsharing/orders/") {
			f.Queries.Add(1)
			no = strings.TrimPrefix(r.URL.Path, "/v3/profitsharing/orders/")
			f.mu.Lock()
			trade = f.requests[no]
			f.mu.Unlock()
			if trade == "" || r.URL.Query().Get("transaction_id") != trade {
				t.Error("PF query did not use original request", r.URL.String())
				w.WriteHeader(400)
				return
			}
			state, result, account, amount := "PROCESSING", "PENDING", "original-mch", o.Amount
			phase := f.Phase.Load()
			if phase != 0 {
				state, result = "FINISHED", "SUCCESS"
			}
			if phase == 2 {
				account = "foreign-mch"
			}
			if phase == 3 {
				amount++
			}
			if phase == 6 {
				result = "CLOSED"
			}
			if phase == 7 {
				trade = "FOREIGN_TRADE"
			}
			out = map[string]any{"out_order_no": no, "transaction_id": trade, "order_id": "WX_" + no, "state": state, "receivers": []map[string]any{{"type": "MERCHANT_ID", "account": account, "amount": amount, "result": result, "detail_id": "LOCAL_FINISH_DETAIL"}}}
			if phase == 4 {
				delete(out, "receivers")
			}
		} else {
			t.Error("unexpected PF endpoint", r.Method, r.URL.String())
			w.WriteHeader(500)
			return
		}
		raw, _ := json.Marshal(out)
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := "local-finish-only"
		hash := sha256.Sum256([]byte(stamp + "\n" + nonce + "\n" + string(raw) + "\n"))
		sig, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, hash[:])
		w.Header().Set("Wechatpay-Timestamp", stamp)
		w.Header().Set("Wechatpay-Nonce", nonce)
		w.Header().Set("Wechatpay-Serial", "PUB_KEY_ID_FINISH")
		w.Header().Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(sig))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write(raw)
	}))
	t.Cleanup(up.Close)
	p, e := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local-app", MerchantID: "original-mch", MerchantSerial: "LOCAL_FINISH_SERIAL", PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_FINISH": &platform.PublicKey}, EnableChannelSharing: true, Methods: []string{"native"}, HTTPClient: &http.Client{Transport: sharingReceiverTLSTransport{up}}})
	if e != nil {
		t.Fatal(e)
	}
	f.Flow.Providers[7] = p
	return f
}
func TestChannelSharingFinishAcceptanceThenReadOnlyWorkerReleasesOnlyOriginalReserveOnce(t *testing.T) {
	f := newSharingFinishFixture(t)
	ctx := context.Background()
	first, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "original-finish", channelSharingActor)
	if e != nil || first.State != "pending" || first.FundsReleased || f.Sends.Load() != 1 || f.Queries.Load() != 0 {
		t.Fatal("acceptance must remain reserved", first, e)
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 0 || b.Reserved != 100 {
		t.Fatal(b, e)
	}
	f.Phase.Store(1)
	reader := f.Flow
	reader.AllowLive = false
	final, e := reader.ReconcileChannelSharingFinish(ctx, first.ID)
	if e != nil || final.State != "succeeded" || !final.FundsReleased || final.ReleaseAmount != 100 || final.ExpectedUnsplit != 100 {
		t.Fatal(final, e)
	}
	for i := 0; i < 3; i++ {
		if _, e = reader.QueryChannelSharingFinish(ctx, 751, "original-finish", channelSharingActor); e != nil {
			t.Fatal(e)
		}
	}
	b, e = f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 100 || b.Reserved != 0 || f.Sends.Load() != 1 || f.Queries.Load() != 1 {
		t.Fatal(b, e, f.Sends.Load(), f.Queries.Load())
	}
	var journals, system, release int
	if e = f.Store.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM journals WHERE event_key=$1),(SELECT count(*) FROM backoffice_audit WHERE resource='channel-sharing-finishes' AND operation='query-result' AND actor_role='system' AND actor_id=0),(SELECT count(*) FROM channel_sharing_fund_releases WHERE order_id=$2)", fmt.Sprintf("channel-sharing-finish:%d:release", first.ID), f.Order.ID).Scan(&journals, &system, &release); e != nil || journals != 1 || system != 1 || release != 1 {
		t.Fatal(journals, system, release, e)
	}
}
