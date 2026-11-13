package cryptopay

import (
	"context"
	"github.com/jackc/pgx/v5"
)

const withdrawalColumns = "id,merchant_id,kind,request_id,network,address,amount,state,tx_id,COALESCE(invoice_id,''),approved_by,source,cancel_requested"

func scanWithdrawal(row pgx.Row) (Withdrawal, error) {
	var v Withdrawal
	var n int64
	e := row.Scan(&v.ID, &v.MerchantID, &v.Kind, &v.RequestID, &v.Network, &v.Address, &n, &v.State, &v.TxID, &v.InvoiceID, &v.ApprovedBy, &v.Source, &v.CancellationRequested)
	if e == pgx.ErrNoRows {
		return v, ErrNotFound
	}
	v.Amount = FormatAmount(n)
	return v, e
}
func (s *Service) RequestWithdrawal(ctx context.Context, in WithdrawalInput) (Withdrawal, error) {
	return s.requestOutgoing(ctx, in, "settlement", "")
}
func (s *Service) Refund(ctx context.Context, in RefundInput) (Withdrawal, error) {
	v, e := s.GetInvoice(ctx, in.MerchantID, in.InvoiceID)
	if e != nil {
		return Withdrawal{}, e
	}
	return s.requestOutgoing(ctx, WithdrawalInput{in.MerchantID, in.RequestID, v.Network, in.Address, in.Amount}, "refund", v.ID)
}
func (s *Service) RefundHeldFunds(ctx context.Context, in RefundInput) (Withdrawal, error) {
	v, e := s.GetInvoice(ctx, in.MerchantID, in.InvoiceID)
	if e != nil {
		return Withdrawal{}, e
	}
	return s.requestOutgoing(ctx, WithdrawalInput{in.MerchantID, in.RequestID, v.Network, in.Address, in.Amount}, "exception_refund", v.ID)
}
func (s *Service) requestOutgoing(ctx context.Context, in WithdrawalInput, kind, invoice string) (Withdrawal, error) {
	var out Withdrawal
	n, e := ParseAmount(in.Amount)
	a, ae := normalize(in.Network, in.Address)
	cfg, ok := s.Networks[in.Network]
	if e != nil || ae != nil || !ok || !cfg.Enabled || in.MerchantID <= 0 || !validKey(in.RequestID) {
		return out, ErrInvalid
	}
	source := cfg.HotAddress
	if source != "" {
		source, e = normalize(in.Network, source)
		if e != nil {
			return out, ErrInvalid
		}
	}
	tx, e := s.begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = lockMerchant(ctx, tx, in.MerchantID); e != nil {
		return out, e
	}
	out, e = scanWithdrawal(tx.QueryRow(ctx, "SELECT "+withdrawalColumns+" FROM crypto_withdrawals WHERE merchant_id=$1 AND request_id=$2", in.MerchantID, in.RequestID))
	if e == nil {
		if out.Network != in.Network || out.Address != a || out.Amount != FormatAmount(n) || out.Kind != kind || out.InvoiceID != invoice {
			return Withdrawal{}, ErrConflict
		}
		return out, commit(ctx, tx)
	}
	if e != ErrNotFound {
		return out, e
	}
	var allowed bool
	e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM crypto_recipients WHERE merchant_id=$1 AND network=$2 AND address=$3 AND state='approved' AND usable_at<=clock_timestamp())", in.MerchantID, in.Network, a).Scan(&allowed)
	if e != nil {
		return out, e
	}
	if !allowed {
		return out, ErrConflict
	}
	if kind == "refund" {
		var paid bool
		var amount, total int64
		e = tx.QueryRow(ctx, "SELECT credited,amount FROM crypto_invoices WHERE id=$1 AND merchant_id=$2 AND network=$3 FOR UPDATE", invoice, in.MerchantID, in.Network).Scan(&paid, &amount)
		if e != nil || !paid {
			return out, ErrConflict
		}
		e = tx.QueryRow(ctx, "SELECT COALESCE(sum(amount),0)::bigint FROM crypto_withdrawals WHERE invoice_id=$1 AND kind='refund' AND state NOT IN ('cancelled','failed')", invoice).Scan(&total)
		if e != nil {
			return out, e
		}
		if n > amount-total {
			return out, ErrInvalid
		}
	}
	id := randomID()
	if kind == "exception_refund" {
		var received, amount, refunded int64
		var credited bool
		e = tx.QueryRow(ctx, "SELECT received,amount,credited FROM crypto_invoices WHERE id=$1 AND merchant_id=$2 AND network=$3 FOR UPDATE", invoice, in.MerchantID, in.Network).Scan(&received, &amount, &credited)
		if e != nil {
			return out, e
		}
		e = tx.QueryRow(ctx, "SELECT COALESCE(sum(amount),0)::bigint FROM crypto_withdrawals WHERE invoice_id=$1 AND kind='exception_refund' AND state NOT IN ('cancelled','failed')", invoice).Scan(&refunded)
		if e != nil {
			return out, e
		}
		held := received - refunded
		if credited {
			held -= amount
		}
		if n > held {
			return out, ErrInsufficient
		}
		if e = post(ctx, tx, in.Network, "reserve:"+id, posting{0, "suspense", -n}, posting{in.MerchantID, "reserved", n}); e != nil {
			return out, e
		}
		if !credited {
			if _, e = tx.Exec(ctx, "UPDATE crypto_invoices SET state='late' WHERE id=$1", invoice); e != nil {
				return out, e
			}
		}
	} else if e = reserve(ctx, tx, in.Network, in.MerchantID, n, id); e != nil {
		return out, e
	}
	var inv any
	if invoice != "" {
		inv = invoice
	}
	_, e = tx.Exec(ctx, "INSERT INTO crypto_withdrawals(id,merchant_id,request_id,kind,invoice_id,network,address,amount,source) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", id, in.MerchantID, in.RequestID, kind, inv, in.Network, a, n, source)
	if e != nil {
		return out, e
	}
	out, e = scanWithdrawal(tx.QueryRow(ctx, "SELECT "+withdrawalColumns+" FROM crypto_withdrawals WHERE id=$1", id))
	if e != nil {
		return out, e
	}
	return out, commit(ctx, tx)
}
func (s *Service) ListWithdrawals(ctx context.Context, merchant int64) ([]Withdrawal, error) {
	if s == nil || s.Pool == nil {
		return nil, ErrUnavailable
	}
	rows, e := s.Pool.Query(ctx, "SELECT "+withdrawalColumns+" FROM crypto_withdrawals WHERE ($1::bigint=0 OR merchant_id=$1) ORDER BY created_at DESC LIMIT 200", merchant)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Withdrawal{}
	for rows.Next() {
		v, e := scanWithdrawal(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Service) GetWithdrawal(ctx context.Context, id string) (Withdrawal, error) {
	return scanWithdrawal(s.Pool.QueryRow(ctx, "SELECT "+withdrawalColumns+" FROM crypto_withdrawals WHERE id=$1", id))
}
func (s *Service) FindWithdrawal(ctx context.Context, merchant int64, requestID string) (Withdrawal, error) {
	if s == nil || s.Pool == nil || merchant <= 0 || !validKey(requestID) {
		return Withdrawal{}, ErrInvalid
	}
	return scanWithdrawal(s.Pool.QueryRow(ctx, "SELECT "+withdrawalColumns+" FROM crypto_withdrawals WHERE merchant_id=$1 AND request_id=$2", merchant, requestID))
}
func (s *Service) ApproveWithdrawal(ctx context.Context, id string, actor int64) (Withdrawal, error) {
	var out Withdrawal
	if actor <= 0 {
		return out, ErrInvalid
	}
	tx, e := s.begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	out, e = scanWithdrawal(tx.QueryRow(ctx, "SELECT "+withdrawalColumns+" FROM crypto_withdrawals WHERE id=$1 FOR UPDATE", id))
	if e != nil {
		return out, e
	}
	if out.State == "approved" {
		return out, commit(ctx, tx)
	}
	if out.State != "requested" {
		return out, ErrConflict
	}
	if _, e = tx.Exec(ctx, "UPDATE crypto_withdrawals SET state='approved',approved_by=$2 WHERE id=$1", id, actor); e != nil {
		return out, e
	}
	if e = audit(ctx, tx, actor, "approve", id); e != nil {
		return out, e
	}
	out.State = "approved"
	out.ApprovedBy = actor
	return out, commit(ctx, tx)
}
func (s *Service) CancelWithdrawal(ctx context.Context, merchant int64, id string) (Withdrawal, error) {
	tx, e := s.begin(ctx)
	if e != nil {
		return Withdrawal{}, e
	}
	defer tx.Rollback(ctx)
	if e = lockMerchant(ctx, tx, merchant); e != nil {
		return Withdrawal{}, e
	}
	v, e := scanWithdrawal(tx.QueryRow(ctx, "SELECT "+withdrawalColumns+" FROM crypto_withdrawals WHERE id=$1 AND merchant_id=$2 FOR UPDATE", id, merchant))
	if e != nil {
		return v, e
	}
	if v.State == "cancelled" {
		return v, commit(ctx, tx)
	}
	if v.Kind == "sweep" {
		return v, ErrConflict
	}
	if v.State == "approved" || v.State == "preparing" || v.State == "prepared" || v.State == "signing" {
		if _, e = tx.Exec(ctx, "UPDATE crypto_withdrawals SET cancel_requested=true WHERE id=$1", id); e != nil {
			return v, e
		}
		v.CancellationRequested = true
		return v, commit(ctx, tx)
	}
	if v.State != "requested" {
		return v, ErrConflict
	}
	n, _ := ParseAmount(v.Amount)
	credit := posting{merchant, "available", n}
	if v.Kind == "exception_refund" {
		credit = posting{0, "suspense", n}
	}
	if e = post(ctx, tx, v.Network, "cancel:"+id, posting{merchant, "reserved", -n}, credit); e != nil {
		return v, e
	}
	if _, e = tx.Exec(ctx, "UPDATE crypto_withdrawals SET state='cancelled' WHERE id=$1", id); e != nil {
		return v, e
	}
	v.State = "cancelled"
	return v, commit(ctx, tx)
}
