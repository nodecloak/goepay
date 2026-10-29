package notification

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"goepay/internal/compat"
)

// hideNameRepo is paidRepo plus the optional notifyordername policy the handler
// discovers by type assertion.
type hideNameRepo struct {
	fakeRepo
	hide bool
}

func (r hideNameRepo) NotifyHideName(context.Context) (bool, error) { return r.hide, nil }

// When notifyordername is on, the callback carries name=product AND the MD5
// signature still verifies — because URLWithPolicy replaces the name before
// signing, exactly like the original creat_callback.
func TestNotifyOrderNameHidesNameAndKeepsSignatureValid(t *testing.T) {
	for _, hide := range []bool{false, true} {
		var raw string
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw = r.URL.RawQuery
			io.WriteString(w, "success")
		}))
		base := paidRepo(target.URL) // name:"g", version v1 (MD5), APIKey "secret"
		h := Handler{Store: hideNameRepo{fakeRepo: base, hide: hide}, Client: target.Client()}
		if e := h.Handle(context.Background(), notifyJob()); e != nil {
			target.Close()
			t.Fatalf("hide=%v delivery failed: %v", hide, e)
		}
		target.Close()
		values, e := url.ParseQuery(raw)
		if e != nil {
			t.Fatalf("hide=%v bad query %q: %v", hide, raw, e)
		}
		wantName := "g"
		if hide {
			wantName = "product"
		}
		if values.Get("name") != wantName {
			t.Fatalf("hide=%v name=%q want %q", hide, values.Get("name"), wantName)
		}
		p, e := compat.Parse(values)
		if e != nil {
			t.Fatalf("hide=%v parse delivered params: %v", hide, e)
		}
		sign := p["sign"]
		delete(p, "sign")
		delete(p, "sign_type")
		if !compat.VerifyMD5(p, base.m.APIKey, sign) {
			t.Fatalf("hide=%v delivered signature does not verify over delivered params", hide)
		}
	}
}
