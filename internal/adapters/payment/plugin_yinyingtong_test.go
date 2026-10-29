package payment

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// yinyingtongPinnedCtx is a plain context; the request timestamp is pinned via
// the plugin clock (see yinyingtongBind), not the context.
func yinyingtongPinnedCtx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

func TestPluginYinyingtongSpecAndBuild(t *testing.T) {
	spec, ok := LookupPlugin("yinyingtong")
	if !ok || spec.Title != "银盈通支付" || !spec.Serves("alipay") || !spec.Serves("bank") {
		t.Fatal("yinyingtong spec", ok)
	}
	// Missing required inputs rejected.
	for _, cfg := range []map[string]string{
		{"appkey": "K", "appmchid": "M"}, // no appid
		{"appid": "A", "appmchid": "M"},  // no appkey
		{"appid": "A", "appkey": "K"},    // no appmchid
	} {
		c := PluginChannel{Plugin: "yinyingtong", PayType: "alipay", Config: cfg}
		if _, e := BuildPluginChannel(c); !errors.Is(e, ErrInvalid) {
			t.Fatalf("expected ErrInvalid for %v, got %v", cfg, e)
		}
	}
	// Unserved pay type rejected.
	if _, e := BuildPluginChannel(PluginChannel{Plugin: "yinyingtong", PayType: "qqpay", Config: map[string]string{"appid": "A", "appkey": "K", "appmchid": "M"}}); e == nil {
		t.Fatal("expected unserved pay type to fail")
	}
	// Good build: Query + Refund caps, alipay methods native+wap.
	prov, e := BuildPluginChannel(PluginChannel{Plugin: "yinyingtong", PayType: "alipay", Config: map[string]string{"appid": "A", "appkey": "K", "appmchid": "M"}})
	if e != nil {
		t.Fatalf("build good: %v", e)
	}
	caps := prov.Capabilities()
	if !caps.Query || !caps.Refund {
		t.Fatalf("caps = %+v", caps)
	}
	if strings.Join(caps.Methods, ",") != "native,wap" {
		t.Fatalf("alipay methods = %v", caps.Methods)
	}
	// wxpay declares native+h5.
	wx, e := BuildPluginChannel(PluginChannel{Plugin: "yinyingtong", PayType: "wxpay", Config: map[string]string{"appid": "A", "appkey": "K", "appmchid": "M"}})
	if e != nil {
		t.Fatalf("build wxpay: %v", e)
	}
	if strings.Join(wx.Capabilities().Methods, ",") != "native,h5" {
		t.Fatalf("wxpay methods = %v", wx.Capabilities().Methods)
	}
	// bank (快捷) is not ported: no methods, so a create is invalid.
	bank, e := BuildPluginChannel(PluginChannel{Plugin: "yinyingtong", PayType: "bank", Config: map[string]string{"appid": "A", "appkey": "K", "appmchid": "M"}})
	if e != nil {
		t.Fatalf("build bank: %v", e)
	}
	if len(bank.Capabilities().Methods) != 0 {
		t.Fatalf("bank should declare no methods, got %v", bank.Capabilities().Methods)
	}
}

// TestPluginYinyingtongSignVector checks the MD5 envelope signature against a
// value computed independently with node (see yyt-sign.js in scratch):
//
//	base (ksorted, sign_keys only, non-empty): app_id, charset, data, format,
//	method, sign_type, timestamp, version ; then &key=SECRET ; upper(MD5).
func TestPluginYinyingtongSignVector(t *testing.T) {
	p := &yinyingtongPlugin{appID: "APP1", appKey: "SECRET", mchID: "MCH1"}
	data, _ := json.Marshal(map[string]string{"merchant_number": "MCH1", "order_number": "T100"})
	params := map[string]string{
		"app_id": "APP1", "method": "gepos.order.query", "format": "JSON",
		"charset": "UTF-8", "sign_type": "MD5", "timestamp": "1970-01-01 00:00:00",
		"version": "1.0", "data": string(data), "req_no": "",
		"terminal_type": "3", "browser_brand": "99",
	}
	const want = "29E66955D64DD89A2EE090652E0493C9"
	if got := p.yinyingtongSign(params); got != want {
		t.Fatalf("sign mismatch:\n got=%s\nwant=%s", got, want)
	}
	// req_no is deterministic from its seed (not part of the signed set here).
	if got := yinyingtongReqNo("T100"); got != "QF18AEFF268F5A61BBDBE1DC4" {
		t.Fatalf("reqno mismatch: %s", got)
	}
}

