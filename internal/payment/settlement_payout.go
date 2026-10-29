package payment

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/merchantpolicy"
	"goepay/internal/privatesearch"
	"time"
)

type SettlementPayout struct {
	RequestID           int64                    `json:"request_id,string"`
	MerchantID          int64                    `json:"merchant_id,string"`
	ChannelID           int64                    `json:"channel_id,string"`
	TransferID          int64                    `json:"transfer_id,string"`
	SendState           string                   `json:"send_state"`
	State               string                   `json:"state"`
	Transfer            Transfer                 `json:"transfer"`
	Funding             *SettlementFunding       `json:"funding"`
	Recipient           *SettlementRecipientView `json:"recipient"`
	CreatedAt           time.Time                `json:"created_at"`
	ProviderKind, AppID string                   `json:"-"`
}

func payoutActor(a TransferBatchActor, mid int64) bool { return a.valid(mid) && a.Role == "admin" }
func (s *Store) settlementPayout(ctx context.Context, q fundsReader, id int64) (SettlementPayout, error) {
	var p SettlementPayout
	if e := s.verifyScheduledRequestSource(ctx, q, id); e != nil {
		return p, e
	}
	e := q.QueryRow(ctx, "SELECT p.request_id,r.merchant_id,p.channel_id,p.transfer_id,p.send_state,p.provider_kind,p.app_id,p.created_at FROM settlement_payouts p JOIN ledger_requests r ON r.id=p.request_id WHERE p.request_id=$1", id).Scan(&p.RequestID, &p.MerchantID, &p.ChannelID, &p.TransferID, &p.SendState, &p.ProviderKind, &p.AppID, &p.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	if e != nil {
		return p, e
	}
	p.Transfer, e = s.scanTransfer(q.QueryRow(ctx, "SELECT "+transferColumns+transferFrom+" WHERE t.id=$1", p.TransferID))
	if e != nil {
		return p, e
	}
	p.State = p.Transfer.Status
	if p.SendState != "claimed" {
		p.State = p.SendState
		p.Transfer.Status = p.SendState
	}
	v, e := scanLedgerRequest(q.QueryRow(ctx, "SELECT "+requestColumns+" FROM ledger_requests WHERE id=$1", id))
	if e != nil {
		return p, e
	}
	v, e = attachSettlementFunding(ctx, q, v)
	if e != nil {
		return p, e
	}
	v, e = s.attachSettlementRecipient(ctx, q, v)
	p.Funding, p.Recipient = v.Funding, v.SettlementRecipient
	if e == nil {
		e = s.verifySettlementAutoPayout(ctx, q, id)
	}
	return p, e
}
func (s *Store) SettlementPayout(ctx context.Context, id int64) (SettlementPayout, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return SettlementPayout{}, e
	}
	defer tx.Rollback(ctx)
	p, e := s.settlementPayout(ctx, tx, id)
	if e != nil {
		return p, e
	}
	return p, tx.Commit(ctx)
}
func (s *Store) lockSettlementRequest(ctx context.Context, tx pgx.Tx, id int64) (LedgerRequest, []byte, error) {
	var mid int64
	var profile []byte
	e := tx.QueryRow(ctx, "SELECT merchant_id FROM ledger_requests WHERE id=$1 AND kind='settlements'", id).Scan(&mid)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	if e != nil {
		return LedgerRequest{}, profile, e
	}
	if e = tx.QueryRow(ctx, "SELECT profile FROM merchants WHERE id=$1 FOR NO KEY UPDATE", mid).Scan(&profile); e != nil {
		return LedgerRequest{}, profile, e
	}
	v, e := scanLedgerRequest(tx.QueryRow(ctx, "SELECT "+requestColumns+" FROM ledger_requests WHERE id=$1 AND kind='settlements' FOR UPDATE", id))
	if e == nil {
		e = s.verifyScheduledRequestSource(ctx, tx, v.ID)
	}
	return v, profile, e
}
func (s *Store) settlementWritePolicy(ctx context.Context, tx pgx.Tx, mid int64, profile []byte) error {
	settle, e := merchantpolicy.ResolveSettlementPermission(profile)
	if e != nil {
		return e
	}
	if !settle {
		return ErrState
	}
	var active bool
	if e := tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", mid).Scan(&active); e != nil {
		return e
	}
	var p struct {
		Enabled bool `json:"transfer_api_enabled"`
	}
	if !active || json.Unmarshal(profile, &p) != nil || !p.Enabled {
		return ErrState
	}
	policy, e := readTransferPolicy(ctx, tx)
	if e != nil {
		return e
	}
	if !policy.Enabled {
		return ErrState
	}
	return nil
}
func (s *Store) originalSettlementRecipient(ctx context.Context, q fundsReader, id int64) (SettlementRecipientInput, []byte, error) {
	var in SettlementRecipientInput
	var hash, cipher []byte
	e := q.QueryRow(ctx, "SELECT identity_hash,recipient_cipher FROM settlement_recipients WHERE request_id=$1", id).Scan(&hash, &cipher)
	if errors.Is(e, pgx.ErrNoRows) {
		return in, nil, ErrState
	}
	if e != nil {
		return in, nil, e
	}
	in, e = s.decryptSettlementRecipient("request", id, cipher)
	if e != nil {
		return in, nil, e
	}
	digest, e := s.settlementRecipientHash(in)
	if e != nil {
		return in, nil, e
	}
	if !hmac.Equal(hash, digest) {
		return in, nil, ErrConflict
	}
	return in, hash, nil
}
func (g ProviderFlow) settlementProvider(channel int64, recipient SettlementRecipientInput, req adapter.TransferRequest) (string, string, error) {
	p := g.Providers[channel]
	if p == nil {
		return "", "", adapter.ErrUnsupported
	}
	if recipient.Type != "wxpay" && recipient.Type != "alipay" && recipient.Type != "bank" {
		return "", "", adapter.ErrUnsupported
	}
	if recipient.Type != "bank" && p.ID() != recipient.Type {
		return "", "", ErrConflict
	}
	if recipient.Type == "bank" {
		product, ok := p.(adapter.BankTransferProduct)
		if !ok || !(p.ID() == "wxpay" && product.BankTransferProduct() == adapter.BankProductWechat || p.ID() == "alipay" && product.BankTransferProduct() == adapter.BankProductAlipay) {
			return "", "", adapter.ErrUnsupported
		}
	}
	if _, ok := p.(adapter.TransferProvider); !ok {
		return "", "", adapter.ErrUnsupported
	}
	identity, ok := p.(adapter.ApplicationIdentity)
	if !ok || identity.ApplicationID() == "" {
		return "", "", adapter.ErrUnsupported
	}
	app := identity.ApplicationID()
	if recipient.AppID != "" && recipient.AppID != app {
		return "", "", ErrConflict
	}
	caps, ok := p.(adapter.TransferCapabilities)
	if !ok {
		return "", "", adapter.ErrUnsupported
	}
	found := false
	for _, typ := range caps.TransferTypes() {
		if typ == recipient.Type {
			found = true
		}
	}
	if !found {
		return "", "", adapter.ErrUnsupported
	}
	if validator, ok := p.(adapter.TransferValidator); ok {
		if e := validator.ValidateTransfer(req); e != nil {
			return "", "", e
		}
	}
	return p.ID(), app, nil
}
func payoutEvent(ctx context.Context, tx pgx.Tx, id int64, action, state string, actor TransferBatchActor) error {
	if _, e := tx.Exec(ctx, "INSERT INTO settlement_payout_events(request_id,action,actor_role,actor_id,actor_name,result_state) VALUES($1,$2,$3,$4,$5,$6)", id, action, actor.Role, actor.ID, actor.Name, state); e != nil {
		return e
	}
	raw, _ := json.Marshal(map[string]string{"request_id": fmt.Sprint(id), "state": state})
	_, e := tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'settlement-payouts',$4,$5,$6)", actor.Role, actor.ID, actor.Name, id, "payout-"+action, raw)
	return e
}
func (g ProviderFlow) PrepareSettlementPayout(ctx context.Context, id, channel int64, actor TransferBatchActor, check TransferBatchCheck) (SettlementPayout, error) {
	if g.Store == nil || id <= 0 || channel <= 0 {
		return SettlementPayout{}, ErrInvalid
	}
	if !g.paymentWritesEnabled() {
		return SettlementPayout{}, ErrLiveDisabled
	}
	resolved, resolveErr := g.parentSettlementRuntime(ctx, id, channel)
	if resolveErr != nil {
		return SettlementPayout{}, resolveErr
	}
	g = resolved
	tx, e := g.Store.pool.Begin(ctx)
	if e != nil {
		return SettlementPayout{}, e
	}
	defer tx.Rollback(ctx)
	p, e := g.prepareSettlementPayoutTx(ctx, tx, id, channel, actor, check, false)
	if e != nil {
		return p, e
	}
	if e = tx.Commit(ctx); e != nil {
		return p, e
	}
	return g.Store.SettlementPayout(ctx, id)
}

