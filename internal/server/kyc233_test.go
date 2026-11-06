package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/identity"
	"goepay/internal/kyc"
	"goepay/internal/payment"
)

type localKYC233 struct {
	initCalls, queryCalls, ocrCalls                      atomic.Int64
	initResult                                           kyc.InitResult
	queryResult                                          kyc.QueryResult
	initError, queryError, ocrError                      error
	initStarted, initRelease, queryStarted, queryRelease chan struct{}
	ocrStarted, ocrRelease                               chan struct{}
	observedPhone                                        string
	lastAttempt                                          kyc.Attempt
	lastQuery                                            kyc.Attempt
}

func (p *localKYC233) Initialize(ctx context.Context, _ kyc.Snapshot, v kyc.Submission, attempt kyc.Attempt) (kyc.InitResult, error) {
	p.initCalls.Add(1)
	p.observedPhone = v.Phone
	p.lastAttempt = attempt
	if p.initStarted != nil {
		close(p.initStarted)
		select {
		case <-p.initRelease:
		case <-ctx.Done():
			return kyc.InitResult{}, ctx.Err()
		}
	}
	return p.initResult, p.initError
}
func (p *localKYC233) Query(ctx context.Context, _ kyc.Snapshot, attempt kyc.Attempt) (kyc.QueryResult, error) {
	p.queryCalls.Add(1)
	p.lastQuery = attempt
	if p.queryStarted != nil {
		close(p.queryStarted)
		select {
		case <-p.queryRelease:
		case <-ctx.Done():
			return kyc.QueryResult{}, ctx.Err()
		}
	}
	return p.queryResult, p.queryError
}
func (p *localKYC233) Recognize(ctx context.Context, _ kyc.Snapshot, _ string, _ []byte) (kyc.OCRResult, error) {
	p.ocrCalls.Add(1)
	if p.ocrStarted != nil {
		close(p.ocrStarted)
		select {
		case <-p.ocrRelease:
		case <-ctx.Done():
			return kyc.OCRResult{}, ctx.Err()
		}
	}
	return kyc.OCRResult{Fields: map[string]string{"name": "本地模拟识别"}}, p.ocrError
}

type kyc233Fixture struct {
	channel168Fixture
	merchantToken, merchantCSRF string
	provider                    *localKYC233
}

func newKYC233(t *testing.T) kyc233Fixture {
	t.Helper()
	f := kyc233Fixture{channel168Fixture: channel168Local(t)}
	if _, e := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('merchant233','merchant',1,'Synthetic Merchant','unused-synthetic-hash')`); e != nil {
		t.Fatal(e)
	}
	var e error
	f.merchantToken, f.merchantCSRF, e = f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "merchant233", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	f.provider = &localKYC233{initResult: kyc.InitResult{Token: "synthetic-proof-token233", URL: "https://faceid.qq.com/?token=synthetic233", Scan: "wechat", Status: "pending"}, queryResult: kyc.QueryResult{Status: "passed"}}
	f.s.KYC233Provider = f.provider
	f.s.KYC233BaseURL = "https://local.example.invalid"
	return f
}
func (f kyc233Fixture) call(method, role, path string, body any) *httptest.ResponseRecorder {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, "/console/v1/"+role+path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	token, csrf := f.token, f.csrf
	if role == "merchant" {
		token, csrf = f.merchantToken, f.merchantCSRF
	}
	r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func kyc233View[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var envelope struct {
		Code int `json:"code"`
		Data T   `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &envelope) != nil || envelope.Code != 0 {
		t.Fatalf("KYC233 HTTP %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("identity response cached")
	}
	return envelope.Data
}
func (f kyc233Fixture) settings(t *testing.T) kyc.ConfigInput {
	t.Helper()
	v := kyc233View[kyc.ConfigView](t, f.call("GET", "admin", "/settings/certification", nil))
	in := kyc.ConfigInput{ExpectedRevision: v.Revision, Mode: v.Mode, ChannelID: v.ChannelID, Corporate: v.Corporate, Force: v.Force, Money: v.Money, OCRType: v.OCRType, Credentials: map[string]kyc.CredentialEdit{}}
	for _, k := range kyc.CredentialKeys() {
		in.Credentials[k] = kyc.CredentialEdit{Action: "keep"}
	}
	return in
}
func (f kyc233Fixture) configure(t *testing.T, fee string) kyc.ConfigView {
	t.Helper()
	in := f.settings(t)
	in.Mode = 4
	in.Money = fee
	in.Credentials["cert_qcloudid"] = kyc.CredentialEdit{Action: "replace", Value: "local-secret-id233"}
	in.Credentials["cert_qcloudkey"] = kyc.CredentialEdit{Action: "replace", Value: "local-secret-key233"}
	in.Credentials["cert_qcloudruleid"] = kyc.CredentialEdit{Action: "replace", Value: "local-rule233"}
	return kyc233View[kyc.ConfigView](t, f.call("PUT", "admin", "/settings/certification", in))
}
func submission233() map[string]any {
	return map[string]any{"expected_revision": "1", "certtype": 0, "certcardtype": 0, "certname": "本地测试", "certno": "11010519491231002X", "certcorpname": "", "certcorpno": "", "consent": true}
}
func operation233(id string) kyc.Operation { return kyc.Operation{AttemptID: id} }

