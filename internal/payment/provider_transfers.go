package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/ledger"
	"goepay/internal/privatesearch"
	"math"
	"net/url"
	"strings"
	"unicode/utf8"
)

const transferColumns = "t.id,t.merchant_id,t.transfer_no,t.amount,t.status,t.cost_amount,t.fee_amount,t.fee_rate_bps,t.biz_no,COALESCE(a.channel_id,0),COALESCE(a.state,''),COALESCE(a.provider_transfer_no,''),COALESCE(a.pay_date,''),a.recipient_cipher,COALESCE(b.state,''),COALESCE(b.bank_fee_amount,0),(b.bank_fee_amount IS NOT NULL),COALESCE(b.service_fee_held,0),COALESCE(b.returned_principal,0),COALESCE(b.returned_service_fee,0),COALESCE(b.returned_bank_fee,0),b.next_check_at,ARRAY(SELECT j.id::text FROM journals j WHERE j.event_key LIKE 'transfer:'||t.id::text||':bank-return:%' ORDER BY j.id),COALESCE(b.provider_kind,''),COALESCE(ab.estimated_fee_amount,0),(ab.estimated_fee_amount IS NOT NULL),COALESCE(batch_item.state,''),COALESCE(sp.request_id,0),COALESCE(sp.send_state,'')"
const transferFrom = " FROM transfers t LEFT JOIN provider_transfer_attempts a ON a.transfer_id=t.id LEFT JOIN bank_transfer_lifecycle b ON b.transfer_id=t.id LEFT JOIN alipay_bank_bindings ab ON ab.transfer_id=t.id LEFT JOIN transfer_batch_items batch_item ON batch_item.transfer_id=t.id LEFT JOIN settlement_payouts sp ON sp.transfer_id=t.id "

