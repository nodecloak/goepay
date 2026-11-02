package kyc233

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	domain "goepay/internal/kyc233"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
)

type tlsPeer struct {
	t    *testing.T
	peer *httptest.Server
}

func (p tlsPeer) RoundTrip(r *http.Request) (*http.Response, error) {
	p.t.Helper()
	if r.URL.Scheme != "https" || r.URL.User != nil || r.URL.Port() != "" {
		p.t.Fatal("request escaped strict provider HTTPS")
	}
	switch r.URL.Host {
	case "phone3.market.alicloudapi.com", "companythree.shumaidata.com", "saf.cn-shanghai.aliyuncs.com", "ocr-api.cn-hangzhou.aliyuncs.com", "faceid.tencentcloudapi.com", "aip.baidubce.com":
	default:
		p.t.Fatal("unexpected provider host", r.URL.Host)
	}
	clone := r.Clone(r.Context())
	target, _ := url.Parse(p.peer.URL)
	u := *r.URL
	u.Host = target.Host
	clone.URL = &u
	return p.peer.Client().Transport.RoundTrip(clone)
}
func peerAdapter(t *testing.T, handler http.HandlerFunc) *Adapter {
	t.Helper()
	s := httptest.NewTLSServer(handler)
	t.Cleanup(s.Close)
	return New(&http.Client{Transport: tlsPeer{t, s}})
}
func submission() domain.Submission {
	return domain.Submission{CertType: 0, CardType: 0, Name: "Synthetic Person", Number: "110101199001010015", Phone: "13800000000", Consent: true}
}
func attempt() domain.Attempt {
	return domain.Attempt{ID: strings.Repeat("a", 32), State: strings.Repeat("b", 64), CallbackURL: "https://pay.example.invalid/merchant/account/certificate?attempt_id=" + strings.Repeat("a", 32) + "&state=" + strings.Repeat("b", 64)}
}

// These assertions fail if the adapter accepts an HTTP/redirect/error as proof,
// discloses upstream identities, or silently repeats a billable operation.
func TestKYC233PhoneCompanyPrecheckAndRedaction(t *testing.T) {
	calls := 0
	bad := false
	p := peerAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "APPCODE synthetic-code" {
			t.Error("missing appcode")
		}
		switch r.URL.Path {
		case "/companythree/check":
			if r.URL.Query().Get("companyName") != "Synthetic Company" || r.URL.Query().Get("creditNo") != "synthetic-credit" || r.URL.Query().Get("legalPerson") != "Synthetic Person" {
				t.Error("company identity mismatch")
			}
			io.WriteString(w, `{"code":200,"data":{"result":0,"desc":"一致"}}`)
		case "/phonethree":
			if r.URL.Query().Get("phone") != "13800000000" || r.URL.Query().Get("realname") != "Synthetic Person" {
				t.Error("phone identity mismatch")
			}
			if bad {
				w.WriteHeader(503)
				io.WriteString(w, "synthetic-code Synthetic Person 110101199001010015")
			} else {
				io.WriteString(w, `{"code":200,"msg":"一致"}`)
			}
		default:
			t.Error("wrong path")
		}
	})
	snap := domain.Snapshot{Mode: 2, Corporate: true, Credentials: domain.Credentials{AppCode: "synthetic-code", CompanyAppCode: "synthetic-code"}}
	s := submission()
	s.CertType = 1
	s.CompanyName = "Synthetic Company"
	s.CompanyNumber = "synthetic-credit"
	got, e := p.Initialize(context.Background(), snap, s, attempt())
	if e != nil || got.Status != "passed" || calls != 2 {
		t.Fatal(got, e, calls)
	}
	bad = true
	s.CertType = 0
	_, e = p.Initialize(context.Background(), snap, s, attempt())
	if e == nil || strings.Contains(e.Error(), "synthetic-code") || strings.Contains(e.Error(), "Synthetic Person") {
		t.Fatal("identity leaked or invalid proof accepted", e)
	}
	if calls != 3 {
		t.Fatal("unexpected retry", calls)
	}
}

