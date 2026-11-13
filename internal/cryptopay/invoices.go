package cryptopay

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	"goepay/internal/cryptochain"
	"net/url"
	"strings"
	"time"
)

func randomID() string {
	v := make([]byte, 24)
	if _, e := rand.Read(v); e != nil {
		panic(e)
	}
	return hex.EncodeToString(v)
}
func validKey(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func validURL(s string) bool {
	if s == "" {
		return true
	}
	u, e := url.Parse(s)
	return e == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == "" && len(s) <= 2048
}
func normalize(network, address string) (string, error) {
	return cryptochain.NormalizeAddress(cryptochain.Chain(network), address)
}
func (s *Service) token(id string) string {
	h := hmac.New(sha256.New, s.TokenKey)
	h.Write([]byte("GOEPAY/crypto-checkout/v1\x00" + id))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (s *Service) AddAddress(ctx context.Context, network, address, role string) error {
	a, e := normalize(network, address)
	if e != nil || a != address || role != "deposit" && role != "treasury" && role != "hot" {
		return ErrInvalid
	}
	tx, e := s.begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var previous string
	e = tx.QueryRow(ctx, "SELECT role FROM crypto_addresses WHERE network=$1 AND address=$2", network, a).Scan(&previous)
	if e == nil {
		if previous != role {
			return ErrConflict
		}
		return commit(ctx, tx)
	}
	if e != pgx.ErrNoRows {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO crypto_addresses(network,address,role) VALUES($1,$2,$3)", network, a, role); e != nil {
		return e
	}
	return commit(ctx, tx)
}
func (s *Service) CreateInvoice(ctx context.Context, in CreateInvoiceInput) (Invoice, error) {
	var out Invoice
	if s == nil || len(s.TokenKey) != 32 {
		return out, ErrUnavailable
	}
	n, e := ParseAmount(in.Amount)
	if e != nil || in.ChannelID < 0 || in.MerchantID <= 0 || !validKey(in.OutTradeNo) || !validURL(in.NotifyURL) || !validURL(in.ReturnURL) || in.CNYAmount < 0 {
		return out, ErrInvalid
	}
	if in.ExpiresIn == 0 {
		in.ExpiresIn = 20 * time.Minute
	}
	if in.ExpiresIn < time.Minute || in.ExpiresIn > 24*time.Hour {
		return out, ErrInvalid
	}
	if in.Version == "" {
		in.Version = "native"
	}
	if in.Version != "native" && in.Version != "v1" && in.Version != "v2" {
		return out, ErrInvalid
	}
	if in.Name == "" {
		in.Name = "product"
	}
	if len(in.Name) > 256 || len(in.Param) > 2048 {
		return out, ErrInvalid
	}
	tx, e := s.begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = lockMerchant(ctx, tx, in.MerchantID); e != nil {
		return out, e
	}
	var payStatus int
	if e = tx.QueryRow(ctx, "SELECT COALESCE((to_jsonb(m)->>'pay')::int,1) FROM merchants m WHERE id=$1", in.MerchantID).Scan(&payStatus); e != nil {
		return out, e
	}
	if payStatus != 1 {
		return out, ErrInvalid
	}
	old, e := readInvoice(ctx, tx, "merchant_id=$1 AND out_trade_no=$2", in.MerchantID, in.OutTradeNo)
	if e == nil {
		if in.Network != "" && old.Network != in.Network || in.ChannelID > 0 && old.ChannelID != in.ChannelID || old.Amount != FormatAmount(n) || old.NotifyURL != in.NotifyURL || old.ReturnURL != in.ReturnURL || old.CNYAmount != in.CNYAmount || old.Name != in.Name || old.Param != in.Param || old.Version != in.Version {
			return out, ErrConflict
		}
		old.CheckoutToken = s.token(old.ID)
		return old, commit(ctx, tx)
	}
	if e != ErrNotFound {
		return out, e
	}
	var route channelAdmission
	if s.RequireChannels || in.ChannelID > 0 {
		route, e = s.admitChannel(ctx, tx, in, n)
		if e != nil {
			return out, e
		}
		in.Network = route.channel.Network
	}
	cfg, ok := s.Networks[in.Network]
	if !ok || !cfg.Enabled || cfg.FeeBPS < 0 || cfg.FeeBPS > 10000 {
		return out, ErrInvalid
	}
	var address string
	e = tx.QueryRow(ctx, "SELECT address FROM crypto_addresses WHERE network=$1 AND role='deposit' AND NOT used ORDER BY address FOR UPDATE SKIP LOCKED LIMIT 1", in.Network).Scan(&address)
	if e == pgx.ErrNoRows {
		return out, ErrUnavailable
	}
	if e != nil {
		return out, e
	}
	id := randomID()
	token := s.token(id)
	hash := sha256.Sum256([]byte(token))
	feeBPS := cfg.FeeBPS
	var channelID, revision, sealedFee any
	feeSource := "legacy_network"
	snapshot := []byte(`{}`)
	if route.channel.ID > 0 {
		channelID, revision, sealedFee = route.channel.ID, route.channel.Revision, route.feeBPS
		feeBPS, feeSource, snapshot = route.feeBPS, route.feeSource, route.snapshot
	}
	fee := n/10000*feeBPS + (n%10000*feeBPS)/10000
	if e = currentGroupDeadline(ctx, tx, route.validUntil); e != nil {
		return out, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO crypto_invoices(id,merchant_id,out_trade_no,network,address,amount,fee,token_hash,token_cipher,notify_url,return_url,cny_amount,expires_at,name,param,version,channel_id,channel_revision,fee_bps,fee_source,channel_snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8,''::bytea,$9,$10,$11,clock_timestamp()+$12*interval '1 second',$13,$14,$15,$16,$17,$18,$19,$20)`, id, in.MerchantID, in.OutTradeNo, in.Network, address, n, fee, hash[:], in.NotifyURL, in.ReturnURL, in.CNYAmount, int64(in.ExpiresIn.Seconds()), in.Name, in.Param, in.Version, channelID, revision, sealedFee, feeSource, snapshot)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, "UPDATE crypto_addresses SET used=true WHERE network=$1 AND address=$2", in.Network, address); e != nil {
		return out, e
	}
	out, e = readInvoice(ctx, tx, "id=$1", id)
	if e != nil {
		return out, e
	}
	out.CheckoutToken = token
	// The final identity/business proof can wait on locks. Check the effective
	// membership deadline only after that proof has returned, immediately before
	// commit, so a policy that expired during the wait cannot admit a new invoice.
	if e = authorize(ctx, tx); e != nil {
		return Invoice{}, e
	}
	if e = currentGroupDeadline(ctx, tx, route.validUntil); e != nil {
		return Invoice{}, e
	}
	return out, tx.Commit(ctx)
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

const invoiceColumns = "id,merchant_id,out_trade_no,network,address,amount,received,fee,CASE WHEN state IN ('pending','underpaid') AND clock_timestamp()>expires_at THEN 'expired' ELSE state END,expires_at,notify_url,return_url,cny_amount,name,param,version,COALESCE(channel_id,0),COALESCE(channel_revision,0),COALESCE(fee_bps,0),fee_source,channel_snapshot"

func scanInvoice(row pgx.Row) (Invoice, error) {
	var v Invoice
	var a, r, f int64
	e := row.Scan(&v.ID, &v.MerchantID, &v.OutTradeNo, &v.Network, &v.Address, &a, &r, &f, &v.State, &v.ExpiresAt, &v.NotifyURL, &v.ReturnURL, &v.CNYAmount, &v.Name, &v.Param, &v.Version, &v.ChannelID, &v.ChannelRevision, &v.FeeBPS, &v.FeeSource, &v.ChannelSnapshot)
	if e == pgx.ErrNoRows {
		return v, ErrNotFound
	}
	v.Amount = FormatAmount(a)
	v.Received = FormatAmount(r)
	v.Fee = FormatAmount(f)
	return v, e
}
func readInvoice(ctx context.Context, q queryer, where string, args ...any) (Invoice, error) {
	return scanInvoice(q.QueryRow(ctx, "SELECT "+invoiceColumns+" FROM crypto_invoices WHERE "+where, args...))
}
func (s *Service) GetInvoice(ctx context.Context, merchant int64, id string) (Invoice, error) {
	if s == nil || s.Pool == nil {
		return Invoice{}, ErrUnavailable
	}
	v, e := readInvoice(ctx, s.Pool, "id=$1 AND ($2::bigint=0 OR merchant_id=$2)", id, merchant)
	if e == nil && len(s.TokenKey) == 32 {
		v.CheckoutToken = s.token(id)
	}
	return v, e
}
func (s *Service) GetCheckout(ctx context.Context, id, token string) (Invoice, error) {
	if len(token) != 43 || s == nil || s.Pool == nil {
		return Invoice{}, ErrNotFound
	}
	var expected []byte
	e := s.Pool.QueryRow(ctx, "SELECT token_hash FROM crypto_invoices WHERE id=$1", id).Scan(&expected)
	hash := sha256.Sum256([]byte(token))
	if e != nil || subtle.ConstantTimeCompare(expected, hash[:]) != 1 {
		return Invoice{}, ErrNotFound
	}
	v, e := readInvoice(ctx, s.Pool, "id=$1", id)
	v.NotifyURL = ""
	v.ReturnURL = ""
	return v, e
}
func (s *Service) ListInvoices(ctx context.Context, merchant int64) ([]Invoice, error) {
	return s.ListChannelInvoices(ctx, merchant, 0)
}

func (s *Service) ListChannelInvoices(ctx context.Context, merchant, channelID int64) ([]Invoice, error) {
	if s == nil || s.Pool == nil {
		return nil, ErrUnavailable
	}
	if merchant < 0 || channelID < 0 {
		return nil, ErrInvalid
	}
	rows, e := s.Pool.Query(ctx, "SELECT "+invoiceColumns+" FROM crypto_invoices WHERE ($1::bigint=0 OR merchant_id=$1) AND ($2::bigint=0 OR channel_id=$2) ORDER BY created_at DESC,id DESC LIMIT 200", merchant, channelID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Invoice{}
	for rows.Next() {
		v, e := scanInvoice(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Service) RecordDeposit(ctx context.Context, in Deposit) error {
	n, e := ParseAmount(in.Amount)
	if e != nil || !validKey(in.TxID) || in.EventIndex < 0 || in.BlockHeight < 0 || in.BlockHash == "" || strings.TrimSpace(in.BlockHash) != in.BlockHash || in.OccurredAt.IsZero() {
		return ErrInvalid
	}
	a, e := normalize(in.Network, in.Address)
	if e != nil {
		return ErrInvalid
	}
	tx, e := s.begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// Serialize ingestion before matching: duplicate events never credit twice.
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,199))", in.Network); e != nil {
		return e
	}
	var oldAmount int64
	var oldAddress, oldBlock string
	var oldHeight int64
	e = tx.QueryRow(ctx, "SELECT amount,address,block_hash,block_height FROM crypto_deposits WHERE network=$1 AND tx_id=$2 AND event_index=$3", in.Network, in.TxID, in.EventIndex).Scan(&oldAmount, &oldAddress, &oldBlock, &oldHeight)
	if e == nil {
		if oldAmount != n || oldAddress != a || oldBlock != in.BlockHash || oldHeight != in.BlockHeight {
			return ErrConflict
		}
		return commit(ctx, tx)
	}
	if e != pgx.ErrNoRows {
		return e
	}
	var id, state string
	var owner, total, received, fee int64
	var expiry, created time.Time
	var credited bool
	e = tx.QueryRow(ctx, "SELECT id,merchant_id,amount,received,fee,state,expires_at,credited,created_at FROM crypto_invoices WHERE network=$1 AND address=$2 FOR UPDATE", in.Network, a).Scan(&id, &owner, &total, &received, &fee, &state, &expiry, &credited, &created)
	if e == pgx.ErrNoRows {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	if received > maxAmount-n {
		return ErrInvalid
	}
	_, e = tx.Exec(ctx, "INSERT INTO crypto_deposits(network,tx_id,event_index,block_height,block_hash,address,invoice_id,amount,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", in.Network, in.TxID, in.EventIndex, in.BlockHeight, in.BlockHash, a, id, n, in.OccurredAt)
	if e != nil {
		return e
	}
	if e = post(ctx, tx, in.Network, "deposit:"+in.TxID+":"+strconvIndex(in.EventIndex), posting{0, "clearing", -n}, posting{0, "suspense", n}); e != nil {
		return e
	}
	received += n
	if state == "late" || !credited && (in.OccurredAt.After(expiry) || in.OccurredAt.Unix() < created.Unix()) {
		state = "late"
	} else if received < total {
		state = "underpaid"
	} else {
		if !credited {
			if e = post(ctx, tx, in.Network, "invoice:"+id, posting{0, "suspense", -total}, posting{owner, "available", total - fee}, posting{0, "fees", fee}); e != nil {
				return e
			}
			credited = true
			if _, e = tx.Exec(ctx, "INSERT INTO crypto_notifications(invoice_id) VALUES($1)", id); e != nil {
				return e
			}
		}
		state = "paid"
		if received > total {
			state = "overpaid"
		}
	}
	_, e = tx.Exec(ctx, "UPDATE crypto_invoices SET received=$2,state=$3,credited=$4 WHERE id=$1", id, received, state, credited)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO crypto_sweep_queue(network,address) VALUES($1,$2) ON CONFLICT(network,address) DO UPDATE SET revision=crypto_sweep_queue.revision+1,next_attempt=CASE WHEN crypto_sweep_queue.failures<3 THEN clock_timestamp() ELSE 'infinity'::timestamptz END", in.Network, a); e != nil {
		return e
	}
	return commit(ctx, tx)
}
func (s *Service) FindInvoice(ctx context.Context, merchant int64, tradeNo, outTradeNo string) (Invoice, error) {
	if s == nil || s.Pool == nil {
		return Invoice{}, ErrUnavailable
	}
	if tradeNo != "" {
		if !strings.HasPrefix(tradeNo, "USDT") {
			return Invoice{}, ErrNotFound
		}
		return s.GetInvoice(ctx, merchant, strings.TrimPrefix(tradeNo, "USDT"))
	}
	v, e := readInvoice(ctx, s.Pool, "merchant_id=$1 AND out_trade_no=$2", merchant, outTradeNo)
	if e == nil && len(s.TokenKey) == 32 {
		v.CheckoutToken = s.token(v.ID)
	}
	return v, e
}
func strconvIndex(n int) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(digits[n%10]) + s
		n /= 10
	}
	return s
}
