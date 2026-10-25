package channelconfig

import (
	"context"
	"errors"
	provider "goepay/internal/adapters/payment"
	"testing"
)

func TestAlipayBankFactoryExplicitCertificateProductAndWriteGuard(t *testing.T) {
	c := certificateChannel(t)
	c.Config["enable_bank_transfer"] = "true"
	c.Config["bank_transfer_scene"] = "企业退款"
	c.Config["bank_transfer_scene_reports"] = `[{"info_type":"退款原因","info_content":"本地测试"}]`
	clients, e := Build(Import{Version: 1, Channels: []Channel{c}}, false)
	if e != nil {
		t.Fatal(e)
	}
	p := clients[9]
	if got := p.(provider.TransferCapabilities).TransferTypes(); len(got) != 2 {
		t.Fatal("bank not explicitly enabled", got)
	}
	if got := p.(provider.BankTransferProduct).BankTransferProduct(); got != provider.BankProductAlipay {
		t.Fatal("guard lost bank product", got)
	}
	if _, e = p.(provider.TransferProvider).Transfer(context.Background(), provider.TransferRequest{Type: "bank"}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal("bank financial write bypassed deployment switch", e)
	}
	for _, bad := range []string{"TRUE", "1", "invalid"} {
		c.Config["enable_bank_transfer"] = bad
		if _, e = Build(Import{Version: 1, Channels: []Channel{c}}, false); e == nil {
			t.Fatal("invalid bank flag accepted", bad)
		}
	}
	c.Config["enable_bank_transfer"] = "false"
	clients, e = Build(Import{Version: 1, Channels: []Channel{c}}, false)
	if e != nil || len(clients[9].(provider.TransferCapabilities).TransferTypes()) != 1 {
		t.Fatal("disabled bank advertised", e)
	}
	c.Config["enable_bank_transfer"] = "true"
	delete(c.Config, "bank_transfer_scene_reports")
	if _, e = Build(Import{Version: 1, Channels: []Channel{c}}, false); e == nil {
		t.Fatal("bank without required transfer report enabled")
	}
}
