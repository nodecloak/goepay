package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// complaintAlipayClient builds the adapter against a signed httptest gateway.
// The test holds the keypair, so it independently (a) verifies the adapter's
// request signature over the real AlipayRequestCanonical string and (b) signs
// each response node the adapter must verify.
func complaintAlipayClient(t *testing.T, handler func(w http.ResponseWriter, method string, biz map[string]string, key *rsa.PrivateKey)) *AlipayComplaint {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(b))
		params := map[string]string{}
		for k := range form {
			params[k] = form.Get(k)
		}
		// Independent request-signature check: recompute the canonical content
		// and verify the adapter's RSA2 signature against the test public key.
		canonical := AlipayRequestCanonical(params)
		if !verifyRSA([]byte(canonical), params["sign"], &key.PublicKey) {
			t.Errorf("adapter request signature invalid for %s", params["method"])
		}
		if params["sign_type"] != "RSA2" || params["app_id"] != "2021app" {
			t.Errorf("request params %v", params)
		}
		handler(w, params["method"], params, key)
	}))
	t.Cleanup(srv.Close)
	c, e := NewAlipayComplaint(AlipayConfig{
		AppID: "2021app", SellerID: "2088000000000001", PrivateKey: key, PublicKey: &key.PublicKey,
		GatewayURL: srv.URL, HTTPClient: srv.Client(),
	})
	if e != nil {
		t.Fatal(e)
	}
	return c
}

// alipaySignNode signs a response node with the gateway key so the adapter's
// verifyResponse accepts it.
func alipaySignNode(t *testing.T, method, node string, key *rsa.PrivateKey) string {
	t.Helper()
	sig, e := rsaSign(key, crypto.SHA256, node)
	if e != nil {
		t.Fatal(e)
	}
	field := strings.ReplaceAll(method, ".", "_") + "_response"
	return `{"` + field + `":` + node + `,"sign":"` + sig + `"}`
}

func TestComplaintAlipayBatchQueryAndDetail(t *testing.T) {
	c := complaintAlipayClient(t, func(w http.ResponseWriter, method string, biz map[string]string, key *rsa.PrivateKey) {
		switch method {
		case "alipay.merchant.tradecomplain.batchquery":
			node := `{"code":"10000","msg":"Success","page_num":1,"page_size":10,"total_num":1,"trade_complain_infos":[` +
				`{"complain_event_id":"2088CE01","status":"MERCHANT_PROCESSING","leaf_category_name":"商品问题","complain_reason":"未发货","content":"下单未发货","merchant_order_no":"T200","trade_amount":"12.00","phone_no":"139","gmt_create":"2026-10-28 09:00:00"}]}`
			io.WriteString(w, alipaySignNode(t, method, node, key))
		case "alipay.merchant.tradecomplain.query":
			if biz := biz["biz_content"]; !strings.Contains(biz, `"complain_event_id":"2088CE01"`) {
				t.Errorf("query biz %s", biz)
			}
			node := `{"code":"10000","complain_event_id":"2088CE01","status":"MERCHANT_PROCESSING","leaf_category_name":"商品问题","complain_reason":"未发货","content":"下单未发货","merchant_order_no":"T200","trade_amount":"12.00","phone_no":"139","gmt_create":"2026-10-28 09:00:00","gmt_modified":"2026-10-28 10:00:00","reply_detail_infos":[{"reply_role":"MERCHANT","reply_content":"正在处理","reply_time":"2026-10-28 09:30:00"}]}`
			io.WriteString(w, alipaySignNode(t, method, node, key))
		default:
			t.Errorf("unexpected method %s", method)
		}
	})

	page, e := c.BatchQuery(context.Background(), "", "", "", 1, 10)
	if e != nil || page.TotalCount != 1 || len(page.Items) != 1 {
		t.Fatalf("page %+v %v", page, e)
	}
	item := page.Items[0]
	if item.ThirdID != "2088CE01" || item.PayType != "alipay" || item.Type != "商品问题" || item.Title != "未发货" || len(item.TradeNos) != 1 || item.TradeNos[0] != "T200" {
		t.Fatalf("item %+v", item)
	}
	if item.AmountCents == nil || *item.AmountCents != 1200 {
		t.Fatalf("amount %v", item.AmountCents)
	}

	rec, e := c.Query(context.Background(), "2088CE01")
	if e != nil || rec.ThirdID != "2088CE01" || rec.ModifiedAt != "2026-10-28 10:00:00" || len(rec.Negotiation) != 1 || rec.Negotiation[0].Content != "正在处理" {
		t.Fatalf("detail %+v %v", rec, e)
	}
}

func TestComplaintAlipayMutations(t *testing.T) {
	var lastMethod, lastBiz string
	var mode string
	c := complaintAlipayClient(t, func(w http.ResponseWriter, method string, biz map[string]string, key *rsa.PrivateKey) {
		lastMethod, lastBiz = method, biz["biz_content"]
		switch mode {
		case "ok":
			io.WriteString(w, alipaySignNode(t, method, `{"code":"10000","msg":"Success"}`, key))
		case "rejected":
			io.WriteString(w, alipaySignNode(t, "error", `{"code":"40004","msg":"Business Failed","sub_code":"COMPLAINT_NOT_EXIST"}`, key))
		}
	})

	mode = "ok"
	if e := c.FeedbackSubmit(context.Background(), "2088CE01", "REFUND", "已全额退款", []string{"img1", "img2"}); e != nil {
		t.Fatal("feedback", e)
	}
	if lastMethod != "alipay.merchant.tradecomplain.feedback.submit" || !strings.Contains(lastBiz, `"feedback_code":"REFUND"`) || !strings.Contains(lastBiz, `"feedback_images":"img1,img2"`) {
		t.Fatalf("feedback biz %s", lastBiz)
	}
	if e := c.ReplySubmit(context.Background(), "2088CE01", "感谢反馈", nil); e != nil || lastMethod != "alipay.merchant.tradecomplain.reply.submit" {
		t.Fatal("reply", e, lastMethod)
	}
	if e := c.SupplementSubmit(context.Background(), "2088CE01", "补充凭证", nil); e != nil || lastMethod != "alipay.merchant.tradecomplain.supplement.submit" {
		t.Fatal("supplement", e, lastMethod)
	}

	// A definitive business refusal surfaces as a ProviderError (not success).
	mode = "rejected"
	var pe *ProviderError
	if e := c.FeedbackSubmit(context.Background(), "2088CE01", "REFUND", "x", nil); !errors.As(e, &pe) {
		t.Fatalf("business refusal must be definitive, got %T %v", e, e)
	}

	// Local validation guards.
	if e := c.FeedbackSubmit(context.Background(), "bad!!", "C", "x", nil); !errors.Is(e, ErrInvalid) {
		t.Fatal("bad id accepted", e)
	}
	if e := c.ReplySubmit(context.Background(), "2088CE01", "", nil); !errors.Is(e, ErrInvalid) {
		t.Fatal("empty reply accepted", e)
	}
}
