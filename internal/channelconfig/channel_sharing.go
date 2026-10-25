package channelconfig

import (
	"context"
	provider "goepay/internal/adapters/payment"
)

func (g guarded) ChannelSharingRelationAccount() string {
	if p, ok := g.Provider.(provider.ChannelSharingRelationIdentity); ok {
		return p.ChannelSharingRelationAccount()
	}
	return ""
}

func sharingOptions(c Channel) (bool, bool, error) {
	values := map[string]bool{}
	for _, field := range []string{"enable_channel_sharing", "enable_channel_sharing_return"} {
		switch c.Config[field] {
		case "", "false":
		case "true":
			values[field] = true
		default:
			return false, false, ErrImport
		}
	}
	enabled, returns := values["enable_channel_sharing"], values["enable_channel_sharing_return"]
	if returns && (!enabled || c.Plugin != "alipay" || c.Config["sharing_return_evidence"] != "refund_royaltys") || !returns && c.Config["sharing_return_evidence"] != "" {
		return false, false, ErrImport
	}
	return enabled, returns, nil
}
func (g guarded) ChannelSharingCapabilities() provider.ChannelSharingCapabilities {
	if p, ok := g.Provider.(provider.ChannelSharingProvider); ok {
		return p.ChannelSharingCapabilities()
	}
	return provider.ChannelSharingCapabilities{}
}
func (g guarded) ValidateChannelSharingReceiver(r provider.SharingReceiver, remove bool) error {
	if !g.allowLive {
		return ErrLiveDisabled
	}
	if p, ok := g.Provider.(provider.ChannelSharingReceiverValidator); ok {
		return p.ValidateChannelSharingReceiver(r, remove)
	}
	return provider.ErrUnsupported
}
func (g guarded) ChannelSharingRefundMode(method string) (string, error) {
	if p, ok := g.Provider.(provider.ChannelSharingPaymentProfile); ok {
		return p.ChannelSharingRefundMode(method)
	}
	return "", provider.ErrUnsupported
}
func (g guarded) ValidateChannelSharing(r provider.ChannelSharingRequest) error {
	if !g.allowLive {
		return ErrLiveDisabled
	}
	if p, ok := g.Provider.(provider.ChannelSharingProvider); ok {
		return p.ValidateChannelSharing(r)
	}
	return provider.ErrUnsupported
}
func (g guarded) BindChannelSharingReceiver(ctx context.Context, operation string, r provider.SharingReceiver) error {
	if !g.allowLive {
		return ErrLiveDisabled
	}
	if p, ok := g.Provider.(provider.ChannelSharingProvider); ok {
		return p.BindChannelSharingReceiver(ctx, operation, r)
	}
	return provider.ErrUnsupported
}
func (g guarded) UnbindChannelSharingReceiver(ctx context.Context, operation string, r provider.SharingReceiver) error {
	if !g.allowLive {
		return ErrLiveDisabled
	}
	if p, ok := g.Provider.(provider.ChannelSharingProvider); ok {
		return p.UnbindChannelSharingReceiver(ctx, operation, r)
	}
	return provider.ErrUnsupported
}
func (g guarded) SubmitChannelSharing(ctx context.Context, r provider.ChannelSharingRequest) (provider.ChannelSharingResult, error) {
	if !g.allowLive {
		return provider.ChannelSharingResult{}, ErrLiveDisabled
	}
	if p, ok := g.Provider.(provider.ChannelSharingProvider); ok {
		return p.SubmitChannelSharing(ctx, r)
	}
	return provider.ChannelSharingResult{}, provider.ErrUnsupported
}
func (g guarded) QueryChannelSharing(ctx context.Context, r provider.ChannelSharingRequest) (provider.ChannelSharingResult, error) {
	if p, ok := g.Provider.(provider.ChannelSharingProvider); ok {
		return p.QueryChannelSharing(ctx, r)
	}
	return provider.ChannelSharingResult{}, provider.ErrUnsupported
}
func (g guarded) ReturnChannelSharing(ctx context.Context, r provider.ChannelSharingReturnRequest) (provider.ChannelSharingReturnResult, error) {
	if !g.allowLive {
		return provider.ChannelSharingReturnResult{}, ErrLiveDisabled
	}
	if p, ok := g.Provider.(provider.ChannelSharingProvider); ok {
		return p.ReturnChannelSharing(ctx, r)
	}
	return provider.ChannelSharingReturnResult{}, provider.ErrUnsupported
}
func (g guarded) QueryChannelSharingReturn(ctx context.Context, r provider.ChannelSharingReturnRequest) (provider.ChannelSharingReturnResult, error) {
	if p, ok := g.Provider.(provider.ChannelSharingProvider); ok {
		return p.QueryChannelSharingReturn(ctx, r)
	}
	return provider.ChannelSharingReturnResult{}, provider.ErrUnsupported
}
func (g guarded) FinishChannelSharing(ctx context.Context, r provider.ChannelSharingFinishRequest) (provider.ChannelSharingFinishResult, error) {
	if !g.allowLive {
		return provider.ChannelSharingFinishResult{}, ErrLiveDisabled
	}
	if p, ok := g.Provider.(provider.ChannelSharingFinisher); ok {
		return p.FinishChannelSharing(ctx, r)
	}
	return provider.ChannelSharingFinishResult{}, provider.ErrUnsupported
}
func (g guarded) QueryChannelSharingFinish(ctx context.Context, r provider.ChannelSharingFinishRequest) (provider.ChannelSharingFinishResult, error) {
	if p, ok := g.Provider.(provider.ChannelSharingFinisher); ok {
		return p.QueryChannelSharingFinish(ctx, r)
	}
	return provider.ChannelSharingFinishResult{}, provider.ErrUnsupported
}
func (g guarded) ChannelSharingRemaining(ctx context.Context, transaction string) (int64, error) {
	if p, ok := g.Provider.(provider.ChannelSharingRemainingProvider); ok {
		return p.ChannelSharingRemaining(ctx, transaction)
	}
	return 0, provider.ErrUnsupported
}
