package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

type SubchannelListFilter struct {
	MerchantID, ID, ParentID int64
	PayType, Name            string
	Page, Size               int
}
type SubchannelPage struct {
	Items []PaymentSubchannel `json:"items"`
	Total int64               `json:"total"`
	Page  int                 `json:"page"`
	Size  int                 `json:"size"`
}

func joinedSubchannelColumns() string {
	return "c." + strings.ReplaceAll(childColumns, ",", ",c.") + `,(SELECT count(*) FROM backoffice_audit h WHERE h.resource='payment-subchannels' AND h.resource_id=c.id AND h.operation IN ('payment-subchannel-create','payment-subchannel-update','payment-subchannel-stop','payment-subchannel-archive')),a.subchannel_id,a.version,a.merchant_id,a.parent_id,a.pay_type,a.schema_hash,a.nonce,a.ciphertext,a.proof_hash`
}

const joinedSubchannelAccount = ` LEFT JOIN payment_subchannel_accounts a ON a.subchannel_id=c.id AND a.version=c.account_version `

func (s *Store) scanJoinedSubchannel(row pgx.Row) (PaymentSubchannel, error) {
	var c PaymentSubchannel
	var account subchannelAccount
	e := row.Scan(&c.ID, &c.MerchantID, &c.ParentID, &c.PayType, &c.Name, &c.Enabled, &c.Weight, &c.MinAmount, &c.MaxAmount, &c.Revision, &c.AccountVersion, &c.proof, &c.CreatedAt, &c.UpdatedAt, &c.Archived, &c.historyRevision, &account.Child, &account.Version, &account.Merchant, &account.Parent, &account.Kind, &account.Schema, &account.Nonce, &account.Ciphertext, &account.Proof)
	if e != nil {
		return c, e
	}
	return s.completeSubchannel(c, account)
}

func (s *Store) completeSubchannel(c PaymentSubchannel, a subchannelAccount) (PaymentSubchannel, error) {
	proof, e := s.childProof(c)
	accountProof, ae := s.accountProof(a)
	if e != nil || ae != nil || c.Revision <= 0 || c.historyRevision != c.Revision || !hmac.Equal(proof, c.proof) || !hmac.Equal(accountProof, a.Proof) || a.Child != c.ID || a.Version != c.AccountVersion || a.Merchant != c.MerchantID || a.Parent != c.ParentID || a.Kind != c.PayType {
		return c, ErrConflict
	}
	c.account = a
	c.SchemaFingerprint, e = s.childPublicSchema(a.Schema)
	if e != nil {
		return c, e
	}
	fingerprint, e := s.referralDigest("payment-subchannel168/state", []any{c.proof, a.Proof})
	if e != nil {
		return c, e
	}
	c.Fingerprint = hex.EncodeToString(fingerprint)
	c.Fields = []SubchannelFieldState{}
	c.FeeSource = "unknown"
	c.FeePolicy = "unknown"
	c.Methods = []string{}
	c.UnavailableReason = "account_unconfirmed"
	return c, nil
}
func (s *Store) SubchannelPage(ctx context.Context, a SubchannelActor, f SubchannelListFilter) (SubchannelPage, error) {
	out := SubchannelPage{Items: []PaymentSubchannel{}, Page: f.Page, Size: f.Size}
	if f.Page < 1 || f.Page > 1000000 || f.Size < 1 || f.Size > 100 || f.ID < 0 || f.ParentID < 0 || len(f.Name) > 120 || f.PayType != "" && f.PayType != "alipay" && f.PayType != "wxpay" {
		return out, ErrInvalid
	}
	if a.User.Role == "merchant" {
		if f.MerchantID != a.User.ID {
			return out, ErrInvalid
		}
	} else if a.User.Role != "admin" || a.User.AdminClass != "super" || f.MerchantID < 0 {
		return out, ErrInvalid
	}
	if a.Delegated {
		return out, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = a.check(ctx, tx); e != nil {
		return out, e
	}
	predicate := ` FROM payment_subchannels c WHERE NOT c.archived AND ($1::bigint=0 OR c.merchant_id=$1) AND ($2::bigint=0 OR c.id=$2) AND ($3::text='' OR c.pay_type=$3) AND ($4::text='' OR position(lower($4) in lower(c.name))>0) AND ($5::bigint=0 OR c.parent_id=$5)`
	args := []any{f.MerchantID, f.ID, f.PayType, f.Name, f.ParentID}
	if e = tx.QueryRow(ctx, `SELECT count(*)`+predicate, args...).Scan(&out.Total); e != nil {
		return out, e
	}
	query := `WITH page AS (SELECT c.*` + predicate + ` ORDER BY c.id LIMIT $6 OFFSET $7) SELECT ` + joinedSubchannelColumns() + ` FROM page c ` + joinedSubchannelAccount + ` ORDER BY c.id`
	args = append(args, f.Size, (f.Page-1)*f.Size)
	rows, e := tx.Query(ctx, query, args...)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		c, e := s.scanJoinedSubchannel(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, c)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return out, e
	}
	if e = s.childPageStatistics(ctx, tx, out.Items); e != nil {
		return out, e
	}
	if e = a.check(ctx, tx); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

// Statistics use only actual child assignments with born provider attempts and
// immutable verified payment receipts. Later refunds do not erase confirmation.
func (s *Store) childPageStatistics(ctx context.Context, tx pgx.Tx, items []PaymentSubchannel) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(items))
	byID := map[int64]*PaymentSubchannel{}
	for i := range items {
		ids = append(ids, items[i].ID)
		byID[items[i].ID] = &items[i]
	}
	var now time.Time
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return e
	}
	local := now.In(time.FixedZone("UTC+8", 8*3600))
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location()).UTC()
	rows, e := tx.Query(ctx, `SELECT c.id,count(a.id),count(v.order_id),COALESCE(sum(o.amount) FILTER(WHERE v.created_at >= $2::timestamptz),0),COALESCE(sum(o.amount) FILTER(WHERE v.created_at >= $3::timestamptz AND v.created_at < $2::timestamptz),0),CASE WHEN count(a.id)=0 THEN NULL ELSE floor(count(v.order_id)::numeric*10000/count(a.id))::bigint END FROM payment_subchannels c LEFT JOIN payment_subchannel_routes r ON r.subchannel_id=c.id AND r.merchant_id=c.merchant_id LEFT JOIN orders o ON o.merchant_id=r.merchant_id AND o.out_trade_no=r.out_trade_no LEFT JOIN provider_attempts a ON a.order_id=o.id AND a.channel_id=r.parent_id LEFT JOIN verified_payment_receipts v ON v.order_id=a.order_id AND v.channel_id=a.channel_id WHERE c.id=ANY($1::bigint[]) GROUP BY c.id`, ids, day, day.Add(-24*time.Hour))
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var stats SubchannelStatistics
		if e = rows.Scan(&id, &stats.Attempts, &stats.Confirmed, &stats.TodayAmount, &stats.YesterdayAmount, &stats.SuccessBPS); e != nil {
			return e
		}
		stats.Timezone = "UTC+8"
		stats.Basis = "verified_confirmation_gross_including_later_refunds"
		if c := byID[id]; c != nil {
			c.Statistics = &stats
		}
	}
	return rows.Err()
}