func TestKYC233ConfigurationSchemasEncryptionAndAuthorization(t *testing.T) {
	f := newKYC233(t)
	in := f.settings(t)
	if in.Mode != 0 || in.ExpectedRevision != 0 || in.OCRType != "" || len(in.Credentials) != 12 {
		t.Fatal("unsafe defaults")
	}
	bad := in
	bad.Mode = 4
	if got := f.call("PUT", "admin", "/settings/certification", bad).Code; got != 422 {
		t.Fatal("missing credentials accepted", got)
	}
	v := f.configure(t, "0.00")
	if v.Revision != 1 || !v.Credentials["cert_qcloudkey"].Present {
		t.Fatal("config not committed")
	}
	read := f.call("GET", "admin", "/settings/ocr", nil)
	if strings.Contains(read.Body.String(), "local-secret-") {
		t.Fatal("credential echoed")
	}
	var cipher []byte
	if e := f.p.QueryRow(t.Context(), "SELECT config_cipher FROM kyc_settings233").Scan(&cipher); e != nil || bytes.Contains(cipher, []byte("local-secret-key233")) {
		t.Fatal("credentials not encrypted", e)
	}
	if got := f.call("PUT", "admin", "/settings/certification", in).Code; got != 409 {
		t.Fatal("stale config replaced", got)
	}
	for _, payload := range []string{`{"expected_revision":"1","cert_open":null}`, `{"expected_revision":"1","cert_open":0,"cert_channel":"0","cert_corpopen":false,"cert_force":false,"cert_money":"0.00","ocr_type":"","credentials":{}}`} {
		r := httptest.NewRequest("PUT", "/console/v1/admin/settings/ocr", strings.NewReader(payload))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("ambiguous config accepted", w.Code)
		}
	}
	r := httptest.NewRequest("PUT", "/console/v1/admin/settings/certification", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("CSRF missing accepted")
	}
	if _, e := f.p.Exec(t.Context(), "UPDATE console_users SET admin_class='operator' WHERE username='admin168'"); e != nil {
		t.Fatal(e)
	}
	if got := f.call("GET", "admin", "/settings/ocr", nil).Code; got != 401 {
		t.Fatal("demoted session not revoked", got)
	}
	var version int64
	if e := f.p.QueryRow(t.Context(), "SELECT auth_version FROM console_users WHERE username='admin168' AND role='admin'").Scan(&version); e != nil {
		t.Fatal(e)
	}
	var e error
	f.token, f.csrf, e = f.s.Sessions.Create(identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "operator", AuthVersion: version})
	if e != nil {
		t.Fatal(e)
	}
	if got := f.call("GET", "admin", "/settings/ocr", nil).Code; got != 403 {
		t.Fatal("operator read credentials", got)
	}
}

func TestKYC233LocalLifecycleSuccessOnlyFeeAndIdempotency(t *testing.T) {
	f := newKYC233(t)
	if _, e := f.s.Store.AdminAdjustBalance(t.Context(), 1, "credit", "local-cert233-fund", "synthetic local funding", 1000, "test"); e != nil {
		t.Fatal(e)
	}
	f.configure(t, "1.50")
	bad := submission233()
	bad["consent"] = false
	if got := f.call("POST", "merchant", "/account/certificate/submit", bad).Code; got != 422 {
		t.Fatal("consent bypass", got)
	}
	ready := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/submit", submission233()))
	if ready.Status != "ready" || f.provider.initCalls.Load() != 0 {
		t.Fatal("submit initiated billable call")
	}
	pending := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/geturl", operation233(ready.AttemptID)))
	if pending.Status != "pending" || pending.URL == "" {
		t.Fatal("URL unavailable")
	}
	_ = kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/geturl", operation233(ready.AttemptID)))
	if f.provider.initCalls.Load() != 1 {
		t.Fatal("duplicate initialization")
	}
	paid := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/query", operation233(ready.AttemptID)))
	if paid.Cert != 1 || paid.Status != "passed" || paid.Name == "本地测试" || paid.Number == "11010519491231002X" {
		t.Fatal("record missing or not masked")
	}
	_ = kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/query", operation233(ready.AttemptID)))
	if f.provider.queryCalls.Load() != 1 {
		t.Fatal("duplicate completed query reached upstream")
	}
	var balance, journals int64
	if e := f.p.QueryRow(t.Context(), "SELECT balance,(SELECT count(*) FROM journals WHERE event_key LIKE 'certification233:%') FROM accounts WHERE owner_id=1 AND kind='available' AND scope_id=0").Scan(&balance, &journals); e != nil || balance != 850 || journals != 1 {
		t.Fatal("fee not exactly once", balance, journals, e)
	}
	if got := f.call("POST", "merchant", "/account/certificate/submit", submission233()).Code; got != 409 {
		t.Fatal("certified personal replaced", got)
	}
	var encrypted []byte
	if e := f.p.QueryRow(t.Context(), "SELECT record_cipher FROM merchant_certifications233 WHERE merchant_id=1").Scan(&encrypted); e != nil || bytes.Contains(encrypted, []byte("11010519491231002X")) {
		t.Fatal("identity stored plaintext", e)
	}
	report, e := f.s.Store.CheckLedger(t.Context())
	if e != nil || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
		t.Fatal("ledger invalid", e)
	}
}

