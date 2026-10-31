package devdoc

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"goepay/internal/compat"
)

var sandboxTestRSA struct {
	sync.Once
	key *rsa.PrivateKey
}

func playgroundKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	sandboxTestRSA.Do(func() {
		var err error
		sandboxTestRSA.key, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
	})
	return sandboxTestRSA.key
}

func playgroundPayload(t *testing.T, slug string, extra map[string]string) []byte {
	t.Helper()
	p := compat.Params{"pid": "900001", "timestamp": strconv.FormatInt(time.Now().Unix(), 10), "sign_type": "RSA"}
	for k, v := range extra {
		p[k] = v
	}
	key := playgroundKey(t)
	sign, err := compat.SignRSA(p, key)
	if err != nil {
		t.Fatal(err)
	}
	p["sign"] = sign
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"endpoint": slug, "params": p, "public_key": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func playgroundRequest(t *testing.T, raw []byte, origin string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "https://docs.example.com/doc/playground/run", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	if !SandboxServe(w, r) {
		t.Fatal("sandbox endpoint not handled")
	}
	return w
}

func playgroundVerified(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("sandbox HTTP %d: %s", w.Code, w.Body.String())
	}
	var envelope map[string]any
	d := json.NewDecoder(w.Body)
	d.UseNumber()
	if err := d.Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["environment"] != "documentation-sandbox" {
		t.Fatal("missing isolated environment")
	}
	req, ok := envelope["request"].(map[string]any)
	if !ok || req["signature_verified"] != true {
		t.Fatal("request signature not verified")
	}
	response, ok := envelope["response"].(map[string]any)
	if !ok {
		t.Fatal("response missing")
	}
	public, err := compat.ParsePublicKey(envelope["platform_public_key"].(string))
	if err != nil {
		t.Fatal(err)
	}
	p := compat.Params{}
	for k, v := range response {
		switch s := v.(type) {
		case string:
			p[k] = s
		case json.Number:
			p[k] = s.String()
		case bool:
			if s {
				p[k] = "1"
			}
		case nil:
		default:
			continue
		}
	}
	if p["sign_type"] != "RSA" || !compat.VerifyRSAWithProfile(p, p["sign"], public, compat.ProfileSDKV2) || compat.ValidateTimestamp(p["timestamp"], time.Now(), 300*time.Second) != nil {
		t.Fatal("sandbox response RSA/freshness did not verify")
	}
	return envelope
}

func TestPlayground219EndpointCatalogIsBoundedAndComplete(t *testing.T) {
	specs := EndpointSpecs()
	if len(specs) != 11 {
		t.Fatalf("want 11 documented request endpoints, got %d", len(specs))
	}
	seen := map[string]bool{}
	for _, spec := range specs {
		if seen[spec.Slug] || spec.Path == "" || !strings.HasPrefix(spec.Path, "/api/") || EndpointForSlug(spec.Slug) == nil {
			t.Fatalf("invalid endpoint %q", spec.Slug)
		}
		seen[spec.Slug] = true
		for _, f := range spec.Fields {
			if f.Name == "pid" || f.Name == "sign" || f.Name == "timestamp" || f.Name == "sign_type" {
				t.Fatal("credentials must not be editable metadata fields")
			}
		}
	}
	if EndpointForSlug("https://attacker.example/") != nil || EndpointForSlug("/api/pay/refund") != nil || EndpointForSlug("pay_refund") == nil {
		t.Fatal("lookup must use documented slugs only")
	}
}

func TestPlayground219RealRequestAndResponseSignatures(t *testing.T) {
	envelope := playgroundVerified(t, playgroundRequest(t, playgroundPayload(t, "pay_query", map[string]string{"out_trade_no": "DOC_PAID", "param": "中文 & += 测试"}), "https://docs.example.com"))
	request := envelope["request"].(map[string]any)
	if request["path"] != "/api/pay/query" || !strings.Contains(request["canonical"].(string), "param=中文 & += 测试") {
		t.Fatal("actual unencoded canonical request missing")
	}
	response := envelope["response"].(map[string]any)
	if fmt.Sprint(response["status"]) != "1" || response["out_trade_no"] != "DOC_PAID" {
		t.Fatal("paid fixture mismatch")
	}
	if w := playgroundRequest(t, playgroundPayload(t, "pay_query", map[string]string{"out_trade_no": "DOC_UNKNOWN"}), "https://docs.example.com"); fmt.Sprint(playgroundVerified(t, w)["response"].(map[string]any)["code"]) != "-1" {
		t.Fatal("unknown fixture must be a signed business error")
	}
}

