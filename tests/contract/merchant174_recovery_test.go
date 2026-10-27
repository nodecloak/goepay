package contract_test

import (
	"context"
	"testing"

	"goepay/internal/compat"
)

func TestMerchant174RefundClosePreservesAcceptedOriginalRecoveryAndInternalAuthority(t *testing.T) {
	f, p, o := merchant174Runtime(t)
	ctx := context.Background()
	merchant174Set(t, f, 1, 1, 1)
	params := func(no string) compat.Params {
		return compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": no, "money": "1.00"}
	}
	first := merchant174Invoke(t, f, "/api/pay/refund", params("original-refund174"))
	if first["code"] != float64(0) || p.refundWrites.Load() != 1 {
		t.Fatal("refund-on positive control failed", first)
	}
	original, e := f.s.Store.Refund(ctx, 2, "original-refund174")
	if e != nil || original.Status != "pending" {
		t.Fatal(original, e)
	}
	merchant174Set(t, f, 0, 0, 0)
	replay := merchant174Invoke(t, f, "/api/pay/refund", params("original-refund174"))
	if replay["code"] != float64(0) || p.refundWrites.Load() != 1 {
		t.Fatal("closed API re-sent/lost original accepted refund", replay)
	}
	if out := merchant174Invoke(t, f, "/api/pay/refund", params("fresh-refund174")); out["code"] != float64(-1) {
		t.Fatal("closed refund API admitted a new intent", out)
	}
	out := merchant174Invoke(t, f, "/api/pay/refundquery", compat.Params{"out_refund_no": "original-refund174"})
	if out["code"] != float64(0) || out["status"] != float64(1) {
		t.Fatal("closed API blocked trusted original refund query", out)
	}
	recovered, e := f.s.Store.Refund(ctx, 2, "original-refund174")
	if e != nil || recovered.ID != original.ID || recovered.Status != "succeeded" || p.refundWrites.Load() != 1 {
		t.Fatal("refund original identity/final result changed", recovered, e)
	}
	// A merchant's explicit API preference cannot disable a trusted internal
	// refund path or convert it into an API admission.
	direct, e := f.s.ProviderFlow.Refund(ctx, 2, o.TradeNo, "console-origin-refund174", 100)
	if e != nil || direct.ID == 0 || p.refundWrites.Load() != 2 {
		t.Fatal("API-only preference incorrectly blocked trusted console flow", direct, e)
	}
	var count int
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM refunds`).Scan(&count); e != nil || count != 2 {
		t.Fatal("refund close created/duplicated an intent", count, e)
	}
}
func TestMerchant174TransferClosePreservesOriginalQueryAndNeverRearmsIt(t *testing.T) {
	f, p, _ := merchant174Runtime(t)
	ctx := context.Background()
	// Legacy configured administrator grants retain prior API behavior when the
	// new personal policy has never been configured.
	state := apiSwitchState174(t, f.request("GET", "api-switches", "", f.csrf))
	if state["configured"] != false || state["transfer"] != float64(1) {
		t.Fatal("legacy enabled API was silently disabled", state)
	}
	params := func(no string) compat.Params {
		return compat.Params{"type": "alipay", "channel_id": "7", "out_biz_no": no, "account": "synthetic-payee@example.invalid", "money": "1.00", "remark": "synthetic"}
	}
	first := merchant174Invoke(t, f, "/api/transfer/submit", params("original-transfer174"))
	if first["code"] != float64(0) || p.transferWrites.Load() != 1 {
		t.Fatal("transfer-on positive control failed", first)
	}
	original, e := f.s.Store.Transfer(ctx, 2, "original-transfer174")
	if e != nil {
		t.Fatal(e)
	}
	merchant174Set(t, f, 1, 1, 0)
	replay := merchant174Invoke(t, f, "/api/transfer/submit", params("original-transfer174"))
	if replay["biz_no"] != first["biz_no"] || p.transferWrites.Load() != 1 {
		t.Fatal("closed API rearmed/lost original transfer", replay)
	}
	if out := merchant174Invoke(t, f, "/api/transfer/submit", params("fresh-transfer174")); out["code"] != float64(-1) {
		t.Fatal("closed transfer API accepted fresh funds", out)
	}
	if out := merchant174Invoke(t, f, "/api/transfer/balance", compat.Params{}); out["code"] != float64(-1) {
		t.Fatal("closed transfer API balance route ignored preference", out)
	}
	f.s.ProviderFlow.AllowLive = false
	out := merchant174Invoke(t, f, "/api/transfer/query", compat.Params{"out_biz_no": "original-transfer174"})
	if out["code"] != float64(0) || out["status"] != float64(1) {
		t.Fatal("closed API/live write gate blocked original read-only recovery", out)
	}
	final, e := f.s.Store.Transfer(ctx, 2, "original-transfer174")
	if e != nil || final.ID != original.ID || final.ChannelID != original.ChannelID || p.transferWrites.Load() != 1 {
		t.Fatal("transfer changed original binding/send", final.ID, e)
	}
	balance, e := f.s.Store.Balance(ctx, 2)
	if e != nil || balance.Available != 900 || balance.Reserved != 0 {
		t.Fatal("known result lost/duplicated original reserve", balance, e)
	}
	f.s.ProviderFlow.AllowLive = true
	proof := merchant174Invoke(t, f, "/api/transfer/proof", compat.Params{"out_biz_no": "original-transfer174"})
	if proof["code"] != float64(0) || proof["file_id"] != "synthetic-proof174" {
		t.Fatal("closed API could not read original transfer proof", proof)
	}
	var count int
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM transfers`).Scan(&count); e != nil || count != 1 {
		t.Fatal("new/duplicate transfer after close", count, e)
	}
}
