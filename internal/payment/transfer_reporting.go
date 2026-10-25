package payment

import (
	"context"
	"fmt"
	"time"
)

// TransferReportFact deliberately contains no recipient, ciphertext or provider
// credentials. Type is authenticated from the original intent, never inferred
// from a channel's current configuration. The immutable resolution time is the
// business date; later queries cannot move historical success into today.
type TransferReportFact struct {
	MerchantID, ChannelID, Amount int64
	Type                          string
}

func (s *Store) TransferReportingFacts(ctx context.Context, from, until time.Time, limit int) ([]TransferReportFact, error) {
	if limit < 1 || limit > 50001 {
		return nil, ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT t.id,t.merchant_id,t.amount,COALESCE(a.channel_id,0),a.recipient_cipher
 FROM transfers t LEFT JOIN provider_transfer_attempts a ON a.transfer_id=t.id
 WHERE t.status='succeeded' AND t.resolved_at>=$1 AND t.resolved_at<$2
 AND NOT EXISTS(SELECT 1 FROM bank_transfer_lifecycle b WHERE b.transfer_id=t.id AND b.state IN('reversed','reversal_pending'))
 ORDER BY t.id LIMIT $3`, from, until, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	facts := []TransferReportFact{}
	for rows.Next() {
		var f TransferReportFact
		var id int64
		var cipher []byte
		if err = rows.Scan(&id, &f.MerchantID, &f.Amount, &f.ChannelID, &cipher); err != nil {
			return nil, err
		}
		f.Type = "unknown"
		if len(cipher) > 0 {
			intent, e := s.decryptTransfer(id, cipher)
			if e != nil || intent.TransferNo != fmt.Sprintf("TF%d", id) || intent.Amount != f.Amount {
				return nil, ErrConflict
			}
			switch intent.Type {
			case "alipay", "wxpay", "bank":
				f.Type = intent.Type
			default:
				return nil, ErrInvalid
			}
		}
		facts = append(facts, f)
	}
	return facts, rows.Err()
}
