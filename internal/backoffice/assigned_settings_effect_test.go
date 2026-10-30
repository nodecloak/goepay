package backoffice

import (
	"reflect"
	"testing"
)

// blockname/blockalert: a listed keyword contained in the order name blocks it
// with the alert text; blank blockalert falls back to the original default; an
// empty keyword segment (trailing '|') never matches — Pay.php:103-108.
func TestBlockedOrderName(t *testing.T) {
	cases := []struct {
		blockname, blockalert, name string
		wantBlocked                 bool
		wantAlert                   string
	}{
		{"赌博|色情", "", "在线赌博充值", true, defaultBlockAlert},
		{"赌博|色情", "本店禁止该商品", "色情内容", true, "本店禁止该商品"},
		{"赌博|色情", "本店禁止该商品", "正常商品", false, ""},
		{"", "本店禁止", "任意商品", false, ""},
		{"赌博|", "", "赌博机", true, defaultBlockAlert},
		{"赌博|", "", "普通", false, ""},                    // empty segment does not match everything
		{"VIP", "", "超级VIP会员", true, defaultBlockAlert}, // substring match
	}
	for _, c := range cases {
		gotBlocked, gotAlert := BlockedOrderName(c.blockname, c.blockalert, c.name)
		if gotBlocked != c.wantBlocked || gotAlert != c.wantAlert {
			t.Errorf("BlockedOrderName(%q,%q,%q)=%v,%q want %v,%q", c.blockname, c.blockalert, c.name, gotBlocked, gotAlert, c.wantBlocked, c.wantAlert)
		}
	}
}

// blackmsg: the message a blacklisted payer sees; blank falls back to the
// original default — Pay.php:124.
func TestBlacklistMessage(t *testing.T) {
	if got := BlacklistMessage(""); got != defaultBlackMsg {
		t.Errorf("default blackmsg = %q want %q", got, defaultBlackMsg)
	}
	if got := BlacklistMessage("请联系客服处理"); got != "请联系客服处理" {
		t.Errorf("configured blackmsg = %q", got)
	}
}

// forceqq: only on (="1") and an empty merchant QQ blocks collection — Pay.php:81.
func TestForceQQBlocksPayment(t *testing.T) {
	cases := []struct {
		forceqq, qq string
		want        bool
	}{
		{"1", "", true},
		{"1", "  ", true},
		{"1", "12345", false},
		{"0", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := ForceQQBlocksPayment(c.forceqq, c.qq); got != c.want {
			t.Errorf("ForceQQBlocksPayment(%q,%q)=%v want %v", c.forceqq, c.qq, got, c.want)
		}
	}
}

// pageordername: on hides the real name (override, else "onlinepay"); off shows
// the real name — Payment.php:87/122.
func TestCheckoutDisplayName(t *testing.T) {
	cases := []struct {
		pageordername, real, override, want string
	}{
		{"1", "真实商品名", "", "onlinepay"},
		{"1", "真实商品名", "自定义名称", "自定义名称"},
		{"1", "真实商品名", "   ", "onlinepay"},
		{"0", "真实商品名", "自定义名称", "真实商品名"},
		{"", "真实商品名", "", "真实商品名"},
	}
	for _, c := range cases {
		if got := CheckoutDisplayName(c.pageordername, c.real, c.override); got != c.want {
			t.Errorf("CheckoutDisplayName(%q,%q,%q)=%q want %q", c.pageordername, c.real, c.override, got, c.want)
		}
	}
}

// pay_force_cashier_pids: exact decimal id membership in a '|'-separated list.
func TestForcesCashier(t *testing.T) {
	cases := []struct {
		list string
		mid  int64
		want bool
	}{
		{"1001|1002|3", 1002, true},
		{"1001|1002|3", 3, true},
		{"1001|1002|3", 100, false},
		{"100", 10, false}, // exact, not substring
		{"", 1001, false},
		{"1001", 0, false},
	}
	for _, c := range cases {
		if got := ForcesCashier(c.list, c.mid); got != c.want {
			t.Errorf("ForcesCashier(%q,%d)=%v want %v", c.list, c.mid, got, c.want)
		}
	}
}

// settle_*: a blank/missing switch leaves the rail enabled; only "0" disables.
// An unknown rail is left enabled.
func TestSettlementAccountTypeAllowed(t *testing.T) {
	values := map[string]string{"settle_alipay": "1", "settle_wxpay": "0", "settle_qqpay": ""}
	cases := []struct {
		acc  string
		want bool
	}{
		{"alipay", true},
		{"wxpay", false},
		{"qqpay", true},   // blank => on
		{"bank", true},    // missing => on
		{"unknown", true}, // not a gated rail
	}
	for _, c := range cases {
		if got := SettlementAccountTypeAllowed(values, c.acc); got != c.want {
			t.Errorf("SettlementAccountTypeAllowed(%q)=%v want %v", c.acc, got, c.want)
		}
	}
	got := AllowedSettlementAccountTypes(values)
	want := []string{"alipay", "qqpay", "bank"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AllowedSettlementAccountTypes=%v want %v", got, want)
	}
}

// transfer_*: blank/missing offered; only "0" hides; unknown rail offered.
func TestTransferTypeOffered(t *testing.T) {
	values := map[string]string{"transfer_wxpay": "0", "transfer_qqpay": "1"}
	if TransferTypeOffered(values, "wxpay") {
		t.Error("transfer_wxpay=0 should hide wxpay")
	}
	if !TransferTypeOffered(values, "qqpay") {
		t.Error("transfer_qqpay=1 should offer qqpay")
	}
	if !TransferTypeOffered(map[string]string{}, "wxpay") {
		t.Error("missing transfer_wxpay should default offered")
	}
	// transfer_bank is a known rail now; a missing key leaves it offered.
	if !TransferTypeOffered(values, "bank") {
		t.Error("missing transfer_bank should default offered")
	}
	if TransferTypeOffered(map[string]string{"transfer_bank": "0"}, "bank") {
		t.Error("transfer_bank=0 should hide bank")
	}
	// An account type with no transfer_* switch is still offered.
	if !TransferTypeOffered(values, "alipay") {
		t.Error("unknown transfer rail should be offered")
	}
	// values leaves qqpay on and bank on (missing), wxpay off, in list order.
	got := AllowedTransferTypes(values)
	want := []string{"qqpay", "bank"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AllowedTransferTypes=%v want %v", got, want)
	}
}

func TestPayGuardTextValid(t *testing.T) {
	if !payGuardTextValid("正常文本") {
		t.Error("valid text rejected")
	}
	if payGuardTextValid("有换行\n的文本") {
		t.Error("newline accepted")
	}
}