// yinyingtongServer stands in for the GEPOS gateway. It verifies the request
// envelope (path, method header, signature) and dispatches to reply by the
// envelope `method`, exposing the decoded bizdata.
func yinyingtongServer(t *testing.T, appKey string, reply func(method string, biz map[string]string) (code string, inner map[string]any, statusCode int)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var env map[string]string
		if e := json.Unmarshal(body, &env); e != nil {
			t.Fatalf("bad envelope: %v", e)
		}
		method := env["method"]
		if got := r.Header.Get("method"); got != "cash-api@"+method {
			t.Errorf("method header = %q for method %q", got, method)
		}
		if env["app_id"] == "" || method == "" || env["sign"] == "" || env["sign_type"] != "MD5" {
			t.Errorf("missing envelope fields: %v", env)
		}
		// Recompute the signature server-side and compare.
		signer := &yinyingtongPlugin{appKey: appKey}
		if want := signer.yinyingtongSign(env); want != env["sign"] {
			t.Errorf("signature mismatch: got %s want %s", env["sign"], want)
		}
		var biz map[string]string
		_ = json.Unmarshal([]byte(env["data"]), &biz)
		code, inner, status := reply(method, biz)
		if status == 0 {
			status = http.StatusOK
		}
		innerJSON, _ := json.Marshal(inner)
		out := map[string]any{"code": code, "data": string(innerJSON)}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(out)
	}))
}

// yinyingtongBind points the plugin's gateway at the test server and pins the
// request-timestamp clock to 1970-01-01 00:00:00 UTC for deterministic signing.
func yinyingtongBind(p *yinyingtongPlugin, srv *httptest.Server) {
	p.gateway = srv.URL
	p.client = srv.Client()
	p.now = func() time.Time { return time.Unix(0, 0).UTC() }
}

func yinyingtongBuild(t *testing.T) *yinyingtongPlugin {
	t.Helper()
	prov, e := BuildPluginChannel(PluginChannel{
		ID: 14, Plugin: "yinyingtong", PayType: "alipay",
		Config: map[string]string{"appid": "APP1", "appkey": "SECRET", "appmchid": "MCH1"},
	})
	if e != nil {
		t.Fatalf("build: %v", e)
	}
	return prov.(*yinyingtongPlugin)
}

func TestPluginYinyingtongQuerySuccess(t *testing.T) {
	srv := yinyingtongServer(t, "SECRET", func(_ string, biz map[string]string) (string, map[string]any, int) {
		if biz["order_number"] != "T100" || biz["merchant_number"] != "MCH1" {
			t.Errorf("bizdata = %v", biz)
		}
		return "000000", map[string]any{"op_ret_code": "000", "order_number": "T100", "order_id": "GID9", "orderstatus": "00", "amount": "12.30"}, 0
	})
	defer srv.Close()
	p := yinyingtongBuild(t)
	yinyingtongBind(p, srv)

	res, e := p.Query(yinyingtongPinnedCtx(t), QueryRequest{TradeNo: "T100"})
	if e != nil {
		t.Fatalf("query: %v", e)
	}
	if res.State != "SUCCESS" || res.Amount != 1230 || res.ProviderTradeNo != "GID9" || res.TradeNo != "T100" {
		t.Fatalf("result = %+v", res)
	}
}

func TestPluginYinyingtongQueryUnpaid(t *testing.T) {
	srv := yinyingtongServer(t, "SECRET", func(_ string, _ map[string]string) (string, map[string]any, int) {
		return "000000", map[string]any{"op_ret_code": "000", "order_number": "T100", "orderstatus": "01", "amount": "12.30"}, 0
	})
	defer srv.Close()
	p := yinyingtongBuild(t)
	yinyingtongBind(p, srv)
	res, e := p.Query(yinyingtongPinnedCtx(t), QueryRequest{TradeNo: "T100"})
	if e != nil {
		t.Fatalf("query: %v", e)
	}
	if res.State != "NOTPAY" {
		t.Fatalf("expected NOTPAY, got %s", res.State)
	}
}

