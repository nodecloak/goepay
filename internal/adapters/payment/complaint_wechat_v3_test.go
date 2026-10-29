package payment

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// complaintV3Client builds the adapter wired to a signed httptest upstream,
// reusing the package's v3TLSTransport + v3SignedResponse harness so the
// client's request signature and the server's response signature are both
// authenticated exactly as in production.
func complaintV3Client(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, platform *rsa.PrivateKey)) *WechatV3Complaint {
	t.Helper()
	merchant, _ := rsa.GenerateKey(rand.Reader, 2048)
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Independently verify the adapter's request signature against the exact
		// canonical string (method\n uri\n ts\n nonce\n body\n).
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		fields := map[string]string{}
		for _, m := range regexp.MustCompile(`([a-z_]+)="([^"]+)"`).FindAllStringSubmatch(r.Header.Get("Authorization"), -1) {
			fields[m[1]] = m[2]
		}
		canonical := r.Method + "\n" + r.URL.RequestURI() + "\n" + fields["timestamp"] + "\n" + fields["nonce_str"] + "\n" + string(raw) + "\n"
		hash := sha256.Sum256([]byte(canonical))
		sig, _ := base64.StdEncoding.DecodeString(fields["signature"])
		if fields["mchid"] != "local-mch" || fields["serial_no"] != "SERIAL" || rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, hash[:], sig) != nil {
			t.Errorf("adapter request signature mismatch for %s %s", r.Method, r.URL.RequestURI())
		}
		handler(w, r, platform)
	}))
	t.Cleanup(up.Close)
	c, e := NewWechatV3Complaint(WechatV3Config{
		AppID: "local-app", MerchantID: "local-mch", MerchantSerial: "SERIAL",
		PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"),
		VerificationKeys: map[string]*rsa.PublicKey{"PUB_LOCAL": &platform.PublicKey},
		HTTPClient:       &http.Client{Transport: v3TLSTransport{up, t}},
	})
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestComplaintWechatV3BatchQueryAndDetail(t *testing.T) {
	const listBody = `{"limit":10,"offset":0,"total_count":2,"data":[` +
		`{"complaint_id":"200000","complaint_state":"PENDING","complaint_detail":"未收到商品","payer_phone":"138","complaint_time":"2026-10-28T10:00:00+08:00","complaint_order_info":[{"out_trade_no":"T100","amount":1200}]},` +
		`{"complaint_id":"200001","complaint_state":"PROCESSING","complaint_detail":"重复扣款","complaint_order_info":[{"out_trade_no":"T101"}]}]}`
	const detailBody = `{"complaint_id":"200000","complainted_mchid":"local-mch","complaint_state":"PROCESSING","complaint_detail":"未收到商品","payer_phone":"138","complaint_order_info":[{"out_trade_no":"T100","amount":1200}]}`

	c := complaintV3Client(t, func(w http.ResponseWriter, r *http.Request, platform *rsa.PrivateKey) {
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v3/merchant-service/complaints-v2") && r.URL.Query().Get("begin_date") != "":
			if q := r.URL.Query(); q.Get("limit") != "10" || q.Get("offset") != "0" || q.Get("begin_date") != "2026-10-01" || q.Get("end_date") != "2026-10-28" {
				t.Errorf("batch query params %v", q)
			}
			v3SignedResponse(t, w, platform, 200, listBody, "PUB_LOCAL")
		case r.Method == "GET" && r.URL.Path == "/v3/merchant-service/complaints-v2/200000":
			v3SignedResponse(t, w, platform, 200, detailBody, "PUB_LOCAL")
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	page, e := c.BatchQuery(context.Background(), "2026-10-01", "2026-10-28", 1, 10)
	if e != nil {
		t.Fatal(e)
	}
	if page.TotalCount != 2 || len(page.Items) != 2 {
		t.Fatalf("page %+v", page)
	}
	first := page.Items[0]
	if first.ThirdID != "200000" || first.PayType != "wxpay" || first.Phone != "138" || len(first.TradeNos) != 1 || first.TradeNos[0] != "T100" {
		t.Fatalf("item0 %+v", first)
	}
	if first.AmountCents == nil || *first.AmountCents != 1200 {
		t.Fatalf("amount %+v", first.AmountCents)
	}
	// Second item has an order with no amount -> amount stays unknown (nil), never 0.
	if page.Items[1].AmountCents != nil {
		t.Fatalf("unknown amount must stay nil, got %v", *page.Items[1].AmountCents)
	}

	rec, e := c.Query(context.Background(), "200000")
	if e != nil || rec.ThirdID != "200000" || rec.ThirdMchID != "local-mch" || rec.Status != "PROCESSING" {
		t.Fatalf("detail %+v %v", rec, e)
	}
}

func TestComplaintWechatV3MutationsAndOutcomes(t *testing.T) {
	var lastPath, lastBody string
	var mode string
	c := complaintV3Client(t, func(w http.ResponseWriter, r *http.Request, platform *rsa.PrivateKey) {
		b, _ := io.ReadAll(r.Body)
		lastPath, lastBody = r.URL.Path, string(b)
		switch mode {
		case "ok": // WeChat returns 204 empty on success.
			v3SignedResponse(t, w, platform, 204, "", "PUB_LOCAL")
		case "nonempty": // a signed but non-empty body on a mutation is an unknown outcome
			v3SignedResponse(t, w, platform, 200, `{"unexpected":true}`, "PUB_LOCAL")
		case "rejected":
			w.WriteHeader(400)
			w.Header().Set("Content-Type", "application/json")
			// definitive rejection handled by client when code is known; here a 400 is unknown without a signed body
			io.WriteString(w, `{"code":"PARAM_ERROR"}`)
		}
	})

	mode = "ok"
	if e := c.Response(context.Background(), "200000", "我们已处理，请查收退款", []string{"media-1"}); e != nil {
		t.Fatal("response", e)
	}
	if lastPath != "/v3/merchant-service/complaints-v2/200000/response" || !strings.Contains(lastBody, `"complainted_mchid":"local-mch"`) || !strings.Contains(lastBody, `"response_images":["media-1"]`) {
		t.Fatalf("response body %s %s", lastPath, lastBody)
	}
	if e := c.Complete(context.Background(), "200000"); e != nil || lastPath != "/v3/merchant-service/complaints-v2/200000/complete" {
		t.Fatal("complete", e, lastPath)
	}
	if e := c.UpdateRefundProgress(context.Background(), "200000", "APPROVED", "", "已同意退款"); e != nil {
		t.Fatal("refund-progress", e)
	}
	if !strings.Contains(lastBody, `"action":"APPROVED"`) || !strings.Contains(lastBody, `"remark":"已同意退款"`) {
		t.Fatalf("refund body %s", lastBody)
	}
	// REJECTED requires a reason.
	if e := c.UpdateRefundProgress(context.Background(), "200000", "REJECTED", "", ""); !errors.Is(e, ErrInvalid) {
		t.Fatal("rejected without reason must be invalid", e)
	}

	// A non-empty signed body on a mutation is an unknown outcome (never assume success).
	mode = "nonempty"
	var unknown *UnknownOutcome
	if e := c.Complete(context.Background(), "200000"); !errors.As(e, &unknown) {
		t.Fatal("non-empty mutation reply must be unknown", e)
	}

	// Local validation guards.
	if e := c.Response(context.Background(), "bad id!!", "x", nil); !errors.Is(e, ErrInvalid) {
		t.Fatal("bad id accepted", e)
	}
	if e := c.Response(context.Background(), "200000", "", nil); !errors.Is(e, ErrInvalid) {
		t.Fatal("empty content accepted", e)
	}
}