func TestPlayground219EveryEndpointUsesAnIsolatedDynamicFixture(t *testing.T) {
	for _, spec := range EndpointSpecs() {
		t.Run(spec.Slug, func(t *testing.T) {
			fields := map[string]string{}
			for _, f := range spec.Fields {
				if f.Example != "" {
					fields[f.Name] = f.Example
				}
			}
			env := playgroundVerified(t, playgroundRequest(t, playgroundPayload(t, spec.Slug, fields), "https://docs.example.com"))
			response := env["response"].(map[string]any)
			if fmt.Sprint(response["code"]) != "0" {
				t.Fatalf("default example failed: %v", response["msg"])
			}
			if env["request"].(map[string]any)["path"] != spec.Path {
				t.Fatal("request path mismatch")
			}
		})
	}
}

func TestPlayground219RejectsUntrustedAndAmbiguousRequests(t *testing.T) {
	valid := playgroundPayload(t, "pay_query", map[string]string{"out_trade_no": "DOC_PAID"})
	for _, origin := range []string{"", "null", "https://elsewhere.example", "http://docs.example.com", "https://docs.example.com/path", "https://docs.example.com:444"} {
		t.Run(origin, func(t *testing.T) {
			if w := playgroundRequest(t, valid, origin); w.Code != 403 {
				t.Errorf("origin %q HTTP %d", origin, w.Code)
			}
		})
	}
	for _, raw := range []string{`{"endpoint":"pay_query","endpoint":"pay_refund","params":{},"public_key":""}`, `{"endpoint":"pay_query","params":{"pid":"900001","pid":"1"},"public_key":""}`, `{"endpoint":"pay_query","params":{"x":[]},"public_key":""}`, `{"endpoint":"pay_query","params":{},"public_key":"","url":"https://example.com"}`, `{} {}`, `{"endpoint":"https://attacker.example","params":{},"public_key":""}`, `{"endpoint":"pay_query","params":{"x":["a","b"]},"public_key":""}`} {
		t.Run(raw, func(t *testing.T) {
			if w := playgroundRequest(t, []byte(raw), "https://docs.example.com"); w.Code != 400 {
				t.Errorf("ambiguous request HTTP %d", w.Code)
			}
		})
	}
	var payload map[string]any
	if err := json.Unmarshal(valid, &payload); err != nil {
		t.Fatal(err)
	}
	params := payload["params"].(map[string]any)
	params["out_trade_no"] = "DOC_PENDING"
	bad, _ := json.Marshal(payload)
	if w := playgroundRequest(t, bad, "https://docs.example.com"); w.Code != 400 {
		t.Fatal("tampered signature accepted")
	}
	if w := playgroundRequest(t, playgroundPayload(t, "pay_query", map[string]string{"out_trade_no": "DOC_PAID", "timestamp": "1"}), "https://docs.example.com"); w.Code != 400 {
		t.Fatal("expired request accepted")
	}
	if w := playgroundRequest(t, bytes.Repeat([]byte("x"), 32769), "https://docs.example.com"); w.Code != 413 {
		t.Errorf("oversize request HTTP %d", w.Code)
	}
}

