package payment

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/platform/jobs"
	"goepay/internal/refundevent"
)

func (s *Store) refundEvents201() *refundevent.Store {
	return refundevent.New(s.pool, s.indexKey)
}

func (s *Store) refundEventActor201(ctx context.Context, tx pgx.Tx, a MerchantRSAActor) error {
	var enabled bool
	err := tx.QueryRow(ctx, `SELECT enabled FROM merchants WHERE id=$1 FOR UPDATE`, a.User.ID).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) || !enabled {
		return identity.ErrOriginalMerchantSession
	}
	if err != nil {
		return err
	}
	if err = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); err != nil {
		return err
	}
	ready, err := refundevent.Ready(ctx, tx)
	if err != nil {
		return err
	}
	if !ready {
		return refundevent.ErrSource
	}
	return nil
}

func (s *Store) MerchantRefundEventSubscription201(ctx context.Context, a MerchantRSAActor) (refundevent.View, error) {
	var out refundevent.View
	if s == nil || s.pool == nil {
		return out, refundevent.ErrSource
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = s.refundEventActor201(ctx, tx, a); err != nil {
		return out, err
	}
	if out, err = s.refundEvents201().ViewTx(ctx, tx, a.User.ID); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func (s *Store) UpdateMerchantRefundEventSubscription201(ctx context.Context, a MerchantRSAActor, endpoint string, types []string, expected string) (refundevent.View, error) {
	var out refundevent.View
	if s == nil || s.pool == nil {
		return out, refundevent.ErrSource
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = s.refundEventActor201(ctx, tx, a); err != nil {
		return out, err
	}
	if out, err = s.refundEvents201().UpdateTx(ctx, tx, a.User.ID, endpoint, types, expected); err != nil {
		return out, err
	}
	if err = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func (s *Store) insertRefundWithOrigin201(ctx context.Context, tx pgx.Tx, mid, orderID int64, no string, amount, merchantAmount, feeAmount int64, access []refundAdmission179) (Refund, error) {
	ready, err := refundevent.Ready(ctx, tx)
	if err != nil {
		return Refund{}, err
	}
	query := `INSERT INTO refunds(merchant_id,order_id,refund_no,amount,merchant_amount,fee_amount) VALUES($1,$2,$3,$4,$5,$6) RETURNING ` + refundColumns
	args := []any{mid, orderID, no, amount, merchantAmount, feeAmount}
	entry := "platform"
	if len(access) == 1 && access[0].access == refundAPI179 {
		entry = "api"
	}
	if ready {
		query = `INSERT INTO refunds(merchant_id,order_id,refund_no,amount,merchant_amount,fee_amount,event_origin201) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING ` + refundColumns
		args = append(args, entry)
	}
	r, err := scanRefund(tx.QueryRow(ctx, query, args...))
	if err != nil || !ready {
		return r, err
	}
	err = s.refundEvents201().RecordOrigin(ctx, tx, refundevent.Origin{RefundID: r.ID, MerchantID: r.MerchantID, OrderID: r.OrderID, Amount: r.Amount, ExternalNo: r.RefundNo, Entry: entry})
	return r, err
}

func (s *Store) refundOrigin201(ctx context.Context, tx pgx.Tx, r Refund) (refundevent.Origin, []byte, error) {
	var origin refundevent.Origin
	var entry *string
	if err := tx.QueryRow(ctx, `SELECT event_origin201 FROM refunds WHERE id=$1`, r.ID).Scan(&entry); err != nil {
		return origin, nil, err
	}
	if entry == nil {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchant_refund_origins201 WHERE refund_id=$1)`, r.ID).Scan(&exists); err != nil {
			return origin, nil, err
		}
		if exists {
			return origin, nil, refundevent.ErrSource
		}
		return origin, nil, nil
	}
	origin = refundevent.Origin{RefundID: r.ID, MerchantID: r.MerchantID, OrderID: r.OrderID, Amount: r.Amount, ExternalNo: r.RefundNo, Entry: *entry}
	return s.refundEvents201().ReadOrigin(ctx, tx, origin)
}

func (s *Store) verifyRefundOrigin201(ctx context.Context, tx pgx.Tx, r Refund) error {
	ready, err := refundevent.Ready(ctx, tx)
	if err != nil || !ready {
		return err
	}
	_, _, err = s.refundOrigin201(ctx, tx, r)
	return err
}

func (s *Store) captureRefundEvent201(ctx context.Context, tx pgx.Tx, r Refund, p providerRefundResultEvidence, resultProof []byte) error {
	if p.State != "succeeded" {
		return nil
	}
	ready, err := refundevent.Ready(ctx, tx)
	if err != nil || !ready {
		return err
	}
	origin, _, err := s.refundOrigin201(ctx, tx, r)
	if err != nil {
		return err
	}
	if origin.RefundID == 0 || origin.Entry == "api" {
		return nil
	}
	o, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE id=$1`, r.OrderID))
	if err != nil {
		return err
	}
	var cumulative int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM refunds WHERE order_id=$1 AND status='succeeded'`, r.OrderID).Scan(&cumulative); err != nil {
		return err
	}
	content := struct {
		Code            int         `json:"code"`
		RefundNo        string      `json:"refund_no"`
		ExternalNo      string      `json:"out_refund_no"`
		TradeNo         string      `json:"trade_no"`
		ExternalTradeNo string      `json:"out_trade_no"`
		UID             json.Number `json:"uid"`
		Money           json.Number `json:"money"`
		RefundMoney     json.Number `json:"refundmoney"`
		ReduceMoney     json.Number `json:"reducemoney"`
	}{Code: 0, RefundNo: fmt.Sprintf("RF%d", r.ID), ExternalNo: r.RefundNo, TradeNo: o.TradeNo, ExternalTradeNo: o.OutTradeNo, UID: json.Number(fmt.Sprint(r.MerchantID)), Money: json.Number(ledger.Format(r.Amount)), RefundMoney: json.Number(ledger.Format(cumulative)), ReduceMoney: json.Number(ledger.Format(r.MerchantAmount))}
	// encoding/json already keeps Unicode and slashes literal. This encoder
	// also preserves PHP's original literal ampersands and angle brackets.
	raw, err := refundEventJSON201(content)
	if err != nil {
		return err
	}
	return s.refundEvents201().Capture(ctx, tx, origin, resultProof, raw)
}

func refundEventJSON201(value any) ([]byte, error) {
	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false)
	if err := e.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte{'\n'}), nil
}

func (s *Store) verifyRefundEvent201(ctx context.Context, tx pgx.Tx, event refundevent.Event) error {
	r, err := scanRefund(tx.QueryRow(ctx, `SELECT `+refundColumns+` FROM refunds WHERE id=$1`, event.RefundID))
	if err != nil {
		return err
	}
	if r.Status != "succeeded" || r.MerchantID != event.MerchantID || r.OrderID != event.OrderID {
		return refundevent.ErrSource
	}
	if err = s.verifyReferralRefundEvidence(ctx, tx, r); err != nil {
		return err
	}
	if err = s.verifyProviderRefundResult(ctx, tx, r); err != nil {
		return err
	}
	origin, proof, err := s.refundOrigin201(ctx, tx, r)
	if err != nil {
		return err
	}
	if origin.Entry != "platform" || event.OriginProof != hex.EncodeToString(proof) {
		return refundevent.ErrSource
	}
	if err = tx.QueryRow(ctx, `SELECT proof_hash FROM provider_refund_result_evidence WHERE refund_id=$1`, r.ID).Scan(&proof); err != nil {
		return err
	}
	if event.ResultProof != hex.EncodeToString(proof) {
		return refundevent.ErrSource
	}
	return nil
}

func (s *Store) DeliverMerchantRefundEvent201(ctx context.Context, j jobs.Job, key *rsa.PrivateKey, client *http.Client) error {
	if s == nil || s.pool == nil {
		return refundevent.ErrSource
	}
	return s.refundEvents201().Deliver(ctx, j, key, client, s.verifyRefundEvent201)
}

func (s *Store) DeliverMerchantRefundEventClosedNotice201(ctx context.Context, j jobs.Job, sender refundevent.NoticeSender) error {
	if s == nil || s.pool == nil {
		return refundevent.ErrSource
	}
	return s.refundEvents201().DeliverClosedNotice(ctx, j, sender, s.verifyRefundEvent201)
}
