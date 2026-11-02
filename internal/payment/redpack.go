package payment

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/ledger"
)

// Redpack is one 扫码红包: a deferred transfer to whoever claims it.
type Redpack struct {
	ID         int64      `json:"id,string"`
	MerchantID int64      `json:"merchant_id,string"`
	OutBizNo   string     `json:"out_biz_no"`
	ChannelID  int64      `json:"channel_id,string"`
	Type       string     `json:"type"`
	Amount     int64      `json:"amount"`
	Remark     string     `json:"remark"`
	CreatedBy  string     `json:"created_by"`
	Status     string     `json:"status"`
	ExpiresAt  time.Time  `json:"expires_at"`
	ClaimedAt  *time.Time `json:"claimed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// RedpackInput is the original create form: 交易号, 通道, 红包金额, 红包备注.
type RedpackInput struct {
	MerchantID, ChannelID, ActorID int64
	OutBizNo, Type, Remark, Role   string
	Amount                         int64
}

var redpackBizNo = regexp.MustCompile(`^[0-9]{19}$`)

// RedpackLifetime matches the original 24h QR validity.
const RedpackLifetime = 24 * time.Hour

var ErrRedpackClaimed = errors.New("red packet already claimed")
var ErrRedpackExpired = errors.New("red packet expired")

const redpackColumns = `id,merchant_id,out_biz_no,channel_id,type,amount,remark,created_by,status,expires_at,claimed_at,created_at`

func scanRedpack(row pgx.Row) (Redpack, error) {
	var r Redpack
	err := row.Scan(&r.ID, &r.MerchantID, &r.OutBizNo, &r.ChannelID, &r.Type, &r.Amount, &r.Remark, &r.CreatedBy, &r.Status, &r.ExpiresAt, &r.ClaimedAt, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// RedpackTransferNo is the deterministic transfer number of a red packet's
// single claim transfer.
func RedpackTransferNo(id int64) string { return fmt.Sprintf("RP%d", id) }

func validRedpackInput(in RedpackInput) bool {
	return in.MerchantID > 0 && in.ChannelID > 0 && in.ActorID > 0 && redpackBizNo.MatchString(in.OutBizNo) &&
		(in.Type == "wxpay" || in.Type == "alipay") && in.Amount > 0 && (in.Role == "admin" || in.Role == "merchant") &&
		utf8.ValidString(in.Remark) && utf8.RuneCountInString(in.Remark) <= 32 && !strings.ContainsAny(in.Remark, "\x00\r\n")
}

// CreateRedpack records a red packet. It checks the original amount limits
// and that the merchant can currently cover amount+fee, but reserves nothing:
// the claim transfer reserves at claim time. The same 交易号 with the same
// content is idempotent; different content is a conflict (该交易号已存在).
func (s *Store) CreateRedpack(ctx context.Context, in RedpackInput) (Redpack, error) {
	if !validRedpackInput(in) {
		return Redpack{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Redpack{}, err
	}
	defer tx.Rollback(ctx)
	var enabled bool
	var profile []byte
	err = tx.QueryRow(ctx, `SELECT enabled,profile FROM merchants WHERE id=$1 FOR NO KEY UPDATE`, in.MerchantID).Scan(&enabled, &profile)
	if errors.Is(err, pgx.ErrNoRows) {
		return Redpack{}, ErrNotFound
	}
	if err != nil {
		return Redpack{}, err
	}
	if !enabled {
		return Redpack{}, ErrState
	}
	old, err := scanRedpack(tx.QueryRow(ctx, `SELECT `+redpackColumns+` FROM redpacks WHERE merchant_id=$1 AND out_biz_no=$2`, in.MerchantID, in.OutBizNo))
	if err == nil {
		if old.ChannelID != in.ChannelID || old.Type != in.Type || old.Amount != in.Amount || old.Remark != in.Remark {
			return Redpack{}, ErrConflict
		}
		return old, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Redpack{}, err
	}
	if err = s.admitTransferRouting228(ctx, tx, in.Type); err != nil {
		return Redpack{}, err
	}
	var feature merchantRedpackFeature
	if in.Role == "merchant" {
		feature, err = readMerchantRedpackFeature(ctx, tx, in.MerchantID, true)
		if err != nil {
			return Redpack{}, err
		}
		var merchant struct {
			TransferEnabled bool `json:"transfer_api_enabled"`
		}
		if !feature.Enabled || json.Unmarshal(profile, &merchant) != nil || !merchant.TransferEnabled {
			return Redpack{}, ErrState
		}
		ordinary, err := readManualTransferPolicy(ctx, tx, in.MerchantID)
		if err != nil {
			return Redpack{}, err
		}
		if !ordinary.Enabled {
			return Redpack{}, ErrState
		}
	}
	policy, err := readTransferPolicyMode(ctx, tx, false)
	if err != nil {
		return Redpack{}, err
	}
	if policy.MinimumAmount > 0 && in.Amount < policy.MinimumAmount || policy.MaximumAmount > 0 && in.Amount > policy.MaximumAmount {
		return Redpack{}, ErrInvalid
	}
	selectedFee, err := readMerchantTransferFeeSelection(ctx, tx, in.MerchantID, true)
	if err != nil {
		return Redpack{}, err
	}
	cost, _, _, err := transferCost(in.Amount, ledger.Format(selectedFee.RateBPS))
	if err != nil {
		return Redpack{}, err
	}
	funds, err := s.readFundsAvailability(ctx, tx, in.MerchantID, nil)
	if err != nil {
		return Redpack{}, err
	}
	if funds.Available < cost {
		return Redpack{}, ErrInsufficient
	}
	if err = selectedFee.checkDeadline(ctx, tx); err != nil {
		return Redpack{}, err
	}
	if err = checkGroupTransferDeadline(ctx, tx, feature.ValidUntil); err != nil {
		return Redpack{}, err
	}
	out, err := scanRedpack(tx.QueryRow(ctx, `INSERT INTO redpacks(merchant_id,out_biz_no,channel_id,type,amount,remark,created_by,actor_id,expires_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,clock_timestamp()+make_interval(secs=>$9)) RETURNING `+redpackColumns,
		in.MerchantID, in.OutBizNo, in.ChannelID, in.Type, in.Amount, in.Remark, in.Role, in.ActorID, RedpackLifetime.Seconds()))
	if err != nil {
		return Redpack{}, err
	}
	if err = selectedFee.checkDeadline(ctx, tx); err != nil {
		return Redpack{}, err
	}
	if err = checkGroupTransferDeadline(ctx, tx, feature.ValidUntil); err != nil {
		return Redpack{}, err
	}
	return out, tx.Commit(ctx)
}

// Redpack reads one red packet.
func (s *Store) RedpackByExternal228(ctx context.Context, mid int64, no string) (Redpack, error) {
	return scanRedpack(s.pool.QueryRow(ctx, `SELECT `+redpackColumns+` FROM redpacks WHERE merchant_id=$1 AND out_biz_no=$2`, mid, no))
}

func (s *Store) Redpack(ctx context.Context, id int64) (Redpack, error) {
	return scanRedpack(s.pool.QueryRow(ctx, `SELECT `+redpackColumns+` FROM redpacks WHERE id=$1`, id))
}

func (s *Store) redpackClaimerHash(id int64, account string) ([]byte, error) {
	return s.referralDigest("redpack/claimer", []any{id, account})
}

// beginRedpackClaim atomically moves a claimable, unexpired packet to
// claiming for exactly one claimer. A repeat by the same claimer resumes;
// anyone else gets ErrRedpackClaimed.
func (s *Store) beginRedpackClaim(ctx context.Context, id int64, account string) (Redpack, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Redpack{}, false, err
	}
	defer tx.Rollback(ctx)
	r, err := scanRedpack(tx.QueryRow(ctx, `SELECT `+redpackColumns+` FROM redpacks WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return r, false, err
	}
	hash, err := s.redpackClaimerHash(id, account)
	if err != nil {
		return r, false, err
	}
	switch r.Status {
	case "claimable":
		if err = s.admitTransferRouting228(ctx, tx, r.Type); err != nil {
			return r, false, err
		}
		var live bool
		if err = tx.QueryRow(ctx, `SELECT $1>clock_timestamp()`, r.ExpiresAt).Scan(&live); err != nil {
			return r, false, err
		}
		if !live {
			return r, false, ErrRedpackExpired
		}
		if _, err = tx.Exec(ctx, `UPDATE redpacks SET status='claiming',claimer_hash=$2,claimed_at=clock_timestamp() WHERE id=$1 AND status='claimable'`, id, hash); err != nil {
			return r, false, err
		}
		r.Status = "claiming"
		return r, true, tx.Commit(ctx)
	case "expired":
		return r, false, ErrRedpackExpired
	default:
		var stored []byte
		if err = tx.QueryRow(ctx, `SELECT claimer_hash FROM redpacks WHERE id=$1`, id).Scan(&stored); err != nil {
			return r, false, err
		}
		if !hmac.Equal(stored, hash) {
			return r, false, ErrRedpackClaimed
		}
		return r, false, nil
	}
}

