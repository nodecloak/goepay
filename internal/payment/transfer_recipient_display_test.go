package payment

import (
	"bytes"
	adapter "goepay/internal/adapters/payment"
	"testing"
)

func TestTransferRecipientDisplayAuthenticatesOriginalIdentityAndMoney(t *testing.T) {
	s, e := NewWithKey(nil, bytes.Repeat([]byte{150}, 32))
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []struct{ account, masked string }{{"owner@example.invalid", "***alid"}, {"1234567", "***"}, {"收款账号一二三四五六七八", "***五六七八"}} {
		cipher, e := s.encryptTransfer(9007199254740993, adapter.TransferRequest{TransferNo: "TF9007199254740993", Type: "alipay", Account: v.account, Name: "原私人姓名", Title: "原付款标题", Amount: 9007199254740993})
		if e != nil {
			t.Fatal(e)
		}
		out, e := s.TransferRecipientDisplay(9007199254740993, 9007199254740993, cipher)
		if e != nil || out.Type != "alipay" || out.AccountMasked != v.masked || out.NameMasked != "***" {
			t.Fatalf("masked display %+v %v", out, e)
		}
		if _, e = s.TransferRecipientDisplay(9007199254740994, 9007199254740993, cipher); e == nil {
			t.Fatal("swapped row accepted")
		}
		if _, e = s.TransferRecipientDisplay(9007199254740993, 9007199254740992, cipher); e == nil {
			t.Fatal("different amount accepted")
		}
		cipher[len(cipher)-1] ^= 1
		if _, e = s.TransferRecipientDisplay(9007199254740993, 9007199254740993, cipher); e == nil {
			t.Fatal("tampered intent accepted")
		}
	}
}

func TestTransferRecipientDisplayLegacyAndInvalidIntent(t *testing.T) {
	s, e := NewWithKey(nil, bytes.Repeat([]byte{150}, 32))
	if e != nil {
		t.Fatal(e)
	}
	empty, e := s.TransferRecipientDisplay(1, 100, nil)
	if e != nil || empty != (TransferRecipientDisplay{}) {
		t.Fatal("legacy original recipient was invented")
	}
	for _, v := range []adapter.TransferRequest{
		{TransferNo: "TF2", Type: "alipay", Account: "private", Amount: 100},
		{TransferNo: "TF1", Type: "unexpected", Account: "private", Amount: 100},
		{TransferNo: "TF1", Type: "alipay", Account: "", Amount: 100},
	} {
		cipher, e := s.encryptTransfer(1, v)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.TransferRecipientDisplay(1, 100, cipher); e == nil {
			t.Fatal("invalid authenticated original accepted")
		}
	}
	if _, e = s.TransferRecipientDisplay(0, 100, nil); e == nil {
		t.Fatal("zero id accepted")
	}
	if _, e = s.TransferRecipientDisplay(1, 0, nil); e == nil {
		t.Fatal("zero amount accepted")
	}
}