func TestPlayground219SignedValidationErrorsAndNoRealMerchant(t *testing.T) {
	for _, tc := range []struct {
		slug   string
		fields map[string]string
	}{{"pay_create", map[string]string{"out_trade_no": "DOC_NEW", "type": "alipay", "money": "0.001"}}, {"pay_refund", map[string]string{"out_trade_no": "DOC_PAID", "out_refund_no": "DOC_REFUND", "money": "2.00"}}, {"transfer_submit", map[string]string{"out_biz_no": "DOC_TRANSFER", "type": "alipay", "account": "demo@example.com", "name": "测试收款人", "money": "101.00"}}, {"pay_query", map[string]string{"pid": "1", "out_trade_no": "DOC_PAID"}}} {
		t.Run(tc.slug, func(t *testing.T) {
			env := playgroundVerified(t, playgroundRequest(t, playgroundPayload(t, tc.slug, tc.fields), "https://docs.example.com"))
			if fmt.Sprint(env["response"].(map[string]any)["code"]) != "-1" {
				t.Fatal("invalid business parameters accepted")
			}
		})
	}
	r := httptest.NewRequest(http.MethodGet, "https://docs.example.com/doc/playground/run", nil)
	w := httptest.NewRecorder()
	SandboxServe(w, r)
	if w.Code != 405 || w.Header().Get("Allow") != "POST" {
		t.Fatal("method guard missing")
	}
	if SandboxServe(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/pay/create", nil)) {
		t.Fatal("sandbox captured an actual payment endpoint")
	}
}

func TestPlayground219BoundsParametersAndConcurrentWork(t *testing.T) {
	for _, fields := range []map[string]string{
		{"out_trade_no": "DOC_PAID", strings.Repeat("k", 65): "value"},
		{"out_trade_no": "DOC_PAID", "param": strings.Repeat("x", 4097)},
	} {
		if w := playgroundRequest(t, playgroundPayload(t, "pay_query", fields), "https://docs.example.com"); w.Code != 400 {
			t.Errorf("oversized parameter accepted HTTP %d", w.Code)
		}
	}
	fields := map[string]string{"out_trade_no": "DOC_PAID"}
	for i := 0; i < 61; i++ {
		fields[fmt.Sprintf("extra_%d", i)] = "x"
	}
	if w := playgroundRequest(t, playgroundPayload(t, "pay_query", fields), "https://docs.example.com"); w.Code != 400 {
		t.Errorf("more than64 params accepted HTTP %d", w.Code)
	}
}

func TestPlayground219ConcurrentLimitAndHTTPBoundary(t *testing.T) {
	raw := playgroundPayload(t, "pay_query", map[string]string{"out_trade_no": "DOC_PAID"})
	for i := 0; i < cap(playgroundSlots); i++ {
		playgroundSlots <- struct{}{}
	}
	w := playgroundRequest(t, raw, "https://docs.example.com")
	for i := 0; i < cap(playgroundSlots); i++ {
		<-playgroundSlots
	}
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatal("busy sandbox must fail closed with retry timing")
	}
	for _, host := range []string{"127.0.0.1:8189", "[::1]:8189"} {
		r := httptest.NewRequest("POST", "http://"+host+playgroundPath, bytes.NewReader(raw))
		r.Header.Set("Origin", "http://"+host)
		r.Header.Set("Content-Type", "application/json; charset=utf-8")
		w = httptest.NewRecorder()
		SandboxServe(w, r)
		playgroundVerified(t, w)
	}
	r := httptest.NewRequest("POST", "https://docs.example.com"+playgroundPath, bytes.NewReader(raw))
	r.Header.Set("Origin", "https://docs.example.com")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	SandboxServe(w, r)
	if w.Code != 415 {
		t.Fatal("wrong content-type accepted")
	}
	r = httptest.NewRequest("POST", "https://docs.example.com"+playgroundPath, bytes.NewReader(raw))
	r.Header.Add("Origin", "https://docs.example.com")
	r.Header.Add("Origin", "https://docs.example.com")
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	SandboxServe(w, r)
	if w.Code != 403 {
		t.Fatal("ambiguous origins accepted")
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("sandbox error responses must retain private transport headers")
	}
}

