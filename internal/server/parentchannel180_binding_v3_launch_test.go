package server

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"
	"time"
)

func binding180PrepareV3Launch(t *testing.T, setup parent180Runtime, local *binding180TLSFactory, state map[string]any) (map[string]any, map[string]any, map[string]any) {
	t.Helper()
	merchantA, _ := rsa.GenerateKey(rand.Reader, 2048)
	merchantB, _ := rsa.GenerateKey(rand.Reader, 2048)
	platformA, _ := rsa.GenerateKey(rand.Reader, 2048)
	platformB, _ := rsa.GenerateKey(rand.Reader, 2048)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			setup.bad.Add(1)
			w.WriteHeader(400)
			return
		}
		var body struct {
			AppID    string `json:"appid"`
			Merchant string `json:"mchid"`
			Trade    string `json:"out_trade_no"`
			Payer    struct {
				OpenID string `json:"openid"`
			} `json:"payer"`
		}
		if json.Unmarshal(raw, &body) != nil || r.Method != "POST" || r.URL.Path != "/v3/pay/transactions/jsapi" || body.AppID != "synthetic-mini-A" || body.Merchant != "synthetic180-fresh-mch" || body.Payer.OpenID != "synthetic-launch-openid-A" {
			setup.bad.Add(1)
			w.WriteHeader(400)
			return
		}
		fields := map[string]string{}
		for _, m := range regexp.MustCompile(`([a-z_]+)="([^"]*)"`).FindAllStringSubmatch(r.Header.Get("Authorization"), -1) {
			fields[m[1]] = m[2]
		}
		signature, _ := base64.StdEncoding.DecodeString(fields["signature"])
		canonical := r.Method + "\n" + r.URL.RequestURI() + "\n" + fields["timestamp"] + "\n" + fields["nonce_str"] + "\n" + string(raw) + "\n"
		digest := sha256.Sum256([]byte(canonical))
		if rsa.VerifyPKCS1v15(&merchantA.PublicKey, crypto.SHA256, digest[:], signature) != nil || fields["serial_no"] != "BINDING180_A" {
			setup.bad.Add(1)
			w.WriteHeader(403)
			return
		}
		setup.creates.Add(1)
		reply := `{"prepay_id":"synthetic-v3-binding-prepay"}`
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := "binding180-v3-launch"
		w.Header().Set("Wechatpay-Timestamp", stamp)
		w.Header().Set("Wechatpay-Nonce", nonce)
		w.Header().Set("Wechatpay-Serial", "PUB_BIND180")
		w.Header().Set("Wechatpay-Signature", parent180ResponseSignature(platformA, []byte(stamp+"\n"+nonce+"\n"+reply+"\n")))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, reply)
	}))
	t.Cleanup(upstream.Close)
	local.localProvider = func(c channelconfig.Channel) (adapter.Provider, error) {
		if c.Config["wechat_api_version"] != "v3" {
			legacy := *local.parent180TLSFactory
			legacy.localProvider = nil
			raw, err := json.Marshal(struct {
				Version int                   `json:"version"`
				Channel channelconfig.Channel `json:"channel"`
			}{1, c})
			if err != nil {
				return nil, err
			}
			execution, err := legacy.local(raw)
			return execution.Payment, err
		}
		private, err := compat.ParsePrivateKey(c.Certificates["v3_merchant_key"])
		if err != nil {
			return nil, err
		}
		public, err := compat.ParsePublicKey(c.Certificates["v3_verifier_PUB_BIND180"])
		if err != nil {
			return nil, err
		}
		var mini *adapter.MiniProgramConfig
		if c.Config["enable_mini_launch"] == "true" {
			mini = &adapter.MiniProgramConfig{AppID: c.Config["mini_appid"], Path: c.Config["mini_path"]}
		}
		return adapter.NewWechatV3(adapter.WechatV3Config{AppID: c.Config["appid"], MerchantID: c.Config["appmchid"], MerchantSerial: c.Config["v3_merchant_serial"], PrivateKey: private, APIv3Key: []byte(c.Config["v3_api_key"]), VerificationKeys: map[string]*rsa.PublicKey{"PUB_BIND180": public}, Methods: []string{"native", "jsapi"}, MiniProgram: mini, HTTPClient: &http.Client{Transport: parent180RewriteTransport{upstream}}})
	}
	replace := func(v string) map[string]string { return map[string]string{"operation": "replace", "value": v} }
	config := map[string]any{"wechat_api_version": replace("v3"), "v3_api_key": replace("0123456789abcdef0123456789abcdef"), "v3_merchant_serial": replace("BINDING180_A")}
	certs := map[string]any{"v3_merchant_key": replace(parent180Private(merchantA)), "v3_verifier_PUB_BIND180": replace(parent180Public(platformA))}
	patch, _ := json.Marshal(map[string]any{"expected_fingerprint": state["fingerprint"], "account": map[string]any{"config": config, "certificates": certs}})
	w := parent180HTTP(setup.f, "PATCH", "/"+setup.id, string(patch))
	if w.Code != 200 {
		t.Fatal("V3 account construction failed", w.Code, w.Body.String())
	}
	var result struct{ Data map[string]any }
	if json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal("V3 parent response invalid")
	}
	return result.Data, map[string]any{"v3_api_key": replace("fedcba9876543210fedcba9876543210"), "v3_merchant_serial": replace("BINDING180_B")}, map[string]any{"v3_merchant_key": replace(parent180Private(merchantB)), "v3_verifier_PUB_BIND180": replace(parent180Public(platformB))}
}