func TestPluginYinyingtongVerifySuccess(t *testing.T) {
	srv := yinyingtongServer(t, "SECRET", func(_ string, _ map[string]string) (string, map[string]any, int) {
		return "000000", map[string]any{"op_ret_code": "000", "order_number": "T100", "order_id": "GID9", "orderstatus": "00", "amount": "12.30"}, 0
	})
	defer srv.Close()
	p := yinyingtongBuild(t)
	yinyingtongBind(p, srv)

	cb := []byte(`{"dsorderid":"T100","orderstatus":"99","amount":"999.99"}`) // body is NOT trusted
	ev, e := p.VerifyNotification(yinyingtongPinnedCtx(t), nil, cb, ExpectedPayment{TradeNo: "T100", Amount: 1230})
	if e != nil {
		t.Fatalf("verify: %v", e)
	}
	if ev.State != "SUCCESS" || ev.TradeNo != "T100" || ev.Amount != 1230 || ev.ProviderTradeNo != "GID9" {
		t.Fatalf("event = %+v", ev)
	}
}

func TestPluginYinyingtongVerifyUnpaid(t *testing.T) {
	srv := yinyingtongServer(t, "SECRET", func(_ string, _ map[string]string) (string, map[string]any, int) {
		return "000000", map[string]any{"op_ret_code": "000", "order_number": "T100", "orderstatus": "01", "amount": "12.30"}, 0
	})
	defer srv.Close()
	p := yinyingtongBuild(t)
	yinyingtongBind(p, srv)
	cb := []byte(`{"dsorderid":"T100"}`)
	if _, e := p.VerifyNotification(yinyingtongPinnedCtx(t), nil, cb, ExpectedPayment{TradeNo: "T100", Amount: 1230}); !errors.Is(e, ErrInvalid) {
		t.Fatalf("expected ErrInvalid (unpaid), got %v", e)
	}
}

func TestPluginYinyingtongVerifyWrongAmount(t *testing.T) {
	srv := yinyingtongServer(t, "SECRET", func(_ string, _ map[string]string) (string, map[string]any, int) {
		return "000000", map[string]any{"op_ret_code": "000", "order_number": "T100", "orderstatus": "00", "amount": "12.30"}, 0
	})
	defer srv.Close()
	p := yinyingtongBuild(t)
	yinyingtongBind(p, srv)
	cb := []byte(`{"dsorderid":"T100"}`)
	if _, e := p.VerifyNotification(yinyingtongPinnedCtx(t), nil, cb, ExpectedPayment{TradeNo: "T100", Amount: 9999}); !errors.Is(e, ErrAuthentication) {
		t.Fatalf("expected ErrAuthentication (amount), got %v", e)
	}
}

func TestPluginYinyingtongVerifyWrongTradeNo(t *testing.T) {
	// Query would confirm a DIFFERENT order; the callback names T200 but we
	// expect T100 — rejected before any query runs.
	srv := yinyingtongServer(t, "SECRET", func(_ string, _ map[string]string) (string, map[string]any, int) {
		t.Error("query must not run for a mismatched callback trade number")
		return "000000", nil, 0
	})
	defer srv.Close()
	p := yinyingtongBuild(t)
	yinyingtongBind(p, srv)
	cb := []byte(`{"dsorderid":"T200"}`)
	if _, e := p.VerifyNotification(yinyingtongPinnedCtx(t), nil, cb, ExpectedPayment{TradeNo: "T100", Amount: 1230}); !errors.Is(e, ErrAuthentication) {
		t.Fatalf("expected ErrAuthentication (trade no), got %v", e)
	}
}