func (s *Store) scanTransfer(row pgx.Row) (Transfer, error) {
	var t Transfer
	var encrypted []byte
	var batchState string
	e := row.Scan(&t.ID, &t.MerchantID, &t.TransferNo, &t.Amount, &t.Status, &t.CostAmount, &t.FeeAmount, &t.FeeRateBPS, &t.BizNo, &t.ChannelID, &t.AttemptState, &t.ProviderTransferNo, &t.PayDate, &encrypted, &t.BankLifecycle, &t.BankFeeAmount, &t.BankFeeKnown, &t.BankServiceFeeHeld, &t.BankReturnedPrincipal, &t.BankReturnedServiceFee, &t.BankReturnedFee, &t.BankNextCheckAt, &t.BankReversalJournals, &t.BankProduct, &t.BankEstimatedFeeAmount, &t.BankEstimatedFeeKnown, &batchState, &t.SettlementRequestID, &t.SendState)
	if errors.Is(e, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	if e != nil {
		return t, e
	}
	if len(encrypted) > 0 {
		r, e := s.decryptTransfer(t.ID, encrypted)
		if e != nil {
			return t, e
		}
		if r.TransferNo != fmt.Sprintf("TF%d", t.ID) || r.Amount != t.Amount {
			return t, ErrConflict
		}
		t.Type, t.Account, t.Name, t.Title, t.Remark, t.BankCode = r.Type, r.Account, r.Name, r.Title, r.Remark, r.BankCode
	}
	t.LedgerStatus = t.Status
	if t.BankLifecycle == "reversed" || t.BankLifecycle == "reversal_pending" {
		t.Status = t.BankLifecycle
	}
	if batchState == "unsent" || batchState == "cancelled" {
		t.Status = batchState
	}
	t.FundingSource = "merchant_available"
	if t.SettlementRequestID > 0 {
		t.FundingSource = "settlement"
		if t.SendState == "unsent" || t.SendState == "cancelled" {
			t.Status = t.SendState
		}
	}
	return t, nil
}
func (s *Store) encryptTransfer(id int64, r adapter.TransferRequest) ([]byte, error) {
	if s.aead == nil {
		return nil, ErrInvalid
	}
	raw, e := json.Marshal(r)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return s.aead.Seal(nonce, nonce, raw, []byte(fmt.Sprintf("transfer:%d", id))), nil
}
func (s *Store) decryptTransfer(id int64, raw []byte) (adapter.TransferRequest, error) {
	var r adapter.TransferRequest
	if s.aead == nil || len(raw) < s.aead.NonceSize() {
		return r, ErrInvalid
	}
	n := s.aead.NonceSize()
	plain, e := s.aead.Open(nil, raw[:n], raw[n:], []byte(fmt.Sprintf("transfer:%d", id)))
	if e != nil {
		return r, e
	}
	e = json.Unmarshal(plain, &r)
	return r, e
}
func transferRequest(t Transfer) adapter.TransferRequest {
	return adapter.TransferRequest{TransferNo: fmt.Sprintf("TF%d", t.ID), Type: t.Type, Account: t.Account, Name: t.Name, Title: t.Title, Remark: t.Remark, BankCode: t.BankCode, Amount: t.Amount}
}
func transferCost(amount int64, rawRate string) (cost, fee, rate int64, e error) {
	if amount <= 0 {
		return 0, 0, 0, ErrInvalid
	}
	if rawRate == "" {
		rawRate = "0"
	}
	rate, e = ledger.Parse(rawRate)
	if e != nil || rate > 10000 {
		return 0, 0, 0, ErrInvalid
	}
	fee = amount/10000*rate + (amount%10000*rate+5000)/10000
	if amount > math.MaxInt64-fee {
		return 0, 0, 0, ErrInvalid
	}
	return amount + fee, fee, rate, nil
}
func validTransferRequest(r adapter.TransferRequest) bool {
	if r.Amount <= 0 || r.Account == "" || len(r.Account) > 128 || len(r.Name) > 128 || len(r.BankCode) > 32 || utf8.RuneCountInString(r.Remark) > 32 || len(r.Title) > 128 {
		return false
	}
	for _, s := range []string{r.Account, r.Name, r.Title, r.Remark, r.BankCode} {
		if !utf8.ValidString(s) || strings.ContainsAny(s, "\x00\r\n") {
			return false
		}
	}
	return r.Type == "wxpay" || r.Type == "alipay" || r.Type == "bank"
}
func (s *Store) beginTransfer(ctx context.Context, merchantID, channelID int64, no string, r adapter.TransferRequest, validate func(adapter.TransferRequest) error, bankProduct, accountScope string, apiOrigin ...bool) (Transfer, bool, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Transfer{}, false, e
	}
	defer tx.Rollback(ctx)
	t, owner, e := s.beginTransferTx(ctx, tx, merchantID, channelID, no, r, validate, bankProduct, accountScope, apiOrigin...)
	if e != nil {
		return t, false, e
	}
	if e = tx.Commit(ctx); e != nil {
		return t, false, e
	}
	return t, owner, nil
}

func (s *Store) beginTransferTx(ctx context.Context, tx pgx.Tx, merchantID, channelID int64, no string, r adapter.TransferRequest, validate func(adapter.TransferRequest) error, bankProduct, accountScope string, apiOrigin ...bool) (Transfer, bool, error) {
	return s.beginTransferTxWithFeeSelection(ctx, tx, merchantID, channelID, no, r, validate, bankProduct, accountScope, nil, apiOrigin...)
}

