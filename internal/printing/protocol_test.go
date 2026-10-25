package printing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPrintingExcelsecuLocalTLSWireSignatureAndAcceptedSemantics(t *testing.T) {
	c := PrintCommand{RequestNo: "PJ012345678901234567890123456789", Device: "LOCAL-DEVICE", Copies: 2, Voice: true, Credential: Credential{"LOCAL-APP", "local-synthetic-secret"}, Receipt: Receipt{TradeNo: "1001", MerchantName: "Local <cut/>", Name: "item", Amount: 123, Type: "wxpay", PaidAt: "2026-10-23T12:00:00Z", Remark: "<cut/>"}}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi/v1/iot/printer/print" || r.Method != "POST" {
			t.Error("wrong printer path/method")
		}
		b, _ := io.ReadAll(io.LimitReader(r.Body, 8192))
		var req struct {
			AppID     string `json:"appid"`
			ReqID     string `json:"reqid"`
			Timestamp int64  `json:"timestamp"`
			Sign      string `json:"sign"`
			Biztype   int    `json:"biztype"`
			Biz       string `json:"biz"`
		}
		if e := json.Unmarshal(b, &req); e != nil {
			t.Error(e)
		}
		m := hmac.New(sha256.New, []byte(c.Credential.Secret))
		m.Write([]byte(req.AppID + "1792756800000" + req.Biz + "3" + req.ReqID))
		if req.Timestamp != 1792756800000 || req.ReqID != c.RequestNo || req.Biztype != 3 || !hmac.Equal([]byte(req.Sign), []byte(base64.StdEncoding.EncodeToString(m.Sum(nil)))) {
			t.Error("wire signature or original identity changed")
		}
		var biz struct {
			SN, Content, TTS string
			Count            int
		}
		if e := json.Unmarshal([]byte(req.Biz), &biz); e != nil {
			t.Error(e)
		}
		xml, _ := base64.StdEncoding.DecodeString(biz.Content)
		if biz.SN != c.Device || biz.Count != 2 || !strings.Contains(string(xml), "&lt;cut/&gt;") || !strings.Contains(biz.TTS, "1.23") {
			t.Error("receipt/copies/voice payload wrong")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"accepted":true}}`))
	}))
	defer server.Close()
	raw, e := BuildExcelsecuRequest(c, time.UnixMilli(1792756800000))
	if e != nil {
		t.Fatal(e)
	}
	req, e := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/openapi/v1/iot/printer/print", strings.NewReader(string(raw)))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := server.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	out, e := ParseExcelsecuResponse(resp.StatusCode, b)
	if e != nil || out.State != "accepted" {
		t.Fatal("gateway acceptance must not mean physical completion", out, e)
	}
	for _, bad := range []string{`{"code":0,"code":1}`, `{"code":"0"}`, `{"data":{}}`, `not json`} {
		if _, e = ParseExcelsecuResponse(200, []byte(bad)); e == nil {
			t.Fatal("ambiguous gateway response accepted", bad)
		}
	}
}
