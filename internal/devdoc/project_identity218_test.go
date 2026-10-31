package devdoc

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestProjectIdentity218PublicDocumentationIsStandalone(t *testing.T) {
	for _, slug := range allSlugs() {
		t.Run(slug, func(t *testing.T) {
			page := render(t, slug, testSite)
			for _, removed := range []string{"PHP", "epay_pro", "epay_release", "易支付协议", "原版", "兼容下载", "供已有", "迁移使用", "/doc/sdk/SDK.zip", "/doc/sdk/SDK_2.0.zip"} {
				if strings.Contains(page, removed) {
					t.Errorf("public project documentation still contains %q", removed)
				}
			}
		})
	}
}

func TestProjectIdentity218GoSDKDownloadChangesWithContent(t *testing.T) {
	sum := sha256.Sum256(sdk["GoEPay_Go_SDK.zip"])
	want := hex.EncodeToString(sum[:6])
	pattern := regexp.MustCompile(`href="(/doc/sdk/GoEPay_Go_SDK.zip[^"]*)"`)
	for _, slug := range []string{"index", "sdk"} {
		matches := pattern.FindStringSubmatch(render(t, slug, testSite))
		if len(matches) != 2 {
			t.Fatalf("%s: Go SDK download missing", slug)
		}
		u, err := url.Parse(matches[1])
		if err != nil || u.Query().Get("v") != want {
			t.Errorf("%s: SDK URL must carry the content version to avoid old client downloads", slug)
		}
	}
}

func TestProjectIdentity218OnlyGoSDKIsPublished(t *testing.T) {
	if len(sdk) != 1 || len(sdk["GoEPay_Go_SDK.zip"]) == 0 {
		t.Error("only the Go SDK archive may be embedded and published")
	}
	for _, path := range []string{"/doc/sdk/SDK.zip", "/doc/sdk/SDK_2.0.zip"} {
		w := serve(t, "GET", path, okSite)
		if w.Code != http.StatusNotFound || w.Header().Get("Cache-Control") != "no-store" || strings.HasPrefix(w.Body.String(), "PK") {
			t.Errorf("removed archive %s is still downloadable or cacheable", path)
		}
	}
}
