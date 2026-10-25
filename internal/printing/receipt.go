package printing

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid printing request")
var ErrConflict = errors.New("printing evidence conflict")
var ErrNotFound = errors.New("printing record not found")
var ErrState = errors.New("printing operation unavailable")
var ErrUnsupported = errors.New("external printing connector unavailable")

type Receipt struct {
	TradeNo      string `json:"trade_no"`
	MerchantName string `json:"merchant_name"`
	Name         string `json:"name"`
	Amount       int64  `json:"amount,string"`
	Type         string `json:"type"`
	PaidAt       string `json:"paid_at"`
	Remark       string `json:"remark"`
}

func safeText(v string, max int) bool {
	if !utf8.ValidString(v) || len(v) > max {
		return false
	}
	for _, r := range v {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func escape(v string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(v))
	return b.String()
}
func RenderReceipt(r Receipt) ([]byte, error) {
	if r.Amount <= 0 || !safeText(r.TradeNo, 64) || r.TradeNo == "" || !safeText(r.MerchantName, 200) || !safeText(r.Name, 200) || !safeText(r.Type, 32) || !safeText(r.PaidAt, 64) || !safeText(r.Remark, 1000) {
		return nil, ErrInvalid
	}
	return []byte(fmt.Sprintf(`<printer><br/><h3-s justify="center">%s</h3-s><br/><divider symbol="-"/><h4>支付金额: %d.%02d元</h4><br/><h4>支付类型: %s</h4><br/><h4>支付状态: 已核实支付成功</h4><br/><h4>订单号: %s</h4><br/><h4>支付时间: %s</h4><br/><h4>商品: %s</h4><br/><h4>备注: %s</h4><br/><cut/></printer>`, escape(r.MerchantName), r.Amount/100, r.Amount%100, escape(r.Type), escape(r.TradeNo), escape(r.PaidAt), escape(r.Name), escape(r.Remark))), nil
}
