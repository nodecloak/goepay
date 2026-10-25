package payment

import "context"

type TransferBatchList struct {
	Items        []TransferBatch `json:"items"`
	Total        int64           `json:"total"`
	Limit        int             `json:"limit"`
	NextBeforeID int64           `json:"next_before_id,string"`
}

// ListTransferBatches uses an exact external number for recovery after a lost
// prepare response. It never reads a different merchant's batch or calls a provider.
func (s *Store) ListTransferBatches(ctx context.Context, mid int64, externalNo string, beforeID int64, limit int) (TransferBatchList, error) {
	out := TransferBatchList{Items: []TransferBatch{}, Limit: limit}
	if mid <= 0 || beforeID < 0 || limit < 1 || limit > 100 || len(externalNo) > 128 {
		return out, ErrInvalid
	}
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM transfer_batches WHERE merchant_id=$1 AND ($2::text='' OR external_no=$2)", mid, externalNo).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := s.pool.Query(ctx, "SELECT id FROM transfer_batches WHERE merchant_id=$1 AND ($2::text='' OR external_no=$2) AND ($3::bigint=0 OR id<$3) ORDER BY id DESC LIMIT $4", mid, externalNo, beforeID, limit+1)
	if e != nil {
		return out, e
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(ids) > limit {
		ids = ids[:limit]
		out.NextBeforeID = ids[len(ids)-1]
	}
	for _, id := range ids {
		b, e := s.TransferBatch(ctx, mid, id)
		if e != nil {
			return out, e
		}
		b.Items = []TransferBatchItem{}
		out.Items = append(out.Items, b)
	}
	return out, nil
}