func TestKYC233UnknownCancellationCrossMerchantAndConfigRotation(t *testing.T) {
	f := newKYC233(t)
	f.configure(t, "0.00")
	ready := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/submit", submission233()))
	if e := f.s.Store.CreateMerchant(t.Context(), payment.Merchant{ID: 2, Name: "Synthetic Other", APIKey: "local-key233"}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('other233','merchant',2,'Synthetic Other','unused-synthetic-hash')`); e != nil {
		t.Fatal(e)
	}
	other := f
	var e error
	other.merchantToken, other.merchantCSRF, e = f.s.Sessions.Create(identity.User{ID: 2, Role: "merchant", Username: "other233", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	if got := other.call("POST", "merchant", "/account/certificate/geturl", operation233(ready.AttemptID)).Code; got != 409 {
		t.Fatal("cross-merchant attempt", got)
	}
	f.provider.initError = errors.New("synthetic upstream timeout containing PRIVATE_ID233")
	unknown := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/geturl", operation233(ready.AttemptID)))
	if unknown.Status != "unknown" {
		t.Fatal("timeout not durable unknown")
	}
	if got := f.call("POST", "merchant", "/account/certificate/geturl", operation233(ready.AttemptID)).Code; got != 409 || f.provider.initCalls.Load() != 1 {
		t.Fatal("unknown initialization resubmitted")
	}
	_ = kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/reset", operation233(ready.AttemptID)))
	f.provider.initError = nil
	next := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/submit", submission233()))
	if next.AttemptID == ready.AttemptID {
		t.Fatal("cancelled attempt reused")
	}
	in := f.settings(t)
	in.Money = "0.01"
	_ = kyc233View[kyc.ConfigView](t, f.call("PUT", "admin", "/settings/certification", in))
	if got := f.call("POST", "merchant", "/account/certificate/geturl", operation233(next.AttemptID)).Code; got != 409 || f.provider.initCalls.Load() != 1 {
		t.Fatal("stale config sent identity", got)
	}
	var charged int
	if e = f.p.QueryRow(t.Context(), "SELECT count(*) FROM journals WHERE event_key LIKE 'certification233:%'").Scan(&charged); e != nil || charged != 0 {
		t.Fatal("failed attempts charged", e)
	}
}

func (f kyc233Fixture) upload(kind, id string, raw []byte) *httptest.ResponseRecorder {
	return f.uploadExpected(kind, id, "aliyun", "1", raw)
}
func (f kyc233Fixture) uploadExpected(kind, id, provider, revision string, raw []byte) *httptest.ResponseRecorder {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	form.WriteField("document_type", kind)
	form.WriteField("operation_id", id)
	form.WriteField("expected_provider", provider)
	form.WriteField("expected_revision", revision)
	part, _ := form.CreateFormFile("image", "synthetic.png")
	part.Write(raw)
	form.Close()
	r := httptest.NewRequest("POST", "/console/v1/merchant/account/ocr", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("X-CSRF-Token", f.merchantCSRF)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.merchantToken})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func TestOCR233LocalUploadOneUseValidationAndNoPersistence(t *testing.T) {
	f := newKYC233(t)
	in := f.settings(t)
	in.OCRType = "aliyun"
	in.Credentials["ocr_aliyunid"] = kyc.CredentialEdit{Action: "replace", Value: "synthetic-ocr-id233"}
	in.Credentials["ocr_aliyunkey"] = kyc.CredentialEdit{Action: "replace", Value: "synthetic-ocr-secret233"}
	_ = kyc233View[kyc.ConfigView](t, f.call("PUT", "admin", "/settings/ocr", in))
	var raw bytes.Buffer
	png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 64, 64)))
	if got := f.upload("idcard", "00000000000000000000000000000001", []byte("not image")).Code; got != 422 || f.provider.ocrCalls.Load() != 0 {
		t.Fatal("invalid image reached provider", got)
	}
	if got := f.upload("https://internal/", "00000000000000000000000000000002", raw.Bytes()).Code; got != 422 || f.provider.ocrCalls.Load() != 0 {
		t.Fatal("invalid document reached provider", got)
	}
	ocr := kyc233View[kyc.OCRResult](t, f.upload("idcard", "00000000000000000000000000000003", raw.Bytes()))
	if ocr.Fields["name"] == "" || f.provider.ocrCalls.Load() != 1 {
		t.Fatal("OCR result unavailable")
	}
	if got := f.upload("idcard", "00000000000000000000000000000003", raw.Bytes()).Code; got != 409 || f.provider.ocrCalls.Load() != 1 {
		t.Fatal("OCR operation repeated", got)
	}
	f.provider.ocrError = errors.New("synthetic unknown SECRET233")
	if got := f.upload("idcard", "00000000000000000000000000000004", raw.Bytes()).Code; got != 503 {
		t.Fatal("OCR timeout not unknown", got)
	}
	if got := f.upload("idcard", "00000000000000000000000000000004", raw.Bytes()).Code; got != 409 || f.provider.ocrCalls.Load() != 2 {
		t.Fatal("unknown OCR retried", got)
	}
	var columns int
	if e := f.p.QueryRow(t.Context(), "SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='ocr_operations233'").Scan(&columns); e != nil || columns != 4 {
		t.Fatal("OCR source/result persisted", columns, e)
	}
	info := kyc233View[kyc.Info](t, f.call("GET", "merchant", "/account/certificate/info", nil))
	if info.Cert != 0 {
		t.Fatal("OCR alone certified merchant")
	}
}

func (f kyc233Fixture) managedAlipay(t *testing.T) payment.ParentChannelView {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	origin := channelconfig.Channel{ID: 7, Plugin: "alipay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "2021000000000233", "appmchid": "2088000000000233", "appsecret": parent180Private(key), "appkey": parent180Public(key)}}
	factory, e := channelconfig.NewParentChannelFactory(origin, channelconfig.ParentBuildOptions{})
	if e != nil {
		t.Fatal(e)
	}
	ex, _, e := factory.BootstrapSnapshot(7)
	if e != nil {
		t.Fatal(e)
	}
	f.s.ProviderFlow.Providers[7] = ex.Payment
	f.s.ProviderFlow.ParentFactories = map[int64]adapter.ParentAccountFactory{7: factory}
	template, e := f.s.ProviderFlow.ParentChannelTemplate(7, nil)
	if e != nil {
		t.Fatal(e)
	}
	session, ok := f.s.Sessions.Get(f.token, "admin")
	if !ok {
		t.Fatal("admin session unavailable")
	}
	v, e := f.s.Store.AdoptParentChannel(t.Context(), payment.ParentChannelActor{User: session.User, Token: f.token, CSRF: f.csrf}, template, template.Public().Fingerprint)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestKYC233DurableVerifiedProofRecoversFeeAfterReloginWithoutUpstreamReplay(t *testing.T) {
	for _, mode := range []int{2, 3} {
		t.Run(map[int]string{2: "phone", 3: "alipay_oauth"}[mode], func(t *testing.T) {
			f := newKYC233(t)
			if _, e := f.s.Store.AdminAdjustBalance(t.Context(), 1, "credit", "verified233-fund", "synthetic local funding", 100, "test"); e != nil {
				t.Fatal(e)
			}
			in := f.settings(t)
			in.Mode = mode
			in.Money = "1.00"
			if mode == 2 {
				in.Credentials["cert_appcode"] = kyc.CredentialEdit{Action: "replace", Value: "local-phone-app233"}
				if _, e := f.p.Exec(t.Context(), `UPDATE merchants SET profile='{"bind_mobile":"13800138000","phone":"13900139000"}' WHERE id=1`); e != nil {
					t.Fatal(e)
				}
				f.provider.initResult = kyc.InitResult{Status: "passed", Scan: "phone"}
			} else {
				f.managedAlipay(t)
				in.ChannelID = "7"
				f.provider.initResult.URL = "https://openauth.alipay.com/oauth2/publicAppAuthorize.htm?scope=id_verify"
				f.provider.initResult.Scan = "alipay"
			}
			_ = kyc233View[kyc.ConfigView](t, f.call("PUT", "admin", "/settings/certification", in))
			ready := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/submit", submission233()))
			if _, e := f.s.Store.AdminAdjustBalance(t.Context(), 1, "debit", "verified233-spend", "synthetic local spend", 100, "test"); e != nil {
				t.Fatal(e)
			}
			result := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/geturl", operation233(ready.AttemptID)))
			if mode == 3 {
				if result.Status != "pending" {
					t.Fatal("OAuth not pending")
				}
				wrong := kyc.Operation{AttemptID: ready.AttemptID, State: "wrong", AuthCode: "synthetic-authcode233"}
				if got := f.call("POST", "merchant", "/account/certificate/query", wrong).Code; got != 422 || f.provider.queryCalls.Load() != 0 {
					t.Fatal("OAuth state bypass", got)
				}
				var e error
				f.merchantToken, f.merchantCSRF, e = f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "merchant233", AuthVersion: 1})
				if e != nil {
					t.Fatal(e)
				}
				result = kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/query", kyc.Operation{AttemptID: ready.AttemptID, State: f.provider.lastAttempt.State, AuthCode: "synthetic-authcode233"}))
			}
			if result.Status != "verified" || result.Cert != 0 {
				t.Fatal("provider proof lost on fee shortage", result.Status)
			}
			if mode == 2 && f.provider.observedPhone != "13800138000" {
				t.Fatal("unverified profile phone used")
			}
			var charges int
			if e := f.p.QueryRow(t.Context(), "SELECT count(*) FROM journals WHERE event_key LIKE 'certification233:%'").Scan(&charges); e != nil || charges != 0 {
				t.Fatal("fee charged without funds", e)
			}
			// Log in again and rotate the global configuration. The frozen verified
			// proof and original fee remain usable without another provider request.
			var e error
			f.merchantToken, f.merchantCSRF, e = f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "merchant233", AuthVersion: 1})
			if e != nil {
				t.Fatal(e)
			}
			changed := f.settings(t)
			changed.Money = "2.00"
			_ = kyc233View[kyc.ConfigView](t, f.call("PUT", "admin", "/settings/certification", changed))
			if _, e = f.s.Store.AdminAdjustBalance(t.Context(), 1, "credit", "verified233-topup", "synthetic local topup", 100, "test"); e != nil {
				t.Fatal(e)
			}
			done := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/query", operation233(ready.AttemptID)))
			if done.Cert != 1 || done.Status != "passed" || f.provider.initCalls.Load() != 1 || f.provider.queryCalls.Load() != int64(mode-2) {
				t.Fatal("verified recovery repeated upstream", done.Status)
			}
			if e = f.p.QueryRow(t.Context(), "SELECT count(*) FROM journals WHERE event_key LIKE 'certification233:%'").Scan(&charges); e != nil || charges != 1 {
				t.Fatal("recovery fee not once", e)
			}
		})
	}
}

func TestKYC233InitializeCannotUseDisabledChannelAndDuplicateClaimsSendOnce(t *testing.T) {
	f := newKYC233(t)
	view := f.managedAlipay(t)
	in := f.settings(t)
	in.Mode = 1
	in.ChannelID = "7"
	_ = kyc233View[kyc.ConfigView](t, f.call("PUT", "admin", "/settings/certification", in))
	ready := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/submit", submission233()))
	session, _ := f.s.Sessions.Get(f.token, "admin")
	disabled := false
	rate := "100"
	if _, e := f.s.Store.UpdateParentChannel(t.Context(), payment.ParentChannelActor{User: session.User, Token: f.token, CSRF: f.csrf}, 7, payment.ParentChannelPatch{Expected: view.Fingerprint, Enabled: &disabled, Rate: &rate}, f.s.ProviderFlow.ParentFactories); e != nil {
		t.Fatal(e)
	}
	if got := f.call("POST", "merchant", "/account/certificate/geturl", operation233(ready.AttemptID)).Code; got != 409 || f.provider.initCalls.Load() != 0 {
		t.Fatal("disabled channel initialized", got)
	}
	// A separate explicit attempt tests concurrent claims without remote I/O.
	f2 := newKYC233(t)
	f2.configure(t, "0.00")
	r := kyc233View[kyc.Info](t, f2.call("POST", "merchant", "/account/certificate/submit", submission233()))
	f2.provider.initStarted = make(chan struct{})
	f2.provider.initRelease = make(chan struct{})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f2.call("POST", "merchant", "/account/certificate/geturl", operation233(r.AttemptID)) }()
	select {
	case <-f2.provider.initStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("provider not called")
	}
	second := f2.call("POST", "merchant", "/account/certificate/geturl", operation233(r.AttemptID))
	if second.Code != 409 {
		t.Fatal("concurrent initialization accepted", second.Code)
	}
	close(f2.provider.initRelease)
	_ = kyc233View[kyc.Info](t, <-done)
	if f2.provider.initCalls.Load() != 1 {
		t.Fatal("concurrent duplicate send")
	}
}

func TestOCR233ClaimRechecksExpiryAfterWaitingConfigurationLock(t *testing.T) {
	f := newKYC233(t)
	in := f.settings(t)
	in.OCRType = "aliyun"
	in.Credentials["ocr_aliyunid"] = kyc.CredentialEdit{Action: "replace", Value: "local-id233"}
	in.Credentials["ocr_aliyunkey"] = kyc.CredentialEdit{Action: "replace", Value: "local-key233"}
	_ = kyc233View[kyc.ConfigView](t, f.call("PUT", "admin", "/settings/ocr", in))
	block, e := f.p.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(context.Background())
	if _, e = block.Exec(t.Context(), "SELECT id FROM kyc_settings233 WHERE id=1 FOR UPDATE"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(t.Context(), "UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE role='merchant'"); e != nil {
		t.Fatal(e)
	}
	var raw bytes.Buffer
	png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 64, 64)))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f.upload("idcard", "00000000000000000000000000000009", raw.Bytes()) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting bool
		if e = f.p.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT revision,config_cipher%')").Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("OCR did not reach config lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(1100 * time.Millisecond)
	if e = block.Rollback(t.Context()); e != nil {
		t.Fatal(e)
	}
	select {
	case w := <-done:
		if w.Code != 403 {
			t.Fatal("expired OCR claim continued", w.Code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OCR claim did not terminate")
	}
	if f.provider.ocrCalls.Load() != 0 {
		t.Fatal("expired session sent identity image")
	}
	var claims int
	if e = f.p.QueryRow(t.Context(), "SELECT count(*) FROM ocr_operations233").Scan(&claims); e != nil || claims != 0 {
		t.Fatal("expired operation committed", e)
	}
}

func TestKYC233PrivateProviderProofSurvivesSessionExpiryInFlight(t *testing.T) {
	f := newKYC233(t)
	if _, e := f.s.Store.AdminAdjustBalance(t.Context(), 1, "credit", "private-proof-fund233", "synthetic local funding", 100, "test"); e != nil {
		t.Fatal(e)
	}
	in := f.settings(t)
	in.Mode = 2
	in.Money = "1.00"
	in.Credentials["cert_appcode"] = kyc.CredentialEdit{Action: "replace", Value: "local-phone-app233"}
	_ = kyc233View[kyc.ConfigView](t, f.call("PUT", "admin", "/settings/certification", in))
	if _, e := f.p.Exec(t.Context(), `UPDATE merchants SET profile='{"bind_mobile":"13800138000"}' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	ready := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/submit", submission233()))
	f.provider.initResult = kyc.InitResult{Status: "passed", Scan: "phone"}
	f.provider.initStarted = make(chan struct{})
	f.provider.initRelease = make(chan struct{})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- f.call("POST", "merchant", "/account/certificate/geturl", operation233(ready.AttemptID))
	}()
	select {
	case <-f.provider.initStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("provider not called")
	}
	if _, e := f.p.Exec(t.Context(), "UPDATE console_sessions SET expires_at='epoch' WHERE role='merchant'"); e != nil {
		t.Fatal(e)
	}
	close(f.provider.initRelease)
	select {
	case w := <-done:
		if w.Code != 403 {
			t.Fatal("expired response still authorized", w.Code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("private checkpoint blocked")
	}
	var status string
	var certs, fees int
	if e := f.p.QueryRow(t.Context(), "SELECT status,(SELECT count(*) FROM merchant_certifications233),(SELECT count(*) FROM journals WHERE event_key LIKE 'certification233:%') FROM kyc_attempts233 WHERE id=$1", ready.AttemptID).Scan(&status, &certs, &fees); e != nil || status != "verified" || certs != 0 || fees != 0 {
		t.Fatal("private proof lost or unauthorized fee", status, certs, fees, e)
	}
	var e error
	f.merchantToken, f.merchantCSRF, e = f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "merchant233", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	final := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/query", operation233(ready.AttemptID)))
	if final.Cert != 1 || f.provider.initCalls.Load() != 1 || f.provider.queryCalls.Load() != 0 {
		t.Fatal("recovery repeated billable request")
	}
}

func TestOCR233ConsentedRevisionAndProviderLimitsRejectBeforeClaim(t *testing.T) {
	f := newKYC233(t)
	in := f.settings(t)
	in.OCRType = "aliyun"
	in.Credentials["ocr_aliyunid"] = kyc.CredentialEdit{Action: "replace", Value: "local-id233"}
	in.Credentials["ocr_aliyunkey"] = kyc.CredentialEdit{Action: "replace", Value: "local-key233"}
	_ = kyc233View[kyc.ConfigView](t, f.call("PUT", "admin", "/settings/ocr", in))
	changed := f.settings(t)
	changed.OCRType = "baidu"
	changed.Credentials["ocr_baiduid"] = kyc.CredentialEdit{Action: "replace", Value: "local-baidu-id233"}
	changed.Credentials["ocr_baidukey"] = kyc.CredentialEdit{Action: "replace", Value: "local-baidu-key233"}
	_ = kyc233View[kyc.ConfigView](t, f.call("PUT", "admin", "/settings/ocr", changed))
	var small bytes.Buffer
	png.Encode(&small, image.NewRGBA(image.Rect(0, 0, 64, 64)))
	if got := f.upload("idcard", "00000000000000000000000000000010", small.Bytes()).Code; got != 409 {
		t.Fatal("provider switched after consent", got)
	}
	if got := f.uploadExpected("idcard", "00000000000000000000000000000011", "baidu", "1", small.Bytes()).Code; got != 409 {
		t.Fatal("credential revision switched after consent", got)
	}
	img := image.NewRGBA(image.Rect(0, 0, 1024, 640))
	if _, e := rand.Read(img.Pix); e != nil {
		t.Fatal(e)
	}
	var large bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if e := encoder.Encode(&large, img); e != nil {
		t.Fatal(e)
	}
	if large.Len() <= 2<<20 || large.Len() > 4<<20 {
		t.Fatal("synthetic provider limit fixture wrong size")
	}
	if got := f.uploadExpected("idcard", "00000000000000000000000000000012", "baidu", "2", large.Bytes()).Code; got != 422 {
		t.Fatal("oversized Baidu upload was consumed", got)
	}
	var claims int
	if e := f.p.QueryRow(t.Context(), "SELECT count(*) FROM ocr_operations233").Scan(&claims); e != nil || claims != 0 || f.provider.ocrCalls.Load() != 0 {
		t.Fatal("invalid OCR consent/size initiated provider", claims, e)
	}
}

func TestKYC233CorporateUpgradeAndCertificationRollbackProof(t *testing.T) {
	f := newKYC233(t)
	f.configure(t, "0.00")
	in := f.settings(t)
	in.Corporate = true
	in.Credentials["cert_appcode2"] = kyc.CredentialEdit{Action: "replace", Value: "local-company-app233"}
	_ = kyc233View[kyc.ConfigView](t, f.call("PUT", "admin", "/settings/certification", in))
	person := submission233()
	person["expected_revision"] = "2"
	ready := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/submit", person))
	_ = kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/geturl", operation233(ready.AttemptID)))
	_ = kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/query", operation233(ready.AttemptID)))
	var oldAttempt string
	var oldType int
	var oldAt time.Time
	var oldCipher, oldProof []byte
	if e := f.p.QueryRow(t.Context(), "SELECT attempt_id,cert_type,certified_at,record_cipher,proof_hash FROM merchant_certifications233 WHERE merchant_id=1").Scan(&oldAttempt, &oldType, &oldAt, &oldCipher, &oldProof); e != nil {
		t.Fatal(e)
	}
	company := submission233()
	company["expected_revision"] = "2"
	company["certtype"] = 1
	company["certcorpname"] = "本地模拟公司"
	company["certcorpno"] = "91350100M000100Y43"
	upgrade := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/submit", company))
	_ = kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/geturl", operation233(upgrade.AttemptID)))
	cert := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/query", operation233(upgrade.AttemptID)))
	if cert.CertType != 1 || cert.CompanyNumber == "91350100M000100Y43" {
		t.Fatal("company upgrade missing or unmasked")
	}
	if _, e := f.p.Exec(t.Context(), "UPDATE merchant_certifications233 SET attempt_id=$1,cert_type=$2,certified_at=$3,record_cipher=$4,proof_hash=$5 WHERE merchant_id=1", oldAttempt, oldType, oldAt, oldCipher, oldProof); e != nil {
		t.Fatal(e)
	}
	if got := f.call("GET", "merchant", "/account/certificate/info", nil).Code; got != 409 {
		t.Fatal("old personal proof accepted over company certification", got)
	}
}

func TestOCR233OutputRechecksExpiryAfterWaitingConfigurationLock(t *testing.T) {
	f := newKYC233(t)
	in := f.settings(t)
	in.OCRType = "aliyun"
	in.Credentials["ocr_aliyunid"] = kyc.CredentialEdit{Action: "replace", Value: "local-id233"}
	in.Credentials["ocr_aliyunkey"] = kyc.CredentialEdit{Action: "replace", Value: "local-key233"}
	_ = kyc233View[kyc.ConfigView](t, f.call("PUT", "admin", "/settings/ocr", in))
	f.provider.ocrStarted = make(chan struct{})
	f.provider.ocrRelease = make(chan struct{})
	var raw bytes.Buffer
	png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 64, 64)))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f.upload("idcard", "00000000000000000000000000000013", raw.Bytes()) }()
	select {
	case <-f.provider.ocrStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("OCR provider not called")
	}
	block, e := f.p.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(context.Background())
	if _, e = block.Exec(t.Context(), "SELECT id FROM kyc_settings233 WHERE id=1 FOR UPDATE"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(t.Context(), "UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE role='merchant'"); e != nil {
		t.Fatal(e)
	}
	close(f.provider.ocrRelease)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting bool
		if e = f.p.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT revision,config_cipher%')").Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("OCR output did not wait config lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(1100 * time.Millisecond)
	if e = block.Rollback(t.Context()); e != nil {
		t.Fatal(e)
	}
	select {
	case w := <-done:
		if w.Code != 403 || strings.Contains(w.Body.String(), "本地模拟识别") {
			t.Fatal("expired output disclosed OCR identity", w.Code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OCR output did not terminate")
	}
	if f.provider.ocrCalls.Load() != 1 {
		t.Fatal("output expiry triggered OCR replay")
	}
}

func TestKYC233ServerReturnInboxIsPrivateBoundAndSingleUse(t *testing.T) {
	f := newKYC233(t)
	f.managedAlipay(t)
	in := f.settings(t)
	in.Mode = 3
	in.ChannelID = "7"
	_ = kyc233View[kyc.ConfigView](t, f.call("PUT", "admin", "/settings/certification", in))
	f.provider.initResult.URL = "https://openauth.alipay.com/oauth2/publicAppAuthorize.htm?scope=id_verify"
	f.provider.initResult.Scan = "alipay"
	ready := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/submit", submission233()))
	_ = kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/geturl", operation233(ready.AttemptID)))
	if strings.Contains(f.provider.lastAttempt.CallbackURL, "state=") || !strings.Contains(f.provider.lastAttempt.CallbackURL, "/callbacks/certification233/return?attempt_id=") {
		t.Fatal("callback duplicates OAuth state or uses SPA")
	}
	service, e := f.s.certification233Service()
	if e != nil {
		t.Fatal(e)
	}
	if e = service.CacheReturn(t.Context(), ready.AttemptID, "00000000000000000000000000000000", "synthetic-return-code233"); e == nil {
		t.Fatal("wrong callback state accepted")
	}
	state := f.provider.lastAttempt.State
	for i := 0; i < 2; i++ {
		if e = service.CacheReturn(t.Context(), ready.AttemptID, state, "synthetic-return-code233"); e != nil {
			t.Fatal("same callback replay not idempotent", e)
		}
	}
	if e = service.CacheReturn(t.Context(), ready.AttemptID, state, "different-code233"); e == nil {
		t.Fatal("callback overwrote a pending code")
	}
	info := kyc233View[kyc.Info](t, f.call("GET", "merchant", "/account/certificate/info", nil))
	if !info.AuthCodeReady || info.AuthCodeUsed || f.provider.queryCalls.Load() != 0 {
		t.Fatal("inbox called provider or lost ready state")
	}
	var cipher []byte
	if e = f.p.QueryRow(t.Context(), "SELECT attempt_cipher FROM kyc_attempts233 WHERE id=$1", ready.AttemptID).Scan(&cipher); e != nil || bytes.Contains(cipher, []byte("synthetic-return-code233")) {
		t.Fatal("OAuth code not encrypted", e)
	}
	if got := f.call("POST", "merchant", "/account/certificate/query", kyc.Operation{AttemptID: ready.AttemptID, State: state, AuthCode: "synthetic-return-code233"}).Code; got != 422 || f.provider.queryCalls.Load() != 0 {
		t.Fatal("mixed browser and server code accepted", got)
	}
	var err error
	f.merchantToken, f.merchantCSRF, err = f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "merchant233", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	final := kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/query", operation233(ready.AttemptID)))
	if final.Cert != 1 || !final.AuthCodeUsed || final.AuthCodeReady || f.provider.queryCalls.Load() != 1 || f.provider.lastQuery.AuthCode != "synthetic-return-code233" {
		t.Fatal("inbox not consumed once after login")
	}
	if e = service.CacheReturn(t.Context(), ready.AttemptID, state, "synthetic-return-code233"); e == nil {
		t.Fatal("used OAuth code revived")
	}
	_ = kyc233View[kyc.Info](t, f.call("POST", "merchant", "/account/certificate/query", operation233(ready.AttemptID)))
	if f.provider.queryCalls.Load() != 1 {
		t.Fatal("used callback repeated upstream query")
	}
}