func (s *Store) finishRedpack(ctx context.Context, id int64, transferStatus string) error {
	next := ""
	switch transferStatus {
	case "succeeded":
		next = "succeeded"
	case "failed":
		next = "failed"
	default:
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE redpacks SET status=$2 WHERE id=$1 AND status='claiming'`, id, next)
	return err
}

// ClaimRedpack pays a red packet to account (the claimer's openid / Alipay
// user id obtained by the platform's own authorization, never typed in).
// It runs one ordinary merchant transfer RP<id>; an unknown outcome stays
// claiming and is resolved only by QueryRedpack, never by a second send.
func (g ProviderFlow) ClaimRedpack(ctx context.Context, id int64, account string) (Redpack, Transfer, error) {
	if g.Store == nil || id <= 0 || account == "" || len(account) > 128 {
		return Redpack{}, Transfer{}, ErrInvalid
	}
	r, _, err := g.Store.beginRedpackClaim(ctx, id, account)
	if err != nil {
		return r, Transfer{}, err
	}
	if r.Status == "succeeded" || r.Status == "failed" {
		t, err := g.Store.Transfer(ctx, r.MerchantID, RedpackTransferNo(r.ID))
		return r, t, err
	}
	req := adapter.TransferRequest{Type: r.Type, Account: account, Remark: r.Remark, Amount: r.Amount}
	if r.Type == "alipay" {
		req.Title, req.Remark = r.Remark, "" // as an ordinary Alipay transfer
	}
	t, err := g.Transfer(ctx, r.MerchantID, r.ChannelID, RedpackTransferNo(r.ID), req)
	if t.ID > 0 {
		if ferr := g.Store.finishRedpack(ctx, r.ID, t.Status); ferr != nil {
			return r, t, errors.Join(err, ferr)
		}
	}
	if err == nil && t.ID == 0 {
		err = ErrState
	}
	out, rerr := g.Store.Redpack(ctx, r.ID)
	if rerr != nil {
		return r, t, errors.Join(err, rerr)
	}
	return out, t, err
}

// A browser resumption authenticates the original claimer before observing the
// old transfer. Querying an existing intent never requires enabling new sends.
func (g ProviderFlow) ResumeRedpack228(ctx context.Context, id int64, account string) (Redpack, Transfer, error) {
	if g.Store == nil || id <= 0 || account == "" || len(account) > 128 {
		return Redpack{}, Transfer{}, ErrInvalid
	}
	r, _, err := g.Store.beginRedpackClaim(ctx, id, account)
	if err != nil {
		return r, Transfer{}, err
	}
	t, err := g.Store.Transfer(ctx, r.MerchantID, RedpackTransferNo(id))
	if err == nil {
		if r.Status != "claiming" {
			return r, t, nil
		}
		return g.QueryRedpack(ctx, id)
	}
	if !errors.Is(err, ErrNotFound) {
		return r, t, err
	}
	return g.ClaimRedpack(ctx, id, account)
}

// QueryRedpack resolves a claiming red packet from its claim transfer.
func (g ProviderFlow) QueryRedpack(ctx context.Context, id int64) (Redpack, Transfer, error) {
	r, err := g.Store.Redpack(ctx, id)
	if err != nil || r.Status != "claiming" {
		return r, Transfer{}, err
	}
	t, err := g.QueryTransfer(ctx, r.MerchantID, RedpackTransferNo(r.ID))
	if err != nil {
		return r, t, err
	}
	if err = g.Store.finishRedpack(ctx, r.ID, t.Status); err != nil {
		return r, t, err
	}
	r, err = g.Store.Redpack(ctx, id)
	return r, t, err
}

// ExpireRedpacks marks unclaimed packets past their deadline expired. Nothing
// was reserved for them, so no balance needs to be returned.
func (s *Store) ExpireRedpacks(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE redpacks SET status='expired' WHERE status='claimable' AND expires_at<=clock_timestamp()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Redpacks lists red packets newest first; merchantID 0 lists all (admin).
func (s *Store) Redpacks(ctx context.Context, merchantID int64, limit int) ([]Redpack, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT `+redpackColumns+` FROM redpacks WHERE $1=0 OR merchant_id=$1 ORDER BY id DESC LIMIT $2`, merchantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Redpack{}
	for rows.Next() {
		r, err := scanRedpack(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