func TestKYC233AliyunSignedInitializeQueryAndURLGuard(t *testing.T) {
	pending := true
	evil := false
	p := peerAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		verifyRPC233(t, r.Form, "key")
		if r.Form.Get("Action") != "ExecuteRequest" || r.Form.Get("Service") != "fin_face_verify" || r.Form.Get("Version") != "2017-03-31" || r.Form.Get("Signature") == "" {
			t.Error("missing RPC protocol fields")
		}
		var s map[string]string
		json.Unmarshal([]byte(r.Form.Get("ServiceParameters")), &s)
		if s["method"] == "init" {
			if s["outerOrderNo"] != strings.Repeat("a", 32) || s["certName"] != "Synthetic Person" || s["sceneId"] != "scene" || s["bizCode"] != "FACE" {
				t.Error("wrong init")
			}
			u := "https://picker.antcloudauth.aliyuncs.com/gateway.do?token=opaque"
			if evil {
				u = "https://picker.antcloudauth.aliyuncs.com.evil.invalid/steal"
			}
			json.NewEncoder(w).Encode(map[string]any{"Code": 200, "Data": map[string]string{"certifyId": "opaque", "certifyUrl": u}})
		} else {
			if s["certifyId"] != "opaque" {
				t.Error("wrong query token")
			}
			v := "T"
			if pending {
				v = "F"
			}
			json.NewEncoder(w).Encode(map[string]any{"Code": 200, "Data": map[string]string{"passed": v}})
		}
	})
	snap := domain.Snapshot{Mode: 5, Credentials: domain.Credentials{AliyunID: "id", AliyunKey: "key", AliyunScene: "scene"}}
	got, e := p.Initialize(context.Background(), snap, submission(), attempt())
	if e != nil || got.Status != "pending" || got.Token != "opaque" {
		t.Fatal(got, e)
	}
	a := attempt()
	a.Token = got.Token
	q, e := p.Query(context.Background(), snap, a)
	if e != nil || q.Status != "pending" {
		t.Fatal(q, e)
	}
	pending = false
	q, e = p.Query(context.Background(), snap, a)
	if e != nil || q.Status != "passed" {
		t.Fatal(q, e)
	}
	evil = true
	if _, e = p.Initialize(context.Background(), snap, submission(), attempt()); e == nil {
		t.Fatal("lookalike provider redirect accepted")
	}
}

func TestKYC233TencentRequiresExplicitResultAndTC3(t *testing.T) {
	response := `{"Response":{"Text":null,"RequestId":"synthetic"}}`
	p := peerAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "TC3-HMAC-SHA256 Credential=id/") || r.Header.Get("X-TC-Version") != "2018-03-01" {
			t.Error("missing TC3")
		}
		raw, _ := io.ReadAll(r.Body)
		verifyTC3233(t, r, raw, "key")
		var body map[string]string
		json.Unmarshal(raw, &body)
		if r.Header.Get("X-TC-Action") == "DetectAuth" {
			if body["RuleId"] != "rule" || body["IdCard"] != "110101199001010015" {
				t.Error("wrong DetectAuth")
			}
			io.WriteString(w, `{"Response":{"BizToken":"token","Url":"https://faceid.qq.com/?token=token","RequestId":"synthetic"}}`)
		} else {
			if body["InfoType"] != "1" || body["BizToken"] != "token" {
				t.Error("wrong query")
			}
			io.WriteString(w, response)
		}
	})
	snap := domain.Snapshot{Mode: 4, Credentials: domain.Credentials{TencentID: "id", TencentKey: "key", TencentRuleID: "rule"}}
	got, e := p.Initialize(context.Background(), snap, submission(), attempt())
	if e != nil || got.Token == "token" || got.Token == "" || got.Scan != "wechat" {
		t.Fatal(got, e)
	}
	a := attempt()
	a.Token = got.Token
	for _, tc := range []struct{ body, want string }{{response, "pending"}, {`{"Response":{"Text":{"ErrCode":0,"Name":"Synthetic Person","IdCard":"110101199001010015"},"RequestId":"s"}}`, "passed"}, {`{"Response":{"Text":{"ErrCode":-1},"RequestId":"s"}}`, "rejected"}} {
		response = tc.body
		q, e := p.Query(context.Background(), snap, a)
		if e != nil || q.Status != tc.want {
			t.Fatal(q, e, tc.want)
		}
	}
	response = `{"Response":{"Text":{},"RequestId":"s"}}`
	if q, e := p.Query(context.Background(), snap, a); e == nil && q.Status == "passed" {
		t.Fatal("missing ErrCode accepted")
	}
	response = `{"Response":{"Text":{"ErrCode":0,"Name":"Different Person","IdCard":"110101199001010015"},"RequestId":"s"}}`
	if q, e := p.Query(context.Background(), snap, a); e == nil && q.Status == "passed" {
		t.Fatal("identity changed during provider flow")
	}
}

