package printing

import (
	"strings"
	"testing"
)

func TestReceiptEscapesCommandsAndUsesPaidGross(t *testing.T) {
	r := Receipt{TradeNo: "1000000000000000001", MerchantName: "Local <cut/>", Name: "商品", Amount: 123, Type: "wxpay", PaidAt: "2026-10-23T12:00:00Z", Remark: "<printer><cut/>\x1b"}
	raw, err := RenderReceipt(r)
	if err == nil {
		t.Fatal("control instruction accepted", string(raw))
	}
	r.Remark = "<cut/> & buyer"
	raw, err = RenderReceipt(r)
	if err != nil || strings.Contains(string(raw), "Local <cut/>") || !strings.Contains(string(raw), "&lt;cut/&gt;") || !strings.Contains(string(raw), "1.23") {
		t.Fatal(string(raw), err)
	}
}