func TestPlayground219CatalogCopiesAndStatelessFixtures(t *testing.T) {
	one := EndpointForSlug("pay_create")
	one.Path = "https://elsewhere.example"
	one.Fields[0].Name = "sign"
	one.Fields[3].Options[0] = "other"
	next := EndpointForSlug("pay_create")
	if next.Path != "/api/pay/create" || next.Fields[0].Name != "out_trade_no" || next.Fields[3].Options[0] != "alipay" {
		t.Fatal("catalog must not be mutable through returned slices")
	}
	created := playgroundVerified(t, playgroundRequest(t, playgroundPayload(t, "pay_create", map[string]string{"out_trade_no": "DOC_CREATED_LOCAL", "type": "alipay", "money": "1.23", "name": "示例"}), "https://docs.example.com"))["response"].(map[string]any)
	queried := playgroundVerified(t, playgroundRequest(t, playgroundPayload(t, "pay_query", map[string]string{"trade_no": created["trade_no"].(string)}), "https://docs.example.com"))["response"].(map[string]any)
	if fmt.Sprint(queried["code"]) != "-1" {
		t.Fatal("sandbox demonstration unexpectedly persisted an order")
	}
	closed := playgroundVerified(t, playgroundRequest(t, playgroundPayload(t, "pay_close", map[string]string{"out_trade_no": "DOC_PENDING"}), "https://docs.example.com"))["response"].(map[string]any)
	if fmt.Sprint(closed["code"]) != "0" {
		t.Fatal("close demonstration failed")
	}
	queried = playgroundVerified(t, playgroundRequest(t, playgroundPayload(t, "pay_query", map[string]string{"out_trade_no": "DOC_PENDING"}), "https://docs.example.com"))["response"].(map[string]any)
	if fmt.Sprint(queried["status"]) != "0" {
		t.Fatal("sandbox close altered a shared fixture")
	}
}

func TestPlayground219ConditionalFieldsFollowInterfaceContracts(t *testing.T) {
	for _, tc := range []struct{ slug, name string }{{"pay_submit", "type"}, {"transfer_submit", "out_biz_no"}, {"transfer_submit", "name"}} {
		for _, f := range EndpointForSlug(tc.slug).Fields {
			if f.Name == tc.name && f.Required {
				t.Errorf("%s %s is conditional or recommended, not universally required", tc.slug, tc.name)
			}
		}
	}
	for _, tc := range []struct {
		slug   string
		fields map[string]string
	}{{"pay_submit", map[string]string{"out_trade_no": "DOC_CASHIER", "money": "1.00"}}, {"transfer_submit", map[string]string{"type": "alipay", "account": "sandbox@example.com", "money": "1.00"}}} {
		env := playgroundVerified(t, playgroundRequest(t, playgroundPayload(t, tc.slug, tc.fields), "https://docs.example.com"))
		if fmt.Sprint(env["response"].(map[string]any)["code"]) != "0" {
			t.Errorf("valid optional fields rejected in %s", tc.slug)
		}
	}
}

// Match provider_http.go's V2 pay/create projection. This endpoint returns
// trade_no, pay_type and pay_info; request identities and QR material are not
// exposed under alternate top-level names.
func TestPlayground219CreateProjectionMatchesPaymentHandler(t *testing.T) {
	envelope := playgroundVerified(t, playgroundRequest(t, playgroundPayload(t, "pay_create", map[string]string{"out_trade_no": "DOC_PROJECTION", "type": "alipay", "money": "1.00"}), "https://docs.example.com"))
	response := envelope["response"].(map[string]any)
	allowed := map[string]bool{"code": true, "trade_no": true, "pay_type": true, "pay_info": true, "timestamp": true, "sign_type": true, "sign": true}
	for key := range response {
		if !allowed[key] {
			t.Errorf("pay_create exposes non-handler field %s", key)
		}
	}
	if response["pay_type"] != "qrcode" || !strings.HasPrefix(fmt.Sprint(response["pay_info"]), "DOCUMENTATION_SANDBOX_ONLY:") {
		t.Error("QR material must be an unpayable marker in pay_info")
	}
	var sample map[string]any
	if err := json.Unmarshal([]byte(EndpointForSlug("pay_create").ExampleResponse), &sample); err != nil {
		t.Fatal(err)
	}
	for key := range sample {
		if !allowed[key] {
			t.Errorf("pay_create example exposes non-handler field %s", key)
		}
	}
	if sample["pay_info"] == nil {
		t.Error("pay_create example must use pay_info")
	}
}