// The parent-runtime path persists additional source and audit rows in the
// same transaction. It must recheck this exact fee selection after those waits,
// rather than selecting a different group once the original membership expires.
func (s *Store) beginTransferTxWithFeeSelection(ctx context.Context, tx pgx.Tx, merchantID, channelID int64, no string, r adapter.TransferRequest, validate func(adapter.TransferRequest) error, bankProduct, accountScope string, captured **merchantTransferFeeSelection, apiOrigin ...bool) (Transfer, bool, error) {
	var e error
	var profile []byte
	var enabled bool
	// Non-key serialization remains compatible with the audit FK's KEY SHARE.
	// FOR UPDATE here would deadlock duplicate submission against result audit.
	e = tx.QueryRow(ctx, "SELECT enabled,profile FROM merchants WHERE id=$1 FOR NO KEY UPDATE", merchantID).Scan(&enabled, &profile)
	if errors.Is(e, pgx.ErrNoRows) {
		return Transfer{}, false, ErrNotFound
	}
	if e != nil {
		return Transfer{}, false, e
	}
	if !enabled {
		return Transfer{}, false, ErrState
	}
	recipientHash, requestHash, e := s.transferPolicyHashes(merchantID, r)
	if e != nil {
		return Transfer{}, false, e
	}
	old, e := s.scanTransfer(tx.QueryRow(ctx, "SELECT "+transferColumns+transferFrom+" WHERE t.merchant_id=$1 AND t.transfer_no=$2 FOR UPDATE OF t", merchantID, no))
	if e == nil {
		if e = s.checkTransferAccountScope(ctx, tx, old.ID, accountScope, true); e != nil {
			return old, false, e
		}
		if r.Type == "bank" && old.BankProduct != bankProduct {
			return Transfer{}, false, ErrConflict
		}
		var original []byte
		he := tx.QueryRow(ctx, "SELECT request_hash FROM provider_transfer_policies WHERE transfer_id=$1", old.ID).Scan(&original)
		if he == nil {
			if old.ChannelID != channelID || !hmac.Equal(original, requestHash) {
				return Transfer{}, false, ErrConflict
			}
			return old, false, nil
		}
		if !errors.Is(he, pgx.ErrNoRows) {
			return Transfer{}, false, he
		}
		r.TransferNo = fmt.Sprintf("TF%d", old.ID)
		if old.ChannelID != channelID || transferRequest(old) != r {
			return Transfer{}, false, ErrConflict
		}
		return old, false, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return Transfer{}, false, e
	}
	if len(apiOrigin) > 1 {
		return Transfer{}, false, ErrInvalid
	}
	if len(apiOrigin) == 1 && apiOrigin[0] {
		m, e := s.merchantAPIAccountTx(ctx, tx, merchantID, false)
		if e != nil {
			return Transfer{}, false, e
		}
		if e = s.checkMerchantAPIAdmissionTx(ctx, tx, m, "transfer"); e != nil {
			return Transfer{}, false, e
		}
	}
	var cfg map[string]json.RawMessage
	if json.Unmarshal(profile, &cfg) != nil {
		return Transfer{}, false, ErrInvalid
	}
	var api bool
	json.Unmarshal(cfg["transfer_api_enabled"], &api)
	if !api {
		return Transfer{}, false, ErrState
	}
	policy, e := readManualTransferPolicy(ctx, tx, merchantID)
	if e != nil {
		return Transfer{}, false, e
	}
	if !policy.Enabled {
		return Transfer{}, false, ErrState
	}
	if r.Remark == "" {
		r.Remark = policy.DefaultRemark
	}
	if r.Title == "" {
		r.Title = policy.DefaultTitle
	}
	if !validTransferRequest(r) || (r.Type == "wxpay" && r.Remark == "") {
		return Transfer{}, false, ErrInvalid
	}
	if validate != nil {
		preflight := r
		preflight.TransferNo = "TF0"
		if e = validate(preflight); e != nil {
			return Transfer{}, false, e
		}
	}
	if e = s.enforceTransferPolicy(ctx, tx, merchantID, r, policy, recipientHash); e != nil {
		return Transfer{}, false, e
	}
	selectedFee, e := readMerchantTransferFeeSelection(ctx, tx, merchantID, true)
	if e != nil {
		return Transfer{}, false, e
	}
	if captured != nil {
		*captured = &selectedFee
	}
	cost, fee, rate, e := transferCost(r.Amount, ledger.Format(selectedFee.RateBPS))
	if e != nil {
		return Transfer{}, false, e
	}
	var business string
	if e = tx.QueryRow(ctx, "SELECT nextval('trade_no_seq')::text").Scan(&business); e != nil {
		return Transfer{}, false, e
	}
	t, e := s.reserveTransferTx(ctx, tx, merchantID, no, r.Amount, cost, fee, rate, business)
	if e != nil {
		return t, false, e
	}
	if e = selectedFee.checkDeadline(ctx, tx); e != nil {
		return t, false, e
	}
	r.TransferNo = fmt.Sprintf("TF%d", t.ID)
	cipher, e := s.encryptTransfer(t.ID, r)
	if e != nil {
		return t, false, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO provider_transfer_attempts(transfer_id,channel_id,recipient_cipher,method) VALUES($1,$2,$3,$4)", t.ID, channelID, cipher, r.Type); e != nil {
		return t, false, e
	}
	if e = s.indexPrivateSearchTx(ctx, tx, "transfer_intent", t.ID, merchantID, cipher, privatesearch.Fields{privatesearch.Account: r.Account, privatesearch.Name: r.Name, privatesearch.Remark: r.Remark}); e != nil {
		return t, false, e
	}
	if e = s.bindTransferAccount(ctx, tx, t.ID, accountScope); e != nil {
		return t, false, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO provider_transfer_policies(transfer_id,merchant_id,recipient_hash,request_hash) VALUES($1,$2,$3,$4)", t.ID, merchantID, recipientHash, requestHash); e != nil {
		return t, false, e
	}
	if r.Type == "bank" {
		if _, e = tx.Exec(ctx, "INSERT INTO bank_transfer_lifecycle(transfer_id,provider_kind) VALUES($1,$2)", t.ID, bankProduct); e != nil {
			return t, false, e
		}
		if bankProduct == adapter.BankProductAlipay {
			if _, e = tx.Exec(ctx, "INSERT INTO alipay_bank_bindings(transfer_id,request_digest) VALUES($1,$2)", t.ID, requestHash); e != nil {
				return t, false, e
			}
		}
	}
	t, e = s.scanTransfer(tx.QueryRow(ctx, "SELECT "+transferColumns+transferFrom+" WHERE t.id=$1", t.ID))
	if e != nil {
		return t, false, e
	}
	if e = selectedFee.checkDeadline(ctx, tx); e != nil {
		return t, false, e
	}
	return t, true, nil
}

