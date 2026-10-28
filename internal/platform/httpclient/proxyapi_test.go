package httpclient

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProxyAPISignsBase64TimestampAndDetectsSuccess(t *testing.T) {
	var gotURL, gotTS, gotSign, gotCT string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotURL, gotTS, gotSign, gotCT = r.PostFormValue("url"), r.PostFormValue("timestamp"), r.PostFormValue("sign"), r.Header.Get("Content-Type")
		io.WriteString(w, `{"code":0,"msg":"SUCCESS"}`)
	}))
	defer api.Close()
	client := NewProxyAPIClient()
	client.now = func() time.Time { return time.Unix(1793000000, 0) }
	target := "https://merchant.example/callback?x=1"
	status, body, err := client.Deliver(context.Background(), api.URL, "relay-key207", target)
	if err != nil || status != 200 || !ProxyAPIAcknowledged(body) {
		t.Fatalf("deliver failed: status=%d body=%q err=%v", status, body, err)
	}
	wantURL := base64.StdEncoding.EncodeToString([]byte(target))
	sum := md5.Sum([]byte(wantURL + "1793000000" + "relay-key207"))
	if gotURL != wantURL || gotTS != "1793000000" || gotSign != hex.EncodeToString(sum[:]) || gotCT != "application/x-www-form-urlencoded" {
		t.Fatalf("relay request wrong: url=%q ts=%q sign=%q ct=%q", gotURL, gotTS, gotSign, gotCT)
	}
}

func TestProxyAPIAcknowledgedMatchesOriginalVariantsOnly(t *testing.T) {
	for _, ok := range []string{"success", "SUCCESS", "Success", `{"r":"ok success"}`} {
		if !ProxyAPIAcknowledged(ok) {
			t.Errorf("rejected success body %q", ok)
		}
	}
	for _, bad := range []string{"fail", "ok", "SUCCEED", "Succes", ""} {
		if ProxyAPIAcknowledged(bad) {
			t.Errorf("accepted non-success body %q", bad)
		}
	}
}

func TestProxyAPIRejectsBadInputs(t *testing.T) {
	client := NewProxyAPIClient()
	cases := []struct{ api, key, target string }{
		{"ftp://relay.example", "k", "https://m.example/x"},
		{"relay.example", "k", "https://m.example/x"},
		{"https://relay.example", "", "https://m.example/x"},
		{"https://relay.example", "k", "http://127.0.0.1/x"},
		{"https://relay.example", "k", "not-a-url"},
	}
	for _, tc := range cases {
		if _, _, err := client.Deliver(context.Background(), tc.api, tc.key, tc.target); err != ErrProxyConfig {
			t.Fatalf("accepted bad input %+v: %v", tc, err)
		}
	}
}
