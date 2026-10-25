package payment

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// FundsChannelSharingReceiverView uses the authorization transaction's snapshot
// and authenticated decryption. The caller must authorize view.MerchantID before
// including this masked identity in a financial action preview.
func (s *Store) FundsChannelSharingReceiverView(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id int64) (ChannelSharingReceiverView, error) {
	if s == nil || q == nil || id <= 0 {
		return ChannelSharingReceiverView{}, ErrInvalid
	}
	r, e := scanSharingReceiver(q.QueryRow(ctx, "SELECT "+sharingReceiverColumns+" FROM channel_sharing_receivers WHERE id=$1", id))
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	input, e := s.openSharingReceiverRecord(r)
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	return sharingReceiverView(r, input), nil
}
