package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"goepay/internal/compat"
)

// These literals cover browser IPv4 parsing and IDNA host normalization. None
// requires resolving a hostname or contacting the saved destination.
var notificationReturn200PrivateBrowserURLs = []struct {
	name   string
	target string
}{
	{"short IPv4", "http://127.1/private200"},
	{"short IPv4 trailing dot", "http://127.1./private200"},
	{"octal IPv4", "http://0177.0.0.1/private200"},
	{"hex IPv4 component", "http://0x7f.0.0.1/private200"},
	{"decimal IPv4 integer", "http://2130706433/private200"},
	{"hex IPv4 integer", "http://0x7f000001/private200"},
	{"ideographic full stop", "http://127\u30020.0.1/private200"},
	{"fullwidth full stop", "http://127\uff0e0.0.1/private200"},
	{"halfwidth ideographic full stop", "http://127\uff610.0.1/private200"},
	{"fullwidth IPv4 digits", "http://\uff11\uff12\uff17.0.0.1/private200"},
	{"percent encoded IPv4 digits", "http://%31%32%37.0.0.1/private200"},
	{"percent encoded IPv4 dot", "http://127%2e0.0.1/private200"},
	{"octal private IPv4", "http://0300.0250.0.1/private200"},
	{"decimal private IPv4 integer", "http://3232235521/private200"},
	{"hex private IPv4 integer", "http://0xc0a80001/private200"},
	{"hex link local IPv4 integer", "http://0xa9fea9fe/private200"},
	{"IPv4 mapped IPv6 loopback", "http://[::ffff:127.0.0.1]/private200"},
	{"expanded IPv6 loopback", "http://[0:0:0:0:0:0:0:1]/private200"},
}

func TestNotificationReturn200ActualHTTPPreservesPublicReturnDestinationsAndSignedQuery(t *testing.T) {
	f := notificationReturn200Local(t)
	order := f.order(t, "return200-browser-public", "v1", "https://merchant.example.invalid/original200", true)
	before := f.effects(t)
	for _, target := range []string{
		"https://merchant.example.invalid/return200?extension=%2F%2B%3D",
		"https://8.8.8.8/return200?extension=%2F%2B%3D",
		"https://[2606:4700:4700::1111]/return200?extension=%2F%2B%3D",
		"https://例子.example/return200?extension=%2F%2B%3D",
	} {
		t.Run(target, func(t *testing.T) {
			if _, err := f.p.Exec(t.Context(), "UPDATE orders SET return_url=$1 WHERE id=$2", target, order.ID); err != nil {
				t.Fatal(err)
			}
			original, err := url.Parse(target)
			if err != nil {
				t.Fatal(err)
			}
			result := notificationReturn200URL(t, f.request("admin", f.token, f.csrf, "POST", order.TradeNo+"/return-notification", `{}`))
			params, err := compat.Parse(result.Query())
			if err != nil || result.Scheme != original.Scheme || result.Host != original.Host || result.Path != original.Path || params["extension"] != "/+=" || params["trade_no"] != order.TradeNo || !compat.VerifyMD5(params, "test-secret", params["sign"]) {
				t.Fatal("public return destination or original signed extension changed")
			}
			detail := f.request("merchant", f.merchantToken, f.merchantCSRF, "GET", order.TradeNo, "")
			var capability struct {
				Data struct {
					Allowed bool `json:"can_return_notification"`
				} `json:"data"`
			}
			if detail.Code != http.StatusOK || json.Unmarshal(detail.Body.Bytes(), &capability) != nil || !capability.Data.Allowed {
				t.Fatal("public return destination lost its original merchant capability")
			}
		})
	}
	if after := f.effects(t); after != before {
		t.Fatal("public synchronous return generation changed async delivery or funds")
	}
}

func TestNotificationReturn200ActualHTTPRejectsBrowserNormalizedPrivateReturnURLs(t *testing.T) {
	f := notificationReturn200Local(t)
	order := f.order(t, "return200-browser-private-post", "v1", "https://merchant.example.invalid/original200", true)
	before := f.effects(t)
	for _, tc := range notificationReturn200PrivateBrowserURLs {
		t.Run(tc.name, func(t *testing.T) {
			// Import a synthetic original URL directly, retaining all other paid
			// order facts. The endpoint must validate stored destinations itself.
			if _, err := f.p.Exec(t.Context(), "UPDATE orders SET return_url=$1 WHERE id=$2", tc.target, order.ID); err != nil {
				t.Fatal(err)
			}
			for _, actor := range []struct{ role, token, csrf string }{
				{"admin", f.token, f.csrf},
				{"merchant", f.merchantToken, f.merchantCSRF},
			} {
				t.Run(actor.role, func(t *testing.T) {
					out := f.request(actor.role, actor.token, actor.csrf, "POST", order.TradeNo+"/return-notification", `{}`)
					if out.Code != http.StatusConflict {
						t.Errorf("stored browser-private destination was accepted: HTTP=%d, want 409", out.Code)
					}
					if strings.Contains(out.Body.String(), `"url"`) || strings.Contains(out.Body.String(), "sign=") {
						t.Error("browser-private destination released signed return material")
					}
				})
			}
		})
	}
	if after := f.effects(t); after != before {
		t.Fatal("rejecting browser-private return URLs changed async delivery or funds")
	}
	var audits int
	if err := f.p.QueryRow(t.Context(), "SELECT count(*) FROM backoffice_audit WHERE operation='return-notification'").Scan(&audits); err != nil || audits != 0 {
		t.Fatal("rejected browser-private return generation committed a success audit")
	}
}

func TestNotificationReturn200ActualHTTPDetailDeniesBrowserNormalizedPrivateReturnURLs(t *testing.T) {
	f := notificationReturn200Local(t)
	order := f.order(t, "return200-browser-private-capability", "v1", "https://merchant.example.invalid/original200", true)
	before := f.effects(t)
	for _, tc := range notificationReturn200PrivateBrowserURLs {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.p.Exec(t.Context(), "UPDATE orders SET return_url=$1 WHERE id=$2", tc.target, order.ID); err != nil {
				t.Fatal(err)
			}
			for _, actor := range []struct{ role, token, csrf string }{
				{"admin", f.token, f.csrf},
				{"merchant", f.merchantToken, f.merchantCSRF},
			} {
				t.Run(actor.role, func(t *testing.T) {
					out := f.request(actor.role, actor.token, actor.csrf, "GET", order.TradeNo, "")
					var decoded struct {
						Data map[string]json.RawMessage `json:"data"`
					}
					if out.Code != http.StatusOK || json.Unmarshal(out.Body.Bytes(), &decoded) != nil {
						t.Fatalf("synthetic paid order detail unavailable: HTTP=%d", out.Code)
					}
					var allowed bool
					capability, exists := decoded.Data["can_return_notification"]
					if !exists || json.Unmarshal(capability, &allowed) != nil || allowed {
						t.Error("detail must explicitly deny synchronous return to a browser-private stored URL")
					}
				})
			}
		})
	}
	if after := f.effects(t); after != before {
		t.Fatal("browser-private capability observation changed async delivery or funds")
	}
}
