package payment

import (
	"context"
	"errors"
)

// ComplaintAdapter is the provider-neutral surface the complaint sync + console
// write paths drive. Both the WeChat V3 and Alipay complaint adapters satisfy
// the read half (BatchQuery/Query); provider-specific write actions are
// resolved by a further type assertion to the concrete adapter at the call
// site (WeChat has Response/Complete/UpdateRefundProgress/UploadImage; Alipay
// has FeedbackSubmit/ReplySubmit/SupplementSubmit).
type ComplaintAdapter interface {
	ID() string            // "wxpay" | "alipay"
	ApplicationID() string // bound app id
}

// ErrComplaintUnsupported means the channel's built provider is neither a
// WeChat V3 nor an Alipay provider, so no complaint adapter can be made from it.
var ErrComplaintUnsupported = errors.New("channel provider does not support complaints")

// ComplaintAdapterFromProvider builds a complaint adapter from an ALREADY-BUILT
// payment provider, reusing its verified signing client — exactly how
// ProviderFlow holds a channel's provider in Providers[channelID]. No config is
// re-read and no new signing path is introduced: a *WechatV3 lends its
// wechatV3Client to WechatV3Complaint, an *Alipay lends itself to
// AlipayComplaint. This is the channel->adapter resolution seam: a caller
// resolves the channel's provider the way ProviderFlow does, then hands it here.
func ComplaintAdapterFromProvider(p Provider) (ComplaintAdapter, error) {
	switch v := p.(type) {
	case *WechatV3:
		if v == nil || v.wechatV3Client == nil {
			return nil, ErrComplaintUnsupported
		}
		return &WechatV3Complaint{client: v.wechatV3Client, mchID: v.config.MerchantID}, nil
	case *Alipay:
		if v == nil {
			return nil, ErrComplaintUnsupported
		}
		return &AlipayComplaint{client: v}, nil
	default:
		return nil, ErrComplaintUnsupported
	}
}

// ComplaintBatchQuerier is the common read surface used by the sync job: a
// WeChat adapter queries by yyyy-MM-dd window, an Alipay adapter by status +
// "yyyy-MM-dd HH:mm:ss" window. The sync caller picks the right shape by the
// adapter's ID().
type ComplaintBatchQuerier interface {
	ComplaintAdapter
}

var _ ComplaintBatchQuerier = (*WechatV3Complaint)(nil)
var _ ComplaintBatchQuerier = (*AlipayComplaint)(nil)

// SyncComplaintsPage pulls one page of complaints for a channel's resolved
// adapter and returns the neutral records. It performs provider wire I/O and so
// must run with provider access (the job worker), never in the generic tick.
// beginDate/endDate are yyyy-MM-dd; pageNo starts at 1, pageSize 1..50 (WeChat)
// or 1..20 (Alipay, clamped by the adapter).
func SyncComplaintsPage(ctx context.Context, a ComplaintAdapter, beginDate, endDate string, pageNo, pageSize int) (ComplaintPage, error) {
	switch c := a.(type) {
	case *WechatV3Complaint:
		return c.BatchQuery(ctx, beginDate, endDate, pageNo, pageSize)
	case *AlipayComplaint:
		// Alipay windows are "yyyy-MM-dd HH:mm:ss"; widen the day to full range.
		begin, end := "", ""
		if beginDate != "" {
			begin = beginDate + " 00:00:00"
		}
		if endDate != "" {
			end = endDate + " 23:59:59"
		}
		if pageSize > 20 {
			pageSize = 20
		}
		return c.BatchQuery(ctx, "", begin, end, pageNo, pageSize)
	default:
		return ComplaintPage{}, ErrComplaintUnsupported
	}
}