// Match merchant_api.go's list-row projection, which intentionally differs from
// the richer pay/query projection. Its array is not signed by the scalar RSA.
func TestPlayground219ListProjectionMatchesMerchantHandler(t *testing.T) {
	allowed := map[string]bool{"trade_no": true, "out_trade_no": true, "type": true, "pid": true, "addtime": true, "endtime": true, "name": true, "money": true, "status": true, "param": true, "buyer": true, "api_trade_no": true, "refundmoney": true}
	envelope := playgroundVerified(t, playgroundRequest(t, playgroundPayload(t, "merchant_orders", nil), "https://docs.example.com"))
	rows := envelope["response"].(map[string]any)["data"].([]any)
	if len(rows) != 5 {
		t.Fatal("expected all fixed sample orders")
	}
	for _, value := range rows {
		row := value.(map[string]any)
		for key := range row {
			if !allowed[key] {
				t.Errorf("merchant_orders leaks query-only field %s", key)
			}
		}
	}
	var sample map[string]any
	if err := json.Unmarshal([]byte(EndpointForSlug("merchant_orders").ExampleResponse), &sample); err != nil {
		t.Fatal(err)
	}
	for _, value := range sample["data"].([]any) {
		for key := range value.(map[string]any) {
			if !allowed[key] {
				t.Errorf("list example exposes non-handler field %s", key)
			}
		}
	}
	query := playgroundVerified(t, playgroundRequest(t, playgroundPayload(t, "pay_query", map[string]string{"out_trade_no": "DOC_AUTH"}), "https://docs.example.com"))["response"].(map[string]any)
	if _, ok := query["clientip"]; !ok {
		t.Error("pay_query lost its valid clientip field")
	}
	if _, ok := query["authorization_release_state"]; !ok {
		t.Error("pay_query lost its authorization field")
	}
}

// Match payment/provider_transfers.go validTransferRequest: length is measured
// on raw account/name bytes, and recipient text cannot contain NUL/CR/LF.
func TestPlayground219TransferTextBoundariesMatchValidator(t *testing.T) {
	base := func() map[string]string {
		return map[string]string{"type": "alipay", "account": "sandbox@example.com", "name": "文档收款人", "money": "1.00", "remark": "文档沙箱"}
	}
	for _, tc := range []struct {
		name, field, value string
		want               string
	}{
		{"account128", "account", strings.Repeat("a", 128), "0"},
		{"account128plusSpace", "account", strings.Repeat("a", 128) + " ", "-1"},
		{"account128WithSpaces", "account", " " + strings.Repeat("a", 126) + " ", "0"},
		{"accountMultibyte", "account", strings.Repeat("界", 43), "-1"},
		{"name128", "name", strings.Repeat("a", 128), "0"},
		{"name129", "name", strings.Repeat("a", 129), "-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := base()
			fields[tc.field] = tc.value
			response := playgroundVerified(t, playgroundRequest(t, playgroundPayload(t, "transfer_submit", fields), "https://docs.example.com"))["response"].(map[string]any)
			if fmt.Sprint(response["code"]) != tc.want {
				t.Errorf("raw recipient boundary want %s, got %v", tc.want, response["code"])
			}
		})
	}
	for _, field := range []string{"account", "name", "remark"} {
		for _, control := range []string{"\x00", "\r", "\n"} {
			t.Run(field+strconv.Itoa(int(control[0])), func(t *testing.T) {
				fields := base()
				fields[field] = "demo" + control + "x"
				response := playgroundVerified(t, playgroundRequest(t, playgroundPayload(t, "transfer_submit", fields), "https://docs.example.com"))["response"].(map[string]any)
				if fmt.Sprint(response["code"]) != "-1" {
					t.Errorf("control character accepted in %s", field)
				}
			})
		}
	}
}