// Transfer submits one immutable recipient intent at most once. Unknown and
// crashed attempts require query; they never trigger a replacement transfer.
func (g ProviderFlow) Transfer(ctx context.Context, merchantID, channelID int64, no string, r adapter.TransferRequest) (Transfer, error) {
	return g.transfer(ctx, merchantID, channelID, no, r, false)
}
func (g ProviderFlow) TransferFromAPI(ctx context.Context, merchantID, channelID int64, no string, r adapter.TransferRequest) (Transfer, error) {
	return g.transfer(ctx, merchantID, channelID, no, r, true)
}
func (g ProviderFlow) transfer(ctx context.Context, merchantID, channelID int64, no string, r adapter.TransferRequest, api bool) (Transfer, error) {
	if !g.paymentWritesEnabled() {
		return Transfer{}, ErrLiveDisabled
	}
	if g.Store == nil || merchantID <= 0 || channelID <= 0 || no == "" || len(no) > 128 {
		return Transfer{}, ErrInvalid
	}
	resolved, resolveErr := g.parentTransferRuntime(ctx, merchantID, channelID, no)
	if resolveErr != nil {
		return Transfer{}, resolveErr
	}
	g = resolved
	if e := g.Store.checkChannelWrite(ctx, channelID); e != nil {
		return Transfer{}, e
	}
	r.Account = strings.TrimSpace(r.Account)
	r.Name = strings.TrimSpace(r.Name)
	r.TransferNo = ""
	if !validTransferRequest(r) {
		return Transfer{}, ErrInvalid
	}
	p := g.Providers[channelID]
	tp, ok := p.(adapter.TransferProvider)
	caps, capable := p.(adapter.TransferCapabilities)
	if !ok || !capable {
		return Transfer{}, adapter.ErrUnsupported
	}
	supported := false
	for _, typ := range caps.TransferTypes() {
		if typ == r.Type {
			supported = true
		}
	}
	if !supported {
		return Transfer{}, adapter.ErrUnsupported
	}
	// 商家转账（新） settings are checked before anything is reserved.
	if merchantTransferChannel(p, r.Type) {
		if _, e := g.Store.MerchantTransferScene(ctx); e != nil {
			return Transfer{}, e
		}
	}
	var validate func(adapter.TransferRequest) error
	if v, ok := p.(interface {
		ValidateTransfer(adapter.TransferRequest) error
	}); ok {
		validate = v.ValidateTransfer
	}
	product := ""
	if r.Type == "bank" {
		product = providerBankProduct(p)
		if product != adapter.BankProductAlipay && product != adapter.BankProductWechat {
			return Transfer{}, adapter.ErrUnsupported
		}
	}
	accountScope, e := adapter.TransferAccountScope(p)
	if e != nil {
		return Transfer{}, e
	}
	t, owner, e := g.beginRuntimeParentTransfer(ctx, merchantID, channelID, no, r, validate, product, accountScope, api)
	if e != nil || !owner {
		return t, e
	}
	return g.sendClaimedTransfer(ctx, t, tp)
}