func (g ProviderFlow) prepareSettlementPayoutTx(ctx context.Context, tx pgx.Tx, id, channel int64, actor TransferBatchActor, check TransferBatchCheck, automatic bool) (SettlementPayout, error) {
	v, profile, e := g.Store.lockSettlementRequest(ctx, tx, id)
	if e != nil {
		return SettlementPayout{}, e
	}
	if !payoutActor(actor, v.MerchantID) && !(automatic && settlementAutoActorFor(ctx, actor, id)) {
		return SettlementPayout{}, ErrInvalid
	}
	old, e := g.Store.settlementPayout(ctx, tx, id)
	if e == nil {
		if old.ChannelID != channel {
			return old, ErrConflict
		}
		return old, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return old, e
	}
	if e = g.prepareParentTransferChannelTx(ctx, tx, v.MerchantID, channel); e != nil {
		return SettlementPayout{}, e
	}
	if v.Status != "approved" {
		return SettlementPayout{}, ErrState
	}
	v, e = attachSettlementTerms(ctx, tx, v)
	if e != nil {
		return SettlementPayout{}, e
	}
	if v.Settlement == nil || v.Funding == nil || v.Funding.State != "reserved" || v.Funding.ReservedAmount != v.Amount {
		return SettlementPayout{}, ErrState
	}
	var valid bool
	if e = tx.QueryRow(ctx, "SELECT valid FROM settlement_funding_invariants WHERE request_id=$1", id).Scan(&valid); e != nil {
		return SettlementPayout{}, e
	}
	if !valid {
		return SettlementPayout{}, ErrState
	}
	in, hash, e := g.Store.originalSettlementRecipient(ctx, tx, id)
	if e != nil {
		return SettlementPayout{}, e
	}
	req := adapter.TransferRequest{Type: in.Type, Account: in.Account, Name: in.Name, BankCode: in.BankCode, Amount: v.Settlement.NetAmount, Remark: "结算出款", Title: "结算出款", TransferNo: "TF0"}
	kind, app, e := g.settlementProvider(channel, in, req)
	if e != nil {
		return SettlementPayout{}, e
	}
	if e = g.Store.settlementWritePolicy(ctx, tx, v.MerchantID, profile); e != nil {
		return SettlementPayout{}, e
	}
	if check != nil {
		if e = check(ctx, channel); e != nil {
			return SettlementPayout{}, e
		}
	}
	var tid int64
	var biz string
	if e = tx.QueryRow(ctx, "SELECT nextval('trade_no_seq')::text").Scan(&biz); e != nil {
		return SettlementPayout{}, e
	}
	if e = tx.QueryRow(ctx, "INSERT INTO transfers(merchant_id,transfer_no,amount,cost_amount,fee_amount,fee_rate_bps,biz_no) VALUES($1,$2,$3,$3,0,0,$4) RETURNING id", v.MerchantID, fmt.Sprintf("ST%d", id), req.Amount, biz).Scan(&tid); e != nil {
		return SettlementPayout{}, e
	}
	req.TransferNo = fmt.Sprintf("TF%d", tid)
	cipher, e := g.Store.encryptTransfer(tid, req)
	if e != nil {
		return SettlementPayout{}, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO provider_transfer_attempts(transfer_id,channel_id,recipient_cipher,method) VALUES($1,$2,$3,$4)", tid, channel, cipher, req.Type); e != nil {
		return SettlementPayout{}, e
	}
	if e = g.Store.indexPrivateSearchTx(ctx, tx, "transfer_intent", tid, v.MerchantID, cipher, privatesearch.Fields{privatesearch.Account: req.Account, privatesearch.Name: req.Name, privatesearch.Remark: req.Remark}); e != nil {
		return SettlementPayout{}, e
	}
	accountScope, e := adapter.TransferAccountScope(g.Providers[channel])
	if e != nil {
		return SettlementPayout{}, e
	}
	if e = g.Store.bindTransferAccount(ctx, tx, tid, accountScope); e != nil {
		return SettlementPayout{}, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO settlement_payouts(request_id,transfer_id,channel_id,provider_kind,app_id,recipient_hash,gross_amount,net_amount,fee_amount) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", id, tid, channel, kind, app, hash, v.Amount, v.Settlement.NetAmount, v.Settlement.FeeAmount); e != nil {
		return SettlementPayout{}, e
	}
	if in.Type == "bank" {
		product := providerBankProduct(g.Providers[channel])
		recipientHash, requestHash, err := g.Store.transferPolicyHashes(v.MerchantID, req)
		if err != nil {
			return SettlementPayout{}, err
		}
		if _, e = tx.Exec(ctx, "INSERT INTO provider_transfer_policies(transfer_id,merchant_id,recipient_hash,request_hash) VALUES($1,$2,$3,$4)", tid, v.MerchantID, recipientHash, requestHash); e != nil {
			return SettlementPayout{}, e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO bank_transfer_lifecycle(transfer_id,provider_kind) VALUES($1,$2)", tid, product); e != nil {
			return SettlementPayout{}, e
		}
		if product == adapter.BankProductAlipay {
			if _, e = tx.Exec(ctx, "INSERT INTO alipay_bank_bindings(transfer_id,request_digest) VALUES($1,$2)", tid, requestHash); e != nil {
				return SettlementPayout{}, e
			}
		}
		if _, e = tx.Exec(ctx, "INSERT INTO settlement_bank_payouts(request_id,transfer_id,bank_product) VALUES($1,$2,$3)", id, tid, product); e != nil {
			return SettlementPayout{}, e
		}
	}
	if e = payoutEvent(ctx, tx, id, "prepare", "unsent", actor); e != nil {
		return SettlementPayout{}, e
	}
	prepared, e := g.Store.settlementPayout(ctx, tx, id)
	if e != nil {
		return prepared, e
	}
	if e = g.Store.sealParentTransferSource(ctx, tx, prepared.Transfer, g.parentBindings[channel], g.Providers[channel]); e != nil {
		return prepared, e
	}
	if e = g.checkParentTransferWriteTx(ctx, tx, prepared.Transfer); e != nil {
		return prepared, e
	}
	return prepared, nil
}