func TestPluginYinyingtongVerifyQueryMismatchTradeNo(t *testing.T) {
	// Callback names our trade number, but the upstream reply binds to another
	// order_number: a confused/forged confirmation must be rejected.
	srv := yinyingtongServer(t, "SECRET", func(_ string, _ map[string]string) (string, map[string]any, int) {
		return "000000", map[string]any{"op_ret_code": "000", "order_number": "OTHER", "orderstatus": "00", "amount": "12.30"}, 0
	})
	defer srv.Close()
	p := yinyingtongBuild(t)
	yinyingtongBind(p, srv)
	cb := []byte(`{"dsorderid":"T100"}`)
	if _, e := p.VerifyNotification(yinyingtongPinnedCtx(t), nil, cb, ExpectedPayment{TradeNo: "T100", Amount: 1230}); !errors.Is(e, ErrAuthentication) {
		t.Fatalf("expected ErrAuthentication (reply trade no), got %v", e)
	}
}

func TestPluginYinyingtongVerifyTransportFailure(t *testing.T) {
	// Server returns 500: the outcome is unknown, never a verified payment.
	srv := yinyingtongServer(t, "SECRET", func(_ string, _ map[string]string) (string, map[string]any, int) {
		return "", nil, http.StatusInternalServerError
	})
	defer srv.Close()
	p := yinyingtongBuild(t)
	yinyingtongBind(p, srv)
	cb := []byte(`{"dsorderid":"T100"}`)
	_, e := p.VerifyNotification(yinyingtongPinnedCtx(t), nil, cb, ExpectedPayment{TradeNo: "T100", Amount: 1230})
	var unknown *UnknownOutcome
	if !errors.As(e, &unknown) {
		t.Fatalf("expected UnknownOutcome on 5xx, got %v", e)
	}
}

func TestPluginYinyingtongVerifyRefusal(t *testing.T) {
	// Definitive business refusal from the gateway => ProviderError, no ack.
	srv := yinyingtongServer(t, "SECRET", func(_ string, _ map[string]string) (string, map[string]any, int) {
		return "900999", map[string]any{"op_ret_code": "999", "op_ret_subcode": "ORDER_NOT_FOUND"}, 0
	})
	defer srv.Close()
	p := yinyingtongBuild(t)
	yinyingtongBind(p, srv)
	cb := []byte(`{"dsorderid":"T100"}`)
	e := func() error {
		_, err := p.VerifyNotification(yinyingtongPinnedCtx(t), nil, cb, ExpectedPayment{TradeNo: "T100", Amount: 1230})
		return err
	}()
	var perr *ProviderError
	if !errors.As(e, &perr) {
		t.Fatalf("expected ProviderError, got %v", e)
	}
}

func TestPluginYinyingtongAck(t *testing.T) {
	p := yinyingtongBuild(t)
	if s, _, b := p.NotificationAck(nil); s != http.StatusOK || b != "00" {
		t.Fatalf("success ack = %d %q", s, b)
	}
	if s, _, b := p.NotificationAck(ErrInvalid); s != http.StatusOK || b != "01" {
		t.Fatalf("failure ack = %d %q", s, b)
	}
	if m := p.NotificationMethods(); len(m) != 1 || m[0] != http.MethodPost {
		t.Fatalf("methods = %v", m)
	}
}

// Guard: the gateway constant is a real https URL (so pluginCall's URL guard
// passes in production); tests rewrite the transport, not the URL.
func TestPluginYinyingtongGatewayConst(t *testing.T) {
	if !strings.HasPrefix(yinyingtongGateway, "https://") {
		t.Fatalf("gateway not https: %s", yinyingtongGateway)
	}
}

func yinyingtongBuildType(t *testing.T, payType string) *yinyingtongPlugin {
	t.Helper()
	prov, e := BuildPluginChannel(PluginChannel{
		ID: 14, Plugin: "yinyingtong", PayType: payType,
		Config: map[string]string{"appid": "APP1", "appkey": "SECRET", "appmchid": "MCH1", "trade_platform_no": "REF9", "channel_merch_no": "CM1"},
	})
	if e != nil {
		t.Fatalf("build %s: %v", payType, e)
	}
	return prov.(*yinyingtongPlugin)
}

func yinyingtongCreateReq(method string) CreateRequest {
	return CreateRequest{
		TradeNo:   "T100",
		Subject:   "测试订单",
		Method:    method,
		Amount:    1230,
		NotifyURL: "https://pay.example.com/pay/notify/T100/",
		ReturnURL: "https://pay.example.com/pay/return/T100/",
	}
}

