package payment

import (
	"context"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
)

// sendClaimedTransfer shares the original verified result/ledger rules for
// single and batch transfers. The caller already owns a durable send barrier.
func (g ProviderFlow) sendClaimedTransfer(ctx context.Context, t Transfer, tp adapter.TransferProvider) (Transfer, error) {
	if !g.paymentWritesEnabled() {
		return t, ErrLiveDisabled
	}
	if e := g.checkParentTransferWrite(ctx, t); e != nil {
		return t, e
	}
	if e := g.Store.checkChannelWrite(ctx, t.ChannelID); e != nil {
		return t, e
	}
	req := transferRequest(t)
	if merchantTransferChannel(g.Providers[t.ChannelID], t.Type) {
		// The bill carries the site scene/report settings. Without them
		// nothing is sent, so the transfer definitely failed.
		var scene error
		if req, scene = g.Store.merchantTransferRequest(ctx, req); scene != nil {
			if save := g.Store.resolveProviderTransfer(ctx, t.ID, "failed", "", ""); save != nil {
				return t, errors.Join(scene, save)
			}
			current, _ := g.Store.Transfer(ctx, t.MerchantID, t.TransferNo)
			return current, scene
		}
	}
	result, e := tp.Transfer(ctx, req)
	if e != nil {
		state := "unknown"
		if t.Type != "bank" && definiteAuthenticatedTransferRejection(e, g.Providers[t.ChannelID], t.Type) {
			state = "failed"
		}
		if save := g.Store.resolveProviderTransfer(ctx, t.ID, state, "", ""); save != nil {
			return t, errors.Join(e, save)
		}
		current, _ := g.Store.Transfer(ctx, t.MerchantID, t.TransferNo)
		return current, e
	}
	if result.TransferNo != fmt.Sprintf("TF%d", t.ID) || result.Amount != t.Amount {
		g.Store.resolveProviderTransfer(ctx, t.ID, "unknown", "", "")
		return t, &adapter.UnknownOutcome{Operation: "transfer response", Cause: adapter.ErrAuthentication}
	}
	if t.Type == "bank" {
		if e = g.Store.resolveBankResult(ctx, t, result, true); e != nil {
			return t, &adapter.UnknownOutcome{Operation: "persist bank transfer", Cause: e}
		}
		return g.Store.Transfer(ctx, t.MerchantID, t.TransferNo)
	}
	state := transferProviderState(result.State)
	if state == "succeeded" && result.ProviderTransferNo == "" {
		state = "unknown"
	}
	if e = g.Store.resolveProviderTransferPackage(ctx, t.ID, state, result.ProviderTransferNo, result.PayDate, result.PackageInfo, g.Providers[t.ChannelID]); e != nil {
		return t, &adapter.UnknownOutcome{Operation: "persist transfer", Cause: e}
	}
	return g.Store.Transfer(ctx, t.MerchantID, t.TransferNo)
}
