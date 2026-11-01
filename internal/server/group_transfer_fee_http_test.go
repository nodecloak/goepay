package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
)

func TestGroupTransferFeeAndRedpackActualHTTPCapabilitiesAdmissionAndZeroSemantics(t *testing.T) {
	f, provider, credentials := redpackFixture(t)
	if _, err := f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=body||'{"transfer_rate":"2.50","user_transfer_red":"1"}'::jsonb WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	capabilities := func() string {
		t.Helper()
		out := groupFeatureHTTPRequest(f, credentials[0], credentials[1], "GET", "transfers/capabilities", "")
		var envelope struct {
			Data struct {
				Rate string `json:"transfer_rate"`
			} `json:"data"`
		}
		if out.Code != http.StatusOK || json.Unmarshal(out.Body.Bytes(), &envelope) != nil {
			t.Fatalf("capability HTTP=%d", out.Code)
		}
		return envelope.Data.Rate
	}
	if rate := capabilities(); rate != "2.50" {
		t.Errorf("capability returned profile rate %q instead of effective group rate", rate)
	}
	listed := groupFeatureHTTPRequest(f, credentials[0], credentials[1], "GET", "redpacks", "")
	var list struct {
		Data struct {
			Open bool `json:"open"`
		} `json:"data"`
	}
	if listed.Code != http.StatusOK || json.Unmarshal(listed.Body.Bytes(), &list) != nil || !list.Data.Open {
		t.Error("group-enabled redpack feature missing from read capability")
	}
	created := groupFeatureHTTPRequest(f, credentials[0], credentials[1], "POST", "redpacks", `{"channel_id":"7","type":"alipay","out_biz_no":"2026110100000000204","amount":"1.00","remark":"Synthetic group redpack"}`)
	if created.Code != http.StatusOK {
		t.Fatalf("group redpack enable did not apply to creation: HTTP=%d", created.Code)
	}
	if _, err := f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=body||'{"transfer_rate":"0","settle_rate":"3.21","user_transfer_red":"0"}'::jsonb WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	if rate := capabilities(); rate != "3.21" {
		t.Fatal("literal zero did not use effective settlement rate", rate)
	}
	closed := groupFeatureHTTPRequest(f, credentials[0], credentials[1], "POST", "redpacks", `{"channel_id":"7","type":"alipay","out_biz_no":"2026110100000000205","amount":"1.00","remark":"Synthetic closed redpack"}`)
	if closed.Code != http.StatusForbidden {
		t.Fatalf("closed group redpack feature accepted creation: HTTP=%d", closed.Code)
	}
	if _, err := f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=body||'{"transfer_rate":"0.00"}'::jsonb WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	if rate := capabilities(); rate != "0.00" {
		t.Fatal("explicit zero-decimal fee did not stay free", rate)
	}
	if provider.sent.Load() != 0 {
		t.Fatal("capability or redpack creation sent a real transfer")
	}
}

func TestGroupTransferFeeDisabledMerchantAdminCapabilitiesRemainReadable(t *testing.T) {
	f, provider, _ := redpackFixture(t)
	if _, err := f.p.Exec(t.Context(), `UPDATE merchants SET enabled=false WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/console/v1/admin/transfers/capabilities?merchant_id=1", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	out := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(out, r)
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Writes  bool   `json:"writes_enabled"`
			Enabled bool   `json:"merchant_enabled"`
			Rate    string `json:"transfer_rate"`
			Ledger  string `json:"ledger_available_money"`
		} `json:"data"`
	}
	if out.Code != http.StatusOK || json.Unmarshal(out.Body.Bytes(), &envelope) != nil || envelope.Code != 0 || envelope.Data.Writes || envelope.Data.Enabled || envelope.Data.Rate != "0" || envelope.Data.Ledger != "100.00" {
		t.Fatalf("disabled merchant lost administrator read capability: HTTP=%d data=%+v", out.Code, envelope.Data)
	}
	if _, err := f.s.ProviderFlow.Transfer(t.Context(), 1, 7, "disabled-fee-capability", adapter.TransferRequest{Type: "alipay", Account: "synthetic-disabled@example.invalid", Amount: 100, Title: "Synthetic disabled check"}); !errors.Is(err, payment.ErrState) || provider.sent.Load() != 0 {
		t.Fatal("read-only disabled capability weakened actual transfer admission", err, provider.sent.Load())
	}
}

func TestGroupTransferFeeActualRSAMerchantAPIBalanceUsesEffectiveRate(t *testing.T) {
	f, _, _ := redpackFixture(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	public := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	f.s.PlatformKey = key // Synthetic signing key for this isolated HTTP fixture.
	if _, err = f.p.Exec(t.Context(), `UPDATE merchants SET signature_algorithm='RSA',rsa_public_key=$1 WHERE id=1`, public); err != nil {
		t.Fatal(err)
	}
	if _, err = f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=body||'{"transfer_rate":"1.25"}'::jsonb WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	params := compat.Params{"pid": "1", "timestamp": fmt.Sprint(time.Now().Unix()), "sign_type": "RSA"}
	params["sign"], err = compat.SignRSAWithProfile(params, key, compat.ProfileSDKV2)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	for key, value := range params {
		form.Set(key, value)
	}
	r := httptest.NewRequest("POST", "/api/transfer/balance", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	out := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(out, r)
	var envelope struct {
		Code int    `json:"code"`
		Rate string `json:"transfer_rate"`
	}
	if out.Code != http.StatusOK || json.Unmarshal(out.Body.Bytes(), &envelope) != nil || envelope.Code != 0 || envelope.Rate != "1.25" {
		t.Fatalf("RSA balance did not expose effective group fee: HTTP=%d code=%d rate=%q body=%s", out.Code, envelope.Code, envelope.Rate, out.Body.String())
	}
}