func TestPluginYinyingtongCreateAlipayNative(t *testing.T) {
	srv := yinyingtongServer(t, "SECRET", func(method string, biz map[string]string) (string, map[string]any, int) {
		if method != "gepos.pre.pay" {
			t.Errorf("method = %s", method)
		}
		for k, want := range map[string]string{
			"merchant_number": "MCH1", "order_number": "T100", "amount": "12.30",
			"pay_type": "01", "channel_code": "01", "currency": "CNY",
			"order_title": "测试订单", "notify_key_mode": "03", "ref_no": "REF9",
			"bank_mch_id":             "CM1",
			"async_notification_addr": "https://pay.example.com/pay/notify/T100/",
		} {
			if biz[k] != want {
				t.Errorf("biz[%s] = %q want %q", k, biz[k], want)
			}
		}
		if _, ok := biz["profit_sharing"]; ok {
			t.Errorf("profit_sharing set without sharing request")
		}
		return "000000", map[string]any{"op_ret_code": "000", "order_id": "GOMEORD1"}, 0
	})
	defer srv.Close()
	p := yinyingtongBuildType(t, "alipay")
	yinyingtongBind(p, srv)

	res, e := p.Create(yinyingtongPinnedCtx(t), yinyingtongCreateReq("native"))
	if e != nil {
		t.Fatalf("create: %v", e)
	}
	if res.MaterialType != "qrcode" || res.ProviderTradeNo != "GOMEORD1" {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(res.Material, "pages/preOrder/orderPay?orderId=GOMEORD1") {
		t.Fatalf("cashier url = %s", res.Material)
	}
}

func TestPluginYinyingtongCreateWxpayJump(t *testing.T) {
	srv := yinyingtongServer(t, "SECRET", func(method string, biz map[string]string) (string, map[string]any, int) {
		if biz["pay_type"] != "02" || biz["channel_code"] != "02" {
			t.Errorf("pay_type/channel_code = %q/%q", biz["pay_type"], biz["channel_code"])
		}
		if biz["profit_sharing"] != "1" {
			t.Errorf("profit_sharing = %q, want 1", biz["profit_sharing"])
		}
		return "000000", map[string]any{"op_ret_code": "000", "order_id": "GOMEORD2"}, 0
	})
	defer srv.Close()
	p := yinyingtongBuildType(t, "wxpay")
	yinyingtongBind(p, srv)

	req := yinyingtongCreateReq("h5")
	req.ProfitSharing = true
	res, e := p.Create(yinyingtongPinnedCtx(t), req)
	if e != nil {
		t.Fatalf("create: %v", e)
	}
	if res.MaterialType != "jump" || !strings.Contains(res.Material, "pages/preOrder/wxPublicOrder?orderId=GOMEORD2") {
		t.Fatalf("result = %+v", res)
	}
}

func TestPluginYinyingtongCreateUpstreamError(t *testing.T) {
	srv := yinyingtongServer(t, "SECRET", func(string, map[string]string) (string, map[string]any, int) {
		return "000000", map[string]any{"op_ret_code": "999", "op_ret_subcode": "MCH_FROZEN"}, 0
	})
	defer srv.Close()
	p := yinyingtongBuildType(t, "alipay")
	yinyingtongBind(p, srv)
	_, e := p.Create(yinyingtongPinnedCtx(t), yinyingtongCreateReq("native"))
	var perr *ProviderError
	if !errors.As(e, &perr) || perr.Code != "YYT_MCH_FROZEN" {
		t.Fatalf("expected ProviderError YYT_MCH_FROZEN, got %v", e)
	}
}

func TestPluginYinyingtongCreateTransportFailure(t *testing.T) {
	srv := yinyingtongServer(t, "SECRET", func(string, map[string]string) (string, map[string]any, int) {
		return "", nil, http.StatusInternalServerError
	})
	defer srv.Close()
	p := yinyingtongBuildType(t, "alipay")
	yinyingtongBind(p, srv)
	_, e := p.Create(yinyingtongPinnedCtx(t), yinyingtongCreateReq("native"))
	var unknown *UnknownOutcome
	if !errors.As(e, &unknown) {
		t.Fatalf("expected UnknownOutcome on 5xx, got %v", e)
	}
}

func TestPluginYinyingtongCreateValidationRejects(t *testing.T) {
	p := yinyingtongBuildType(t, "alipay")
	// method not served by alipay (h5 is wxpay-only).
	if _, e := p.Create(context.Background(), yinyingtongCreateReq("h5")); !errors.Is(e, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for wrong method, got %v", e)
	}
}

// refundReplier builds a server that answers the pre-refund query with the
// given transcode, then the refund itself.
func yinyingtongRefundServer(t *testing.T, transcode, wantScene string) *httptest.Server {
	return yinyingtongServer(t, "SECRET", func(method string, biz map[string]string) (string, map[string]any, int) {
		switch method {
		case "gepos.order.query":
			inner := map[string]any{"op_ret_code": "000", "order_number": "T100", "order_id": "GID9", "orderstatus": "00", "amount": "12.30"}
			if transcode != "" {
				inner["transcode"] = transcode
			}
			return "000000", inner, 0
		case "gepos.refund":
			if wantScene != "" && biz["scene"] != wantScene {
				t.Errorf("refund scene = %q, want %q", biz["scene"], wantScene)
			}
			if biz["old_order_number"] != "T100" || biz["old_order_id"] != "GID9" || biz["order_number"] != "R1" || biz["amount"] != "5.00" {
				t.Errorf("refund biz = %v", biz)
			}
			return "000000", map[string]any{"op_ret_code": "000", "order_id": "RGID1"}, 0
		default:
			t.Errorf("unexpected method %s", method)
			return "900999", nil, 0
		}
	})
}

func TestPluginYinyingtongRefundT61(t *testing.T) {
	srv := yinyingtongRefundServer(t, "T61", "0615")
	defer srv.Close()
	p := yinyingtongBuildType(t, "alipay")
	yinyingtongBind(p, srv)
	res, e := p.Refund(yinyingtongPinnedCtx(t), RefundRequest{TradeNo: "T100", ProviderTradeNo: "GID9", RefundNo: "R1", TotalAmount: 1230, Amount: 500})
	if e != nil {
		t.Fatalf("refund: %v", e)
	}
	if res.State != "SUCCESS" || res.ProviderRefundNo != "RGID1" || res.Amount != 500 {
		t.Fatalf("result = %+v", res)
	}
}

func TestPluginYinyingtongRefundNonT61(t *testing.T) {
	srv := yinyingtongRefundServer(t, "T40", "0606")
	defer srv.Close()
	p := yinyingtongBuildType(t, "alipay")
	yinyingtongBind(p, srv)
	res, e := p.Refund(yinyingtongPinnedCtx(t), RefundRequest{TradeNo: "T100", ProviderTradeNo: "GID9", RefundNo: "R1", TotalAmount: 1230, Amount: 500})
	if e != nil {
		t.Fatalf("refund: %v", e)
	}
	if res.State != "SUCCESS" {
		t.Fatalf("result = %+v", res)
	}
}

func TestPluginYinyingtongRefundUnknownTranscode(t *testing.T) {
	// Query returns no transcode: the scene is unknown and refund must refuse
	// without calling gepos.refund.
	srv := yinyingtongServer(t, "SECRET", func(method string, biz map[string]string) (string, map[string]any, int) {
		if method == "gepos.refund" {
			t.Error("refund must not run with unknown transcode")
		}
		return "000000", map[string]any{"op_ret_code": "000", "order_number": "T100", "orderstatus": "00", "amount": "12.30"}, 0
	})
	defer srv.Close()
	p := yinyingtongBuildType(t, "alipay")
	yinyingtongBind(p, srv)
	_, e := p.Refund(yinyingtongPinnedCtx(t), RefundRequest{TradeNo: "T100", ProviderTradeNo: "GID9", RefundNo: "R1", TotalAmount: 1230, Amount: 500})
	var perr *ProviderError
	if !errors.As(e, &perr) || perr.Code != "YYT_UNKNOWN_TRANSCODE" {
		t.Fatalf("expected YYT_UNKNOWN_TRANSCODE, got %v", e)
	}
}
