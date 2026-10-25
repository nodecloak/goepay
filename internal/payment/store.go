package payment

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/businessnotice"
	"goepay/internal/merchantnotice"
	"os"
	"time"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("idempotency conflict")
	ErrState        = errors.New("invalid state transition")
	ErrInsufficient = errors.New("insufficient available balance")
	ErrInvalid      = errors.New("invalid input")
)

type Merchant struct {
	PayStatus          int    `json:"pay"`
	ConfigRevision     int64  `json:"-"`
	SignatureAlgorithm string `json:"signature_algorithm"`
	ID                 int64  `json:"id,string"`
	Name               string `json:"name"`
	APIKey             string `json:"-"`
	RSAPublicKey       string `json:"-"`
	Enabled            bool   `json:"enabled"`
}
type CreateOrderInput struct {
	CollectionConfigHash        string            `json:"-"`
	PaymentCost                 *PaymentCostQuote `json:"-"`
	Version                     string
	MerchantID                  int64
	OutTradeNo, Name, Type      string
	Amount                      int64
	NotifyURL, ReturnURL, Param string
}
type Order struct {
	MerchantAmount int64      `json:"merchant_amount,string"`
	PlatformFee    int64      `json:"platform_fee,string"`
	FeeRateBPS     int64      `json:"fee_rate_bps"`
	FeeGroupID     int64      `json:"fee_group_id,string"`
	FeePolicy      string     `json:"fee_policy"`
	RefundFeePayer string     `json:"refund_fee_payer"`
	Version        string     `json:"version"`
	ID             int64      `json:"id,string"`
	TradeNo        string     `json:"trade_no"`
	MerchantID     int64      `json:"merchant_id,string"`
	OutTradeNo     string     `json:"out_trade_no"`
	Name           string     `json:"name"`
	Type           string     `json:"type"`
	Amount         int64      `json:"amount,string"`
	NotifyURL      string     `json:"notify_url"`
	ReturnURL      string     `json:"return_url"`
	Param          string     `json:"param"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	PaidAt         *time.Time `json:"paid_at"`
}
type Refund struct {
	MerchantAmount int64  `json:"merchant_amount,string"`
	FeeAmount      int64  `json:"fee_amount,string"`
	ID             int64  `json:"id,string"`
	MerchantID     int64  `json:"merchant_id,string"`
	OrderID        int64  `json:"order_id,string"`
	RefundNo       string `json:"refund_no"`
	Amount         int64  `json:"amount,string"`
	Status         string `json:"status"`
}
type Transfer struct {
	FundingSource          string     `json:"funding_source"`
	SettlementRequestID    int64      `json:"settlement_request_id,string,omitempty"`
	SendState              string     `json:"send_state,omitempty"`
	BankProduct            string     `json:"bank_product,omitempty"`
	BankEstimatedFeeAmount int64      `json:"bank_estimated_fee_amount,string"`
	BankEstimatedFeeKnown  bool       `json:"bank_estimated_fee_known"`
	LedgerStatus           string     `json:"ledger_status"`
	BankLifecycle          string     `json:"bank_lifecycle,omitempty"`
	BankFeeAmount          int64      `json:"bank_fee_amount,string"`
	BankFeeKnown           bool       `json:"bank_fee_known"`
	BankServiceFeeHeld     int64      `json:"bank_service_fee_held,string"`
	BankReturnedPrincipal  int64      `json:"bank_returned_principal,string"`
	BankReturnedServiceFee int64      `json:"bank_returned_service_fee,string"`
	BankReturnedFee        int64      `json:"bank_returned_fee,string"`
	BankNextCheckAt        *time.Time `json:"bank_next_check_at,omitempty"`
	BankReversalJournals   []string   `json:"bank_reversal_journals"`
	BizNo                  string     `json:"biz_no"`
	CostAmount             int64      `json:"cost_amount,string"`
	FeeAmount              int64      `json:"fee_amount,string"`
	FeeRateBPS             int64      `json:"fee_rate_bps"`
	ChannelID              int64      `json:"channel_id,string"`
	AttemptState           string     `json:"-"`
	Type                   string     `json:"type"`
	Account                string     `json:"-"`
	Name                   string     `json:"-"`
	Title                  string     `json:"-"`
	Remark                 string     `json:"remark"`
	BankCode               string     `json:"-"`
	ProviderTransferNo     string     `json:"provider_transfer_no"`
	PayDate                string     `json:"pay_date"`
	ID                     int64      `json:"id,string"`
	MerchantID             int64      `json:"merchant_id,string"`
	TransferNo             string     `json:"transfer_no"`
	Amount                 int64      `json:"amount,string"`
	Status                 string     `json:"status"`
}
type Balance struct {
	Available int64 `json:"available,string"`
	Reserved  int64 `json:"reserved,string"`
}
type Store struct {
	pool     *pgxpool.Pool
	aead     cipher.AEAD
	indexKey []byte
	notice   *merchantnotice.Store
	business *businessnotice.Store
}

func New(pool *pgxpool.Pool) *Store {
	key, _ := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
	s, e := NewWithKey(pool, key)
	if e != nil {
		return &Store{pool: pool}
	}
	return s
}
func NewWithKey(pool *pgxpool.Pool, key []byte) (*Store, error) {
	if len(key) != 32 {
		return nil, errors.New("GOEPAY_MASTER_KEY must contain 32 bytes")
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("goepay/transfer-policy-index/v1"))
	s := &Store{pool: pool, aead: a, indexKey: mac.Sum(nil)}
	if pool != nil {
		s.business, e = businessnotice.NewStore(pool, key, s)
		if e != nil {
			return nil, e
		}
		s.notice, e = merchantnotice.NewStore(pool, key)
		if e != nil {
			return nil, e
		}
	}
	return s, nil
}
func (s *Store) CreateMerchant(ctx context.Context, m Merchant) error {
	return s.CreateMerchantAtomic(ctx, m, nil)
}
func (s *Store) Merchant(ctx context.Context, id int64) (Merchant, error) {
	var m Merchant
	var secret []byte
	e := s.pool.QueryRow(ctx, "SELECT id,name,api_key,rsa_public_key,enabled,signature_algorithm,config_revision,COALESCE((to_jsonb(merchants)->>'pay')::int,1) FROM merchants WHERE id=$1", id).Scan(&m.ID, &m.Name, &secret, &m.RSAPublicKey, &m.Enabled, &m.SignatureAlgorithm, &m.ConfigRevision, &m.PayStatus)
	if errors.Is(e, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	if e != nil {
		return m, e
	}
	if s.aead == nil || len(secret) < s.aead.NonceSize() {
		return m, ErrInvalid
	}
	n := s.aead.NonceSize()
	raw, e := s.aead.Open(nil, secret[:n], secret[n:], []byte(fmt.Sprint(id)))
	if e != nil {
		return m, e
	}
	m.APIKey = string(raw)
	return m, nil
}

const orderColumns = "id,trade_no,merchant_id,out_trade_no,name,type,amount,notify_url,return_url,param,status,created_at,paid_at,version,merchant_amount,platform_fee,fee_rate_bps,fee_group_id,fee_policy,refund_fee_payer"

func scanOrder(row pgx.Row) (Order, error) {
	var o Order
	e := row.Scan(&o.ID, &o.TradeNo, &o.MerchantID, &o.OutTradeNo, &o.Name, &o.Type, &o.Amount, &o.NotifyURL, &o.ReturnURL, &o.Param, &o.Status, &o.CreatedAt, &o.PaidAt, &o.Version, &o.MerchantAmount, &o.PlatformFee, &o.FeeRateBPS, &o.FeeGroupID, &o.FeePolicy, &o.RefundFeePayer)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return o, e
}
func (s *Store) CreateOrder(ctx context.Context, in CreateOrderInput) (Order, error) {
	in, err := normalizeCreateOrderInput(in)
	if err != nil {
		return Order{}, err
	}
	return s.createOrderWithFees(ctx, in)
}

func normalizeCreateOrderInput(in CreateOrderInput) (CreateOrderInput, error) {
	if in.Version == "" {
		in.Version = "v1"
	}
	if in.Version != "v1" && in.Version != "v2" {
		return in, ErrInvalid
	}
	if in.MerchantID <= 0 || len(in.OutTradeNo) == 0 || len(in.OutTradeNo) > 128 || in.Amount <= 0 || len(in.Name) > 256 || len(in.Type) > 32 {
		return in, ErrInvalid
	}
	return in, nil
}
func (s *Store) Order(ctx context.Context, merchantID int64, outTradeNo string) (Order, error) {
	return scanOrder(s.pool.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE merchant_id=$1 AND out_trade_no=$2", merchantID, outTradeNo))
}
func (s *Store) OrderByTradeNo(ctx context.Context, tradeNo string) (Order, error) {
	return scanOrder(s.pool.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE trade_no=$1", tradeNo))
}
func (s *Store) ListOrders(ctx context.Context, merchantID int64, limit int) ([]Order, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, e := s.pool.Query(ctx, "SELECT "+orderColumns+" FROM orders WHERE ($1::bigint=0 OR merchant_id=$1) ORDER BY id DESC LIMIT $2", merchantID, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		o, e := scanOrder(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (s *Store) Balance(ctx context.Context, id int64) (Balance, error) {
	var b Balance
	e := s.pool.QueryRow(ctx, "SELECT COALESCE(sum(balance) FILTER(WHERE kind='available'),0),COALESCE(sum(balance) FILTER(WHERE kind IN ('reserved','settlement_reserve','channel_sharing_reserve')),0) FROM accounts WHERE owner_id=$1", id).Scan(&b.Available, &b.Reserved)
	return b, e
}

// post applies a balanced journal and account balances in the caller transaction. Account locks use canonical ID order.
func post(ctx context.Context, tx pgx.Tx, key, description string, fromOwner int64, fromKind string, toOwner int64, toKind string, amount int64) error {
	if amount <= 0 {
		return ErrInvalid
	}
	var fromID, toID int64
	if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind=$2 AND scope_id=0", fromOwner, fromKind).Scan(&fromID); e != nil {
		return e
	}
	if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind=$2 AND scope_id=0", toOwner, toKind).Scan(&toID); e != nil {
		return e
	}
	return postByAccountIDs(ctx, tx, key, description, fromID, toID, amount)
}
func (s *Store) MarkPaid(ctx context.Context, tradeNo, eventID string) error {
	return s.markPaid(ctx, tradeNo, eventID, false)
}
func (s *Store) markPaid(ctx context.Context, tradeNo, eventID string, allowLate bool, receipts ...PaidReceipt) error {
	if eventID == "" {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE trade_no=$1 FOR UPDATE", tradeNo))
	if e != nil {
		return e
	}
	if e = s.checkScanCancellationBarrier(ctx, tx, o.ID); e != nil {
		return e
	}
	if len(receipts) > 0 {
		if e := recordPaidReceipt(ctx, tx, o.ID, receipts[0]); e != nil {
			return e
		}
		// Authorization is only captured after the same trusted receipt that
		// credits this order. Keep that state change in the ledger transaction.
		if _, e = tx.Exec(ctx, "UPDATE payment_authorizations a SET capture_state='succeeded',updated_at=now() FROM provider_attempts p WHERE a.order_id=$1 AND a.channel_id=$2 AND p.order_id=a.order_id AND p.method='preauth'", o.ID, receipts[0].ChannelID); e != nil {
			return e
		}
	}
	if o.Status == "paid" {
		if e = s.verifyReferralPaidIntegrity(ctx, tx, o); e != nil {
			return e
		}
		if len(receipts) == 1 {
			if e = s.recordPrintingPaidSource(ctx, tx, o, receipts[0], false); e != nil {
				return e
			}
		}
		return tx.Commit(ctx)
	}
	if o.Status == "failed" || o.Status == "closed" {
		if !allowLate {
			return ErrState
		}
		if _, e = tx.Exec(ctx, "INSERT INTO provider_reconciliations(order_id,previous_status,event_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", o.ID, o.Status, eventID); e != nil {
			return e
		}
	}
	tag, e := tx.Exec(ctx, "INSERT INTO inbox(provider,event_id,order_id) VALUES('payment',$1,$2) ON CONFLICT DO NOTHING", eventID, o.ID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	referral, e := s.referralPaymentTerms(ctx, tx, o)
	if e != nil {
		return e
	}
	if (referral.Enabled || referral.profit != nil) && len(receipts) != 1 {
		// A stored receipt projection is not evidence that this call verified
		// the provider. First award creation requires the trusted call's fact.
		return ErrState
	}
	if e = lockPaymentAccountSet(ctx, tx, o, referral); e != nil {
		return e
	}
	if o.MerchantAmount > 0 {
		if e = post(ctx, tx, "payment:"+o.TradeNo, "payment net credit", 0, "clearing", o.MerchantID, "available", o.MerchantAmount); e != nil {
			return e
		}
	}
	if o.PlatformFee > 0 {
		if e = post(ctx, tx, "payment-fee:"+o.TradeNo, "payment platform fee", 0, "clearing", 0, "fees", o.PlatformFee); e != nil {
			return e
		}
	}
	if e = s.reserveChannelSharingPayment(ctx, tx, o); e != nil {
		return e
	}
	if e = s.awardReferralPayment(ctx, tx, o, referral); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE orders SET status='paid',paid_at=statement_timestamp(),provider_event_id=$2 WHERE id=$1", o.ID, eventID); e != nil {
		return e
	}
	if len(receipts) == 1 {
		if e = s.recordPrintingPaidSource(ctx, tx, o, receipts[0], true); e != nil {
			return e
		}
		s.recordMerchantNoticePaid(ctx, tx, o, receipts[0])
	}
	payload := fmt.Sprintf(`{"trade_no":%q,"merchant_id":%d}`, o.TradeNo, o.MerchantID)
	if _, e = tx.Exec(ctx, "INSERT INTO outbox(event_key,kind,payload) VALUES($1,'payment.paid',$2::jsonb)", "payment:"+o.TradeNo, payload); e != nil {
		return e
	}
	if o.NotifyURL != "" {
		if _, e = tx.Exec(ctx, "INSERT INTO jobs(event_key,kind,payload) VALUES($1,'payment.notify',$2::jsonb)", "notify:"+o.TradeNo, payload); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) CloseOrder(ctx context.Context, merchantID int64, outTradeNo string) error {
	tag, e := s.pool.Exec(ctx, "UPDATE orders SET status='closed' WHERE merchant_id=$1 AND out_trade_no=$2 AND status IN ('created','closed')", merchantID, outTradeNo)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrState
	}
	return nil
}
