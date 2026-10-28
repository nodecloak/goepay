package backoffice

import (
	"context"
	"fmt"
	"testing"
)

func TestMerchantTransferSettingsAreRegisteredAndValidated(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, pair := range [][2]string{{"transfer_wxpay_type", "1"}, {"transfer_wxpay_scene_id", "1005"}, {"transfer_wxpay_info_type", "岗位类型|报酬说明"}, {"transfer_wxpay_info_content", "外卖员|7月份配送费"}} {
		if _, e := s.Create(ctx, admin, "settings", input(fmt.Sprintf(`{"key":%q,"value":%q}`, pair[0], pair[1]))); e != nil {
			t.Fatal(pair, e)
		}
	}
	for _, pair := range [][2]string{{"transfer_wxpay_type", "2"}, {"transfer_wxpay_scene_id", "1001-现金营销"}, {"transfer_wxpay_info_type", "a||b"}, {"transfer_wxpay_info_type", "一二三四五六七八九十一二三四五六"}, {"transfer_wxpay_info_content", "x\ny"}, {"transfer_wxpay_info_content", "1|2|3|4|5|6|7|8|9"}} {
		if _, e := s.Create(ctx, admin, "settings", input(fmt.Sprintf(`{"key":%q,"value":%q}`, pair[0], pair[1]))); e == nil {
			t.Fatal("invalid 商家转账 setting accepted", pair)
		}
	}
	for _, empty := range []string{"transfer_wxpay_scene_id", "transfer_wxpay_info_type", "transfer_wxpay_info_content"} {
		if known, err := validateMerchantTransferSetting(empty, ""); !known || err != nil {
			t.Fatal("empty value refused", empty, err)
		}
	}
	if known, _ := validateMerchantTransferSetting("transfer_alipay", "x"); known {
		t.Fatal("claimed an unrelated key")
	}
}
