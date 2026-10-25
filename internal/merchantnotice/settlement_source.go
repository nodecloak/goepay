package merchantnotice

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

type settlementEventSource struct {
	RequestID, TransferID, MerchantID, ChannelID int64
	Gross, Net, Fee, JournalID                   int64
	Provider, AppID, ProviderNo, Transaction     string
	RecipientHash                                []byte
	At                                           time.Time
}

func settlementSource(ctx context.Context, tx pgx.Tx, id int64) (settlementEventSource, error) {
	var p settlementEventSource
	err := tx.QueryRow(ctx, `SELECT p.request_id,p.transfer_id,r.merchant_id,p.channel_id,p.gross_amount,p.net_amount,p.fee_amount,p.provider_kind,p.app_id,p.recipient_hash,a.provider_transfer_no,j.id,j.transaction_id::text,j.created_at
 FROM settlement_payouts p JOIN ledger_requests r ON r.id=p.request_id
 JOIN provider_transfer_attempts a ON a.transfer_id=p.transfer_id AND a.channel_id=p.channel_id
 JOIN journals j ON j.event_key='settlement-payout:'||p.request_id::text||':principal'
 WHERE p.request_id=$1 AND p.send_state='claimed' AND a.provider_transfer_no<>''`, id).Scan(&p.RequestID, &p.TransferID, &p.MerchantID, &p.ChannelID, &p.Gross, &p.Net, &p.Fee, &p.Provider, &p.AppID, &p.RecipientHash, &p.ProviderNo, &p.JournalID, &p.Transaction, &p.At)
	p.At = p.At.UTC()
	return p, err
}

// The payment domain has authenticated and decrypted the ORIGINAL request's
// recipient. This publisher additionally requires the first net-principal
// journal and successful provider receipt in the same transaction. Queries of
// historical successful payouts cannot manufacture another source.
func (s *Store) CaptureSettlement(ctx context.Context, tx pgx.Tx, transferID int64, account string) error {
	if !s.available() || tx == nil || transferID <= 0 || !safeValue(account, 128, false) {
		return ErrInvalid
	}
	var id int64
	if err := tx.QueryRow(ctx, `SELECT p.request_id FROM settlement_payouts p JOIN transfers t ON t.id=p.transfer_id JOIN provider_transfer_attempts a ON a.transfer_id=t.id WHERE t.id=$1 AND t.status='succeeded' AND a.state='succeeded'`, transferID).Scan(&id); err != nil {
		return ErrState
	}
	p, err := settlementSource(ctx, tx, id)
	if err != nil {
		return err
	}
	var current string
	if err = tx.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&current); err != nil {
		return err
	}
	if current != p.Transaction {
		return ErrConflict
	}
	in := Input{Scene: "settle", At: p.At, Params: map[string]string{"money": money(p.Gross), "realmoney": money(p.Net), "account": account, "time": p.At.In(time.FixedZone("UTC+8", 28800)).Format("2006-01-02 15:04:05")}}
	return s.appendEvent(ctx, tx, eventRecord{MerchantID: p.MerchantID, SettlementID: id, Scene: "settle"}, eventData{Version: 1, Settlement: &p, Input: in}, true)
}

func (s *Store) verifySettlementSource(ctx context.Context, tx pgx.Tx, r eventRecord, data eventData) error {
	if r.SettlementID <= 0 || r.OrderID != 0 || r.LoginAuditID != 0 || data.Settlement == nil || data.Login != nil || data.Source != (paidEventSource{}) {
		return ErrConflict
	}
	p, err := settlementSource(ctx, tx, r.SettlementID)
	if err != nil || p.MerchantID != r.MerchantID || p.Transaction != r.Transaction {
		return ErrConflict
	}
	a, _ := json.Marshal(p)
	b, _ := json.Marshal(data.Settlement)
	if !hmac.Equal(a, b) {
		return ErrConflict
	}
	return nil
}
