package payment

import "context"

// SettlementRecipientSummaries reads a bounded, already-authorized merchant ID
// set. It is a display projection, not an authorization or recipient snapshot.
// Only this store decodes private material; callers receive existing safe masks.
func (s *Store) SettlementRecipientSummaries(ctx context.Context, ids []int64) (map[int64]SettlementRecipientView, error) {
	if s == nil || s.pool == nil || len(ids) < 1 || len(ids) > 100 {
		return nil, ErrInvalid
	}
	views := make(map[int64]SettlementRecipientView, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, ErrInvalid
		}
		if _, duplicate := views[id]; duplicate {
			return nil, ErrInvalid
		}
		views[id] = SettlementRecipientView{State: "missing"}
	}
	// Do not join on merchant identity here: a damaged current pointer to
	// another merchant must become unavailable, not look like no configuration.
	rows, err := s.pool.Query(ctx, `SELECT requested.id,p.config_id,
 COALESCE(c.id,0),COALESCE(c.merchant_id,0),COALESCE(c.recipient_type,''),COALESCE(c.app_id,''),c.identity_hash,c.recipient_cipher
 FROM unnest($1::bigint[]) AS requested(id)
 LEFT JOIN settlement_recipient_current p ON p.merchant_id=requested.id
 LEFT JOIN settlement_recipient_configs c ON c.id=p.config_id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var mid int64
		var currentID *int64
		var c recipientRecord
		if err = rows.Scan(&mid, &currentID, &c.ID, &c.MerchantID, &c.Type, &c.AppID, &c.Hash, &c.Cipher); err != nil {
			return nil, err
		}
		if currentID == nil {
			continue
		}
		views[mid] = SettlementRecipientView{State: "unavailable"}
		if c.ID != *currentID {
			continue
		}
		in, decodeErr := s.decodeSettlementConfig(c, mid)
		if decodeErr == nil {
			views[mid] = recipientView("configured", c.ID, in)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return views, nil
}
