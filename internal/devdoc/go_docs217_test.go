package devdoc

import (
	"strings"
	"testing"
)

func TestGoDocumentation217IdentifiesImplementationAndPrimaryClient(t *testing.T) {
	index := render(t, "index", testSite)
	for _, want := range []string{"Go 服务端", "Go SDK", "/doc/sdk/GoEPay_Go_SDK.zip", "V1 接口", "V2"} {
		if !strings.Contains(index, want) {
			t.Errorf("Go introduction missing %q", want)
		}
	}

	page := render(t, "sdk", testSite)
	goDownload := strings.Index(page, `href="/doc/sdk/GoEPay_Go_SDK.zip?v=`)
	if goDownload < 0 {
		t.Error("Go SDK download must be available")
	}
	for _, want := range []string{"本地 replace", "example.com/goepay-sdk", "SubmitURL", "不发送", "VerifyNotification"} {
		if !strings.Contains(page, want) {
			t.Errorf("Go client instructions missing %q", want)
		}
	}
	if strings.Contains(page, "提供 PHP SDK，封装了") || strings.Contains(page, "新接入请使用 V2.0 SDK") {
		t.Error("legacy PHP integration advice is still the primary recommendation")
	}
}

func TestGoDocumentation217SigningBoundaries(t *testing.T) {
	page := render(t, "sign_note", testSite)
	for _, want := range []string{"重复参数", "仅空白", "数组和对象", "HTTPS", "300", "未签名", "RSA PKCS#1 v1.5", "Go SDK"} {
		if !strings.Contains(page, want) {
			t.Errorf("Go protocol signing boundary missing %q", want)
		}
	}
	if strings.Contains(page, "10位整数") || strings.Contains(page, "不包括数组、字节类型参数") {
		t.Error("signing documentation still describes the original permissive parser")
	}
}