func TestOCR233BothProvidersSixDocumentNormalization(t *testing.T) {
	types := []struct{ kind, action, path, raw, expected string }{
		{"idcard", "RecognizeIdcard", "idcard", `{"idNumber":"synthetic-id","name":"Synthetic","address":"Address","sex":"男","ethnicity":"汉","birthDate":"2000年01月02日"}`, "id_no"},
		{"idcard_back", "RecognizeIdcard", "idcard", `{"issueAuthority":"Authority","validPeriod":"2020.01.01-长期"}`, "issue_authority"},
		{"passport", "RecognizePassport", "overseas_passport", `{"passportNumber":"Psynthetic","name":"Synthetic","surname":"Last","givenName":"First","country":"CHN","validToDate":"20300101"}`, "passport_no"},
		{"bank_card", "RecognizeBankCard", "bankcard", `{"bankName":"Bank","cardNumber":"1234 5678","cardType":"DC","validToDate":"2030/01"}`, "card_no"},
		{"business_license", "RecognizeBusinessLicense", "business_license", `{"creditCode":"credit","companyName":"Company","legalPerson":"Synthetic","validFromDate":"20200101","validToDate":""}`, "license_no"},
		{"bank_account_license", "RecognizeBankAccountLicense", "account_opening", `{"bankAccount":"123456","legalRepresentative":"Synthetic","customerName":"Company"}`, "bank_account"},
	}
	for _, tc := range types {
		t.Run(tc.kind, func(t *testing.T) {
			for _, provider := range []string{"aliyun", "baidu"} {
				t.Run(provider, func(t *testing.T) {
					p := peerAdapter(t, func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path == "/oauth/2.0/token" {
							r.ParseForm()
							if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != "id" {
								t.Error("wrong token request")
							}
							io.WriteString(w, `{"access_token":"token","expires_in":3600}`)
							return
						}
						if provider == "aliyun" {
							verifyRPC233(t, r.URL.Query(), "key")
							if r.URL.Query().Get("Action") != tc.action {
								t.Error("wrong OCR action")
							}
							raw, _ := io.ReadAll(r.Body)
							if string(raw) != "synthetic-image" {
								t.Error("not uploaded bytes")
							}
							data := `{"data":` + tc.raw + `}`
							if tc.kind == "idcard" {
								data = `{"data":{"face":{"data":` + tc.raw + `}}}`
							} else if tc.kind == "idcard_back" {
								data = `{"data":{"back":{"data":` + tc.raw + `}}}`
							}
							json.NewEncoder(w).Encode(map[string]string{"Data": data})
						} else {
							r.ParseForm()
							if r.URL.Path != "/rest/2.0/ocr/v1/"+tc.path || r.Form.Get("image") != "c3ludGhldGljLWltYWdl" {
								t.Error("wrong Baidu document upload")
							}
							switch tc.kind {
							case "idcard":
								io.WriteString(w, `{"image_status":"normal","words_result":{"公民身份号码":{"words":"synthetic-id"},"姓名":{"words":"Synthetic"}}}`)
							case "idcard_back":
								io.WriteString(w, `{"image_status":"normal","words_result":{"签发机关":{"words":"Authority"},"失效日期":{"words":"长期"}}}`)
							case "passport":
								io.WriteString(w, `{"words_result":{"护照号":{"words":"Psynthetic"},"姓名拼音":{"words":"Synthetic"}}}`)
							case "bank_card":
								io.WriteString(w, `{"result":{"bank_card_number":"1234 5678","bank_name":"Bank","bank_card_type":1}}`)
							case "business_license":
								io.WriteString(w, `{"words_result":{"社会信用代码":{"words":"credit"},"单位名称":{"words":"Company"},"有效期":{"words":"长期"}}}`)
							case "bank_account_license":
								io.WriteString(w, `{"words_result":{"账号":{"word":["123456"]},"公司名称":{"word":["Company"]}}}`)
							}
						}
					})
					snap := domain.Snapshot{OCRType: provider, Credentials: domain.Credentials{OCRAliyunID: "id", OCRAliyunKey: "key", OCRBaiduID: "id", OCRBaiduKey: "key"}}
					got, e := p.Recognize(context.Background(), snap, tc.kind, []byte("synthetic-image"))
					if e != nil || got.Fields[tc.expected] == "" {
						t.Fatal(got, e)
					}
					if got.Fields["card_no"] != "" && got.Fields["card_no"] != "12345678" {
						t.Fatal("card spaces preserved")
					}
					if got.Fields["period_end"] == "--" {
						t.Fatal("invalid long-term date")
					}
				})
			}
		})
	}
}