// The authenticated transfer response has its own rejection vocabulary. Do not
// reuse order/refund "not found": an uncertain transfer may still be accepted.
func definiteTransferRejection(e *adapter.ProviderError) bool {
	switch e.Code {
	case "SIGNERROR", "PARAM_ERROR", "INVALID_REQUEST", "NOAUTH", "APPID_NOT_EXIST", "MCHID_NOT_EXIST", "NOTENOUGH", "NAME_MISMATCH", "OPENID_ERROR", "PAYEE_NOT_EXIST", "40004:NAME_MISMATCH", "40004:OPENID_ERROR", "40004:PAYEE_NOT_EXIST":
		return true
	case "40002:isv.invalid-signature", "40002:isv.invalid-parameter", "40002:isv.missing-parameter":
		return true
	}
	return false
}

func definiteAuthenticatedTransferRejection(err error, p adapter.Provider, kind string) bool {
	if p == nil {
		return false
	}
	v, valid := adapter.VerifiedProviderRejection(err)
	if !valid || v.Provider != p.ID() || v.Provider != kind {
		return false
	}
	var rejected *adapter.ProviderError
	if !errors.As(err, &rejected) || !definiteTransferRejection(rejected) {
		return false
	}
	if kind == "wxpay" {
		return v.Operation == "/mmpaymkttransfers/promotion/transfers"
	}
	if kind == "alipay" {
		product, ok := p.(adapter.TransferProductIdentity)
		return ok && v.Operation == product.TransferProductIdentity() && (v.Operation == "alipay.fund.trans.toaccount.transfer" || v.Operation == "alipay.fund.trans.uni.transfer")
	}
	return false
}
func transferProviderState(state string) string {
	switch state {
	case "SUCCESS":
		return "succeeded"
	case "FAILED":
		return "failed"
	case "PROCESSING":
		return "pending"
	default:
		return "unknown"
	}
}
func (g ProviderFlow) QueryTransfer(ctx context.Context, merchantID int64, no string) (Transfer, error) {
	if g.Store == nil {
		return Transfer{}, ErrInvalid
	}
	t, e := g.Store.Transfer(ctx, merchantID, no)
	if e != nil {
		return t, e
	}
	resolved, resolveErr := g.ParentTransferIntent(ctx, t)
	if resolveErr != nil {
		return t, resolveErr
	}
	g = resolved
	if e = g.Store.checkTransferAccount(ctx, g.Store.pool, t.ID, g.Providers[t.ChannelID]); e != nil {
		return t, e
	}
	if local, done, err := g.settlementTransferLocal(ctx, t); done || err != nil {
		return local, err
	}
	if local, done, err := g.batchTransferLocal(ctx, t); done || err != nil {
		return local, err
	}
	if (t.Type != "bank" && (t.Status == "succeeded" || t.Status == "failed")) || t.BankLifecycle == "reversed" || t.BankLifecycle == "failed" {
		return t, nil
	}
	p, ok := g.Providers[t.ChannelID].(adapter.TransferProvider)
	if !ok {
		return t, adapter.ErrUnsupported
	}
	if t.Type == "bank" && providerBankProduct(g.Providers[t.ChannelID]) != t.BankProduct {
		return t, adapter.ErrUnsupported
	}
	result, e := p.QueryTransfer(ctx, transferRequest(t))
	if e != nil {
		return g.alipayBankQueryFailure(ctx, t, e)
	}
	if result.TransferNo != fmt.Sprintf("TF%d", t.ID) || result.Amount != t.Amount {
		return g.alipayBankQueryFailure(ctx, t, adapter.ErrAuthentication)
	}
	if t.Type == "bank" {
		e = g.Store.resolveBankResult(ctx, t, result)
		current, read := g.Store.Transfer(ctx, merchantID, no)
		if e != nil {
			return current, e
		}
		if read != nil {
			return current, read
		}
		if current.BankLifecycle == "reversal_pending" {
			return current, ErrBankReversalPending
		}
		return current, nil
	}
	state := transferProviderState(result.State)
	if state == "succeeded" && result.ProviderTransferNo == "" {
		return t, adapter.ErrAuthentication
	}
	if e = g.Store.resolveProviderTransferPackage(ctx, t.ID, state, result.ProviderTransferNo, result.PayDate, result.PackageInfo); e != nil {
		return t, e
	}
	return g.Store.Transfer(ctx, merchantID, no)
}
func (s *Store) resolveProviderTransfer(ctx context.Context, id int64, state, providerNo, payDate string) error {
	return s.resolveProviderTransferPackage(ctx, id, state, providerNo, payDate, "")
}

