package payment

import (
	"context"

	provider "goepay/internal/adapters/payment"
)

// Provider-backed complaint write actions, run synchronously from the console
// (s.ProviderFlow is available there, exactly as the channel-sharing console
// uses it). Each resolves the complaint's channel adapter the same way the sync
// job does and calls the provider-specific write. The caller gates on
// complain_open and on role scope; funds moves (refund-on-complaint) are NOT
// here — they flow through GOEPAY's existing order refund path, which keeps its
// funds authorization and immutable ledger.
//
// WeChat 消费者投诉2.0: 回复(Response) / 反馈处理完成(Complete) /
//   退款审批结果(UpdateRefundProgress) / 图片上传(UploadImage).
// Alipay 交易投诉: 处理(FeedbackSubmit) / 留言回复(ReplySubmit) /
//   补充凭证(SupplementSubmit).

type complaintChannelRecord struct {
	ChannelID int64
	PayType   string
	ThirdID   string
	OwnerID   *int64
}

// complaintForWrite loads a complaint's provider identity and resolves its
// channel adapter. mid>0 scopes to a merchant owner (the merchant console);
// mid==0 is the admin scope (any owner).
func (g ProviderFlow) complaintForWrite(ctx context.Context, id, mid int64) (complaintChannelRecord, provider.ComplaintAdapter, error) {
	var rec complaintChannelRecord
	if g.Store == nil || id <= 0 {
		return rec, nil, ErrInvalid
	}
	e := g.Store.pool.QueryRow(ctx, `SELECT channel_id,pay_type,third_id,owner_id FROM complaints133 WHERE id=$1 AND ($2::bigint=0 OR owner_id=$2)`, id, mid).Scan(&rec.ChannelID, &rec.PayType, &rec.ThirdID, &rec.OwnerID)
	if e != nil {
		return rec, nil, ErrNotFound
	}
	p := g.Providers[rec.ChannelID]
	if p == nil {
		return rec, nil, provider.ErrComplaintUnsupported
	}
	a, e := provider.ComplaintAdapterFromProvider(p)
	if e != nil {
		return rec, nil, e
	}
	if a.ID() != rec.PayType {
		return rec, nil, ErrConflict
	}
	return rec, a, nil
}

// ReplyComplaint posts a reply to the complainant through the provider.
func (g ProviderFlow) ReplyComplaint(ctx context.Context, id, mid int64, content string, images []string) error {
	if !g.paymentWritesEnabled() {
		return ErrLiveDisabled
	}
	rec, a, e := g.complaintForWrite(ctx, id, mid)
	if e != nil {
		return e
	}
	switch c := a.(type) {
	case *provider.WechatV3Complaint:
		return c.Response(ctx, rec.ThirdID, content, images)
	case *provider.AlipayComplaint:
		return c.ReplySubmit(ctx, rec.ThirdID, content, images)
	default:
		return provider.ErrComplaintUnsupported
	}
}

// CompleteComplaint handles/completes a complaint through the provider. WeChat
// uses 反馈处理完成; Alipay uses 处理(feedback) with a feedback code + content.
func (g ProviderFlow) CompleteComplaint(ctx context.Context, id, mid int64, feedbackCode, content string, images []string) error {
	if !g.paymentWritesEnabled() {
		return ErrLiveDisabled
	}
	rec, a, e := g.complaintForWrite(ctx, id, mid)
	if e != nil {
		return e
	}
	switch c := a.(type) {
	case *provider.WechatV3Complaint:
		if e := c.Complete(ctx, rec.ThirdID); e != nil {
			return e
		}
	case *provider.AlipayComplaint:
		if e := c.FeedbackSubmit(ctx, rec.ThirdID, feedbackCode, content, images); e != nil {
			return e
		}
	default:
		return provider.ErrComplaintUnsupported
	}
	// Advance the local state forward so the Pro list reflects the handling.
	_, e = g.Store.pool.Exec(ctx, `UPDATE complaints133 SET local_state='done',last_seen_at=now() WHERE id=$1 AND local_state<>'done'`, id)
	return e
}

// SupplementComplaint supplies supplementary evidence (Alipay 补充凭证 only).
func (g ProviderFlow) SupplementComplaint(ctx context.Context, id, mid int64, content string, images []string) error {
	if !g.paymentWritesEnabled() {
		return ErrLiveDisabled
	}
	rec, a, e := g.complaintForWrite(ctx, id, mid)
	if e != nil {
		return e
	}
	c, ok := a.(*provider.AlipayComplaint)
	if !ok {
		return provider.ErrComplaintUnsupported
	}
	return c.SupplementSubmit(ctx, rec.ThirdID, content, images)
}

// UploadComplaintImage uploads an image to WeChat V3 and returns its media_id
// (Alipay accepts image ids differently and is not supported here). A caller
// uses the returned media id in a subsequent reply/complete.
func (g ProviderFlow) UploadComplaintImage(ctx context.Context, channelID, mid int64, filename string, data []byte) (string, error) {
	if !g.paymentWritesEnabled() {
		return "", ErrLiveDisabled
	}
	if g.Store == nil || channelID <= 0 {
		return "", ErrInvalid
	}
	p := g.Providers[channelID]
	if p == nil {
		return "", provider.ErrComplaintUnsupported
	}
	a, e := provider.ComplaintAdapterFromProvider(p)
	if e != nil {
		return "", e
	}
	c, ok := a.(*provider.WechatV3Complaint)
	if !ok {
		return "", provider.ErrComplaintUnsupported
	}
	return c.UploadImage(ctx, filename, data)
}