func TestKYC233RefusesRedirectOversizedDuplicateJSONAndTransportLeaks(t *testing.T) {
	for _, body := range []string{strings.Repeat("x", (1<<20)+1), `{"code":200,"code":500}`, `{"code":200} trailing`} {
		p := peerAdapter(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		_, e := p.Initialize(context.Background(), domain.Snapshot{Mode: 2, Credentials: domain.Credentials{AppCode: "code"}}, submission(), attempt())
		if e == nil {
			t.Fatal("malformed proof accepted")
		}
	}
	calls := 0
	p := peerAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Location", "https://evil.invalid/")
		w.WriteHeader(307)
	})
	_, e := p.Initialize(context.Background(), domain.Snapshot{Mode: 2, Credentials: domain.Credentials{AppCode: "code"}}, submission(), attempt())
	if e == nil || calls != 1 {
		t.Fatal(e, calls)
	}
	p = New(&http.Client{Transport: errorTransport{}})
	_, e = p.Initialize(context.Background(), domain.Snapshot{Mode: 2, Credentials: domain.Credentials{AppCode: "code"}}, submission(), attempt())
	if e == nil || strings.Contains(e.Error(), "sensitive") {
		t.Fatal(e)
	}
}

type errorTransport struct{}

func (errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("sensitive-PII")
}

func verifyRPC233(t *testing.T, q url.Values, key string) {
	t.Helper()
	keys := []string{}
	for k := range q {
		if k != "Signature" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	escape := func(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
	parts := []string{}
	for _, k := range keys {
		parts = append(parts, escape(k)+"="+escape(q.Get(k)))
	}
	mac := hmac.New(sha1.New, []byte(key+"&"))
	mac.Write([]byte("POST&%2F&" + escape(strings.Join(parts, "&"))))
	if q.Get("Signature") != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
		t.Error("RPC request signature does not bind exact action and identity")
	}
}
func verifyTC3233(t *testing.T, r *http.Request, raw []byte, key string) {
	t.Helper()
	auth := r.Header.Get("Authorization")
	scope := strings.Split(strings.Split(auth, "Credential=id/")[1], ",")[0]
	day := strings.Split(scope, "/")[0]
	digest := func(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
	mac := func(k []byte, s string) []byte { h := hmac.New(sha256.New, k); h.Write([]byte(s)); return h.Sum(nil) }
	canonical := "POST\n/\n\ncontent-type:application/json; charset=utf-8\nhost:faceid.tencentcloudapi.com\n\ncontent-type;host\n" + digest(string(raw))
	toSign := "TC3-HMAC-SHA256\n" + r.Header.Get("X-TC-Timestamp") + "\n" + scope + "\n" + digest(canonical)
	signing := mac(mac(mac([]byte("TC3"+key), day), "faceid"), "tc3_request")
	want := hex.EncodeToString(mac(signing, toSign))
	if !strings.HasSuffix(auth, "Signature="+want) {
		t.Error("TC3 signature does not bind exact identity and query")
	}
}

func TestKYC233NoIOForInvalidInputAndNoTLSProof(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: countTransport{&calls}}
	p := New(client)
	if calls != 0 {
		t.Fatal("constructor performs I/O")
	}
	snap := domain.Snapshot{Mode: 2, Credentials: domain.Credentials{AppCode: "code"}}
	if _, e := p.Initialize(context.Background(), snap, submission(), domain.Attempt{ID: "id", CallbackURL: "http://evil.invalid/"}); e == nil {
		t.Fatal("HTTP return URL accepted")
	}
	if _, e := p.Recognize(context.Background(), snap, "remote_url", []byte("image")); e == nil {
		t.Fatal("unknown document accepted")
	}
	if calls != 0 {
		t.Fatal("invalid request reached provider")
	}
	snap.OCRType = "baidu"
	snap.Credentials.OCRBaiduID = "id"
	snap.Credentials.OCRBaiduKey = "key"
	if _, e := p.Recognize(context.Background(), snap, "idcard", bytes.Repeat([]byte{255}, 2<<20)); e == nil {
		t.Fatal("Baidu urlencoded image limit bypassed")
	}
	if calls != 0 {
		t.Fatal("oversized encoded image reached provider")
	}
	if _, e := p.Initialize(context.Background(), snap, submission(), attempt()); e == nil {
		t.Fatal("plaintext transport accepted as proof")
	}
	if calls != 1 {
		t.Fatal("operation retried")
	}
}

type countTransport struct{ calls *int }

func (c countTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	*c.calls++
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":200}`)), Header: http.Header{}, Request: r}, nil
}