// resolveProviderTransferPackage also records a 商家转账 package_info, in the
// same transaction, for a bill still waiting for the payee.
func (s *Store) resolveProviderTransferPackage(ctx context.Context, id int64, state, providerNo, payDate, pkg string) error {
	if len(providerNo) > 256 || len(payDate) > 64 {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = s.lockSettlementTransfer(ctx, tx, id); e != nil {
		return e
	}
	var local string
	if e = tx.QueryRow(ctx, "SELECT status FROM transfers WHERE id=$1 FOR UPDATE", id).Scan(&local); e != nil {
		return e
	}
	var current, existingNo, existingDate string
	if e = tx.QueryRow(ctx, "SELECT state,provider_transfer_no,pay_date FROM provider_transfer_attempts WHERE transfer_id=$1 FOR UPDATE", id).Scan(&current, &existingNo, &existingDate); e != nil {
		return e
	}
	if current == "succeeded" || current == "failed" {
		if state == "pending" || state == "unknown" || state == current {
			return nil
		}
		return ErrState
	}
	if existingNo != "" && providerNo != "" && existingNo != providerNo {
		return ErrConflict
	}
	if providerNo == "" {
		providerNo = existingNo
	}
	if payDate == "" {
		payDate = existingDate
	}
	if current == "unknown" && state == "pending" {
		state = "unknown"
	}
	if e = s.resolveTransferTx(ctx, tx, id, state); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE provider_transfer_attempts SET state=$2,provider_transfer_no=$3,pay_date=$4,updated_at=now() WHERE transfer_id=$1", id, state, providerNo, payDate); e != nil {
		return e
	}
	if state == "succeeded" {
		s.recordMerchantNoticeSettlement(ctx, tx, id)
	}
	if state == "pending" || state == "unknown" {
		if e = recordTransferPackageTx(ctx, tx, id, pkg); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) TransferByBizNo(ctx context.Context, merchantID int64, bizNo string) (Transfer, error) {
	return s.scanTransfer(s.pool.QueryRow(ctx, "SELECT "+transferColumns+transferFrom+" WHERE t.merchant_id=$1 AND (t.biz_no=$2 OR t.id::text=$2) ORDER BY (t.biz_no=$2) DESC LIMIT 1", merchantID, bizNo))
}

// TransferProof records the application before contacting the provider. A lost
// application response never permits another application; a known file ID is
// queried without performing another financial or proof creation request.
func (g ProviderFlow) TransferProof(ctx context.Context, merchantID int64, no string) (adapter.TransferProofResult, error) {
	var result adapter.TransferProofResult
	if g.Store == nil {
		return result, ErrInvalid
	}
	t, e := g.Store.Transfer(ctx, merchantID, no)
	if e != nil {
		return result, e
	}
	resolved, resolveErr := g.ParentTransferIntent(ctx, t)
	if resolveErr != nil {
		return result, resolveErr
	}
	g = resolved
	if e = g.Store.checkTransferAccount(ctx, g.Store.pool, t.ID, g.Providers[t.ChannelID]); e != nil {
		return result, e
	}
	if t.Status != "succeeded" || t.ProviderTransferNo == "" {
		return result, ErrState
	}
	p, ok := g.Providers[t.ChannelID].(adapter.TransferProofProvider)
	if !ok {
		return result, adapter.ErrUnsupported
	}
	var state, file string
	var encrypted []byte
	e = g.Store.pool.QueryRow(ctx, "SELECT state,file_id,download_cipher FROM provider_transfer_proofs WHERE transfer_id=$1", t.ID).Scan(&state, &file, &encrypted)
	owner := false
	if errors.Is(e, pgx.ErrNoRows) {
		if !g.paymentWritesEnabled() {
			return result, ErrLiveDisabled
		}
		tag, insertErr := g.Store.pool.Exec(ctx, "INSERT INTO provider_transfer_proofs(transfer_id,state) VALUES($1,'applying') ON CONFLICT DO NOTHING", t.ID)
		if insertErr != nil {
			return result, insertErr
		}
		owner = tag.RowsAffected() == 1
		if !owner {
			e = g.Store.pool.QueryRow(ctx, "SELECT state,file_id,download_cipher FROM provider_transfer_proofs WHERE transfer_id=$1", t.ID).Scan(&state, &file, &encrypted)
		} else {
			e = nil
		}
	}
	if e != nil {
		return result, e
	}
	if !owner && state == "succeeded" {
		result = adapter.TransferProofResult{FileID: file, State: "SUCCESS"}
		if len(encrypted) > 0 {
			plain, de := g.Store.decryptProof(t.ID, encrypted)
			if de != nil {
				return result, de
			}
			result.DownloadURL = plain
		}
		return result, nil
	}
	if !owner && state == "failed" {
		return adapter.TransferProofResult{FileID: file, State: "FAILED"}, nil
	}
	if !owner && file == "" {
		return result, &adapter.UnknownOutcome{Operation: "proof application", Cause: ErrState}
	}
	if owner {
		result, e = p.ApplyTransferProof(ctx, t.ProviderTransferNo)
	} else {
		result, e = p.QueryTransferProof(ctx, file)
	}
	if e != nil {
		g.Store.pool.Exec(ctx, "UPDATE provider_transfer_proofs SET state='unknown',updated_at=now() WHERE transfer_id=$1 AND state NOT IN ('succeeded','failed')", t.ID)
		return result, e
	}
	if result.FileID == "" || len(result.FileID) > 256 || (!owner && result.FileID != file) {
		return adapter.TransferProofResult{}, adapter.ErrAuthentication
	}
	next := transferProviderState(result.State)
	if next == "succeeded" && result.DownloadURL == "" {
		next = "pending"
	}
	var cipher []byte
	if result.DownloadURL != "" {
		parsed, pe := url.Parse(result.DownloadURL)
		if pe != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || len(result.DownloadURL) > 8192 {
			return adapter.TransferProofResult{}, adapter.ErrAuthentication
		}
		cipher, e = g.Store.encryptProof(t.ID, result.DownloadURL)
		if e != nil {
			return adapter.TransferProofResult{}, e
		}
	}
	_, e = g.Store.pool.Exec(ctx, "UPDATE provider_transfer_proofs SET state=$2,file_id=$3,download_cipher=COALESCE($4,download_cipher),updated_at=now() WHERE transfer_id=$1 AND state NOT IN ('succeeded','failed') AND (file_id='' OR file_id=$3)", t.ID, next, result.FileID, cipher)
	return result, e
}
func (s *Store) encryptProof(id int64, value string) ([]byte, error) {
	if s.aead == nil {
		return nil, ErrInvalid
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		return nil, e
	}
	return s.aead.Seal(nonce, nonce, []byte(value), []byte(fmt.Sprintf("transfer-proof:%d", id))), nil
}
func (s *Store) decryptProof(id int64, raw []byte) (string, error) {
	if s.aead == nil || len(raw) < s.aead.NonceSize() {
		return "", ErrInvalid
	}
	n := s.aead.NonceSize()
	plain, e := s.aead.Open(nil, raw[:n], raw[n:], []byte(fmt.Sprintf("transfer-proof:%d", id)))
	return string(plain), e
}
