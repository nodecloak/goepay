package cryptopay

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

type Recipient struct {
	ID         string    `json:"id"`
	MerchantID int64     `json:"merchant_id,string"`
	Network    string    `json:"network"`
	Address    string    `json:"address"`
	State      string    `json:"state"`
	UsableAt   time.Time `json:"usable_at"`
}

func scanRecipient(row pgx.Row) (Recipient, error) {
	var r Recipient
	e := row.Scan(&r.ID, &r.MerchantID, &r.Network, &r.Address, &r.State, &r.UsableAt)
	if e == pgx.ErrNoRows {
		return r, ErrNotFound
	}
	return r, e
}

const recipientColumns = "id,merchant_id,network,address,state,usable_at"

func (s *Service) RequestRecipient(ctx context.Context, merchant int64, network, address string) (Recipient, error) {
	var r Recipient
	a, e := normalize(network, address)
	if e != nil || !s.Networks[network].Enabled {
		return r, ErrInvalid
	}
	tx, e := s.begin(ctx)
	if e != nil {
		return r, e
	}
	defer tx.Rollback(ctx)
	if e = lockMerchant(ctx, tx, merchant); e != nil {
		return r, e
	}
	r, e = scanRecipient(tx.QueryRow(ctx, "SELECT "+recipientColumns+" FROM crypto_recipients WHERE merchant_id=$1 AND network=$2 AND address=$3", merchant, network, a))
	if e == nil {
		if r.State == "revoked" {
			return r, ErrConflict
		}
		return r, commit(ctx, tx)
	}
	if e != ErrNotFound {
		return r, e
	}
	if s.ManageNetworks {
		if e = s.requireNetworkEnabled(ctx, tx, network, true); e != nil {
			return r, e
		}
	}
	id := randomID()
	r, e = scanRecipient(tx.QueryRow(ctx, "INSERT INTO crypto_recipients(id,merchant_id,network,address) VALUES($1,$2,$3,$4) RETURNING "+recipientColumns, id, merchant, network, a))
	if e != nil {
		return r, e
	}
	return r, s.commitNetworkOperation(ctx, tx, network)
}
func (s *Service) ListRecipients(ctx context.Context, merchant int64) ([]Recipient, error) {
	if s == nil || s.Pool == nil {
		return nil, ErrUnavailable
	}
	rows, e := s.Pool.Query(ctx, "SELECT "+recipientColumns+" FROM crypto_recipients WHERE ($1::bigint=0 OR merchant_id=$1) ORDER BY usable_at DESC LIMIT 200", merchant)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Recipient{}
	for rows.Next() {
		r, e := scanRecipient(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Service) ApproveRecipient(ctx context.Context, id string, actor int64) (Recipient, error) {
	var r Recipient
	if actor <= 0 {
		return r, ErrInvalid
	}
	tx, e := s.begin(ctx)
	if e != nil {
		return r, e
	}
	defer tx.Rollback(ctx)
	r, e = scanRecipient(tx.QueryRow(ctx, "SELECT "+recipientColumns+" FROM crypto_recipients WHERE id=$1 FOR UPDATE", id))
	if e != nil {
		return r, e
	}
	if r.State == "revoked" {
		return r, ErrConflict
	}
	if _, e = tx.Exec(ctx, "UPDATE crypto_recipients SET state='approved',approved_by=$2 WHERE id=$1", id, actor); e != nil {
		return r, e
	}
	if e = audit(ctx, tx, actor, "approve-recipient", id); e != nil {
		return r, e
	}
	r.State = "approved"
	return r, commit(ctx, tx)
}
func (s *Service) RevokeRecipient(ctx context.Context, id string, merchant int64) error {
	tx, e := s.begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, "UPDATE crypto_recipients SET state='revoked' WHERE id=$1 AND merchant_id=$2", id, merchant)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return commit(ctx, tx)
}
