package channelconfig

import (
	"encoding/json"
	provider "goepay/internal/adapters/payment"
	"io"
	"strings"
)

func alipayBankConfig(c Channel) (bool, string, []provider.AlipayTransferSceneReport, error) {
	switch c.Config["enable_bank_transfer"] {
	case "", "false":
		return false, "", nil, nil
	case "true":
	default:
		return false, "", nil, ErrImport
	}
	var reports []provider.AlipayTransferSceneReport
	d := json.NewDecoder(strings.NewReader(c.Config["bank_transfer_scene_reports"]))
	d.DisallowUnknownFields()
	if e := d.Decode(&reports); e != nil {
		return false, "", nil, ErrImport
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return false, "", nil, ErrImport
	}
	return true, c.Config["bank_transfer_scene"], reports, nil
}

func (g guarded) BankTransferProduct() string {
	if p, ok := g.Provider.(provider.BankTransferProduct); ok {
		return p.BankTransferProduct()
	}
	return provider.BankProductWechat
}
