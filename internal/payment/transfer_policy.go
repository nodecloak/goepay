package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/ledger"
	"strconv"
	"strings"
	"unicode/utf8"
)

type TransferPolicy struct {
	Enabled                                           bool
	MinimumAmount, MaximumAmount, DailyRecipientLimit int64
	DefaultRemark, DefaultTitle                       string
}
type transferPolicyReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (s *Store) TransferPolicy(ctx context.Context) (TransferPolicy, error) {
	return readTransferPolicy(ctx, s.pool)
}
func readTransferPolicy(ctx context.Context, q transferPolicyReader) (TransferPolicy, error) {
	var p TransferPolicy
	rows, e := q.Query(ctx, "SELECT body->>'key',body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key' IN ('transfer_enabled','transfer_minmoney','transfer_maxmoney','transfer_maxlimit','transfer_desc','transfer_name') ORDER BY id FOR SHARE")
	if e != nil {
		return p, e
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if e = rows.Scan(&k, &v); e != nil {
			return p, e
		}
		switch k {
		case "transfer_enabled":
			if v != "true" && v != "false" {
				return p, ErrInvalid
			}
			p.Enabled = v == "true"
		case "transfer_minmoney", "transfer_maxmoney":
			n, pe := ledger.Parse(v)
			if pe != nil {
				return p, ErrInvalid
			}
			if k == "transfer_minmoney" {
				p.MinimumAmount = n
			} else {
				p.MaximumAmount = n
			}
		case "transfer_maxlimit":
			n, pe := strconv.ParseInt(v, 10, 64)
			if pe != nil || n < 0 || n > 1000000 {
				return p, ErrInvalid
			}
			p.DailyRecipientLimit = n
		case "transfer_desc", "transfer_name":
			if !utf8.ValidString(v) || utf8.RuneCountInString(v) > 32 || strings.ContainsAny(v, "\x00\r\n") {
				return p, ErrInvalid
			}
			if k == "transfer_desc" {
				p.DefaultRemark = v
			} else {
				p.DefaultTitle = v
			}
		}
	}
	if e = rows.Err(); e != nil {
		return p, e
	}
	if p.MaximumAmount > 0 && p.MinimumAmount > p.MaximumAmount {
		return p, ErrInvalid
	}
	return p, nil
}
func (s *Store) transferPolicyHashes(mid int64, r adapter.TransferRequest) ([]byte, []byte, error) {
	if len(s.indexKey) != 32 {
		return nil, nil, ErrInvalid
	}
	account := r.Account
	if r.Type == "alipay" {
		account = strings.ToLower(account)
	}
	recipient, _ := json.Marshal([]string{fmt.Sprint(mid), r.Type, account})
	r.TransferNo = ""
	input, _ := json.Marshal(r)
	digest := func(domain string, value []byte) []byte {
		m := hmac.New(sha256.New, s.indexKey)
		m.Write([]byte(domain))
		m.Write([]byte{0})
		m.Write(value)
		return m.Sum(nil)
	}
	return digest("recipient", recipient), digest("request", input), nil
}

// Old encrypted intents are indexed lazily under the same merchant lock before
// enabling quota checks. This prevents pre-upgrade unknown transfers escaping
// the new limit, without placing recipient plaintext in an SQL index.
func (s *Store) indexLegacyTransferPolicies(ctx context.Context, tx pgx.Tx, mid int64) error {
	rows, e := tx.Query(ctx, "SELECT "+transferColumns+transferFrom+" LEFT JOIN provider_transfer_policies p ON p.transfer_id=t.id WHERE t.merchant_id=$1 AND a.transfer_id IS NOT NULL AND p.transfer_id IS NULL", mid)
	if e != nil {
		return e
	}
	var existing []Transfer
	for rows.Next() {
		t, se := s.scanTransfer(rows)
		if se != nil {
			rows.Close()
			return se
		}
		existing = append(existing, t)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	for _, t := range existing {
		recipient, request, he := s.transferPolicyHashes(mid, transferRequest(t))
		if he != nil {
			return he
		}
		if _, e = tx.Exec(ctx, "INSERT INTO provider_transfer_policies(transfer_id,merchant_id,recipient_hash,request_hash) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", t.ID, mid, recipient, request); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) enforceTransferPolicy(ctx context.Context, tx pgx.Tx, mid int64, r adapter.TransferRequest, p TransferPolicy, recipient []byte) error {
	if p.MinimumAmount > 0 && r.Amount < p.MinimumAmount || p.MaximumAmount > 0 && r.Amount > p.MaximumAmount {
		return ErrInvalid
	}
	if p.DailyRecipientLimit == 0 {
		return nil
	}
	if e := s.indexLegacyTransferPolicies(ctx, tx, mid); e != nil {
		return e
	}
	var count int64
	e := tx.QueryRow(ctx, `SELECT count(*) FROM provider_transfer_policies p JOIN transfers t ON t.id=p.transfer_id WHERE p.merchant_id=$1 AND p.recipient_hash=$2 AND NOT EXISTS(SELECT 1 FROM bank_transfer_lifecycle b WHERE b.transfer_id=t.id AND b.state IN ('reversed','failed')) AND (t.status IN ('pending','unknown') OR (t.status='succeeded' AND t.resolved_at >= ((now() AT TIME ZONE 'Asia/Shanghai')::date::timestamp AT TIME ZONE 'Asia/Shanghai')))`, mid, recipient).Scan(&count)
	if e != nil {
		return e
	}
	if count >= p.DailyRecipientLimit {
		return ErrState
	}
	return nil
}
