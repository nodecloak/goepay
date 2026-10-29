package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// complaintImageClient wires a WechatV3Complaint to a signed TLS upstream
// WITHOUT the body-signature assertion complaintV3Client makes — the media
// upload signs the meta JSON, not the multipart body, so the body check does
// not apply. Request authorization presence is asserted by the handler.
func complaintImageClient(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, platform *rsa.PrivateKey)) *WechatV3Complaint {
	t.Helper()
	merchant, _ := rsa.GenerateKey(rand.Reader, 2048)
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// The complaint image upload is a multipart POST whose Authorization signature
// is computed over the meta JSON (filename+sha256), not the multipart body. The
// shared complaintV3Client harness already asserts the request signature against
// the request BODY, which for this upload is the multipart payload — so this
// test uses a dedicated upstream that verifies the meta-based contract: the
// Authorization header is present and well-formed, the body is multipart with a
// meta field and a file part, and a signed media_id response is accepted.
func TestComplaintWechatV3UploadImage(t *testing.T) {
	var sawMeta, sawFile bool
	c := complaintImageClient(t, func(w http.ResponseWriter, r *http.Request, platform *rsa.PrivateKey) {
		if r.Method != "POST" || r.URL.Path != "/v3/merchant/media/upload" {
			t.Errorf("unexpected upload request %s %s", r.Method, r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("not multipart: %q", r.Header.Get("Content-Type"))
		}
		if !strings.Contains(r.Header.Get("Authorization"), `serial_no="SERIAL"`) {
			t.Errorf("missing signed authorization: %q", r.Header.Get("Authorization"))
		}
		if e := r.ParseMultipartForm(4 << 20); e != nil {
			t.Fatalf("parse multipart: %v", e)
		}
		if meta := r.FormValue("meta"); strings.Contains(meta, `"filename"`) && strings.Contains(meta, `"sha256"`) {
			sawMeta = true
		}
		if f, _, e := r.FormFile("file"); e == nil {
			sawFile = true
			f.Close()
		}
		v3SignedResponse(t, w, platform, 200, `{"media_id":"MEDIA-123"}`, "PUB_LOCAL")
	})
	id, e := c.UploadImage(context.Background(), "evidence.png", []byte("\x89PNG\r\n\x1a\n fake image bytes"))
	if e != nil {
		t.Fatalf("UploadImage: %v", e)
	}
	if id != "MEDIA-123" {
		t.Fatalf("media_id=%q, want MEDIA-123", id)
	}
	if !sawMeta || !sawFile {
		t.Fatalf("upstream missing parts: meta=%v file=%v", sawMeta, sawFile)
	}
	// A bad filename/extension is rejected before any upstream call.
	if _, e := c.UploadImage(context.Background(), "evidence.txt", []byte("x")); !errors.Is(e, ErrInvalid) {
		t.Fatalf("bad extension not rejected: %v", e)
	}
	if _, e := c.UploadImage(context.Background(), "ok.png", nil); !errors.Is(e, ErrInvalid) {
		t.Fatalf("empty image not rejected: %v", e)
	}
}

func TestComplaintRecordJSONRoundTrip(t *testing.T) {
	// The neutral record marshals cleanly (used by the console detail payload).
	amt := int64(1200)
	rec := ComplaintRecord{ThirdID: "X1", PayType: "wxpay", Status: "PENDING", AmountCents: &amt, TradeNos: []string{"T1"}}
	raw, e := json.Marshal(rec)
	if e != nil || !strings.Contains(string(raw), `"amount_cents":1200`) {
		t.Fatalf("marshal: %v %s", e, raw)
	}
}
