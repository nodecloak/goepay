// Package noticesource231 contains the immutable birth receipt shared by the
// payment engine and its optional merchant/administrator notification stores.
// It never performs network I/O and stores only a digest of private facts.
package noticesource231

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

var ErrConflict = errors.New("notification source conflict")

type Facts struct {
	Kind                                                   string
	MerchantID, ReferenceID, ChannelID, PolicyRevision     int64
	Day, Reason, PolicyHash, Recipient, AppID, PackageHash string
	Params                                                 map[string]string
}
type Source struct {
	ID          int64
	Facts       Facts
	Transaction string
	At          time.Time
}

func Hash(f Facts) []byte { b, _ := json.Marshal(f); h := sha256.Sum256(b); return h[:] }
func Insert(ctx context.Context, tx pgx.Tx, key string, f Facts) (Source, bool, error) {
	var s Source
	s.Facts = f
	var mid any
	if f.MerchantID > 0 {
		mid = f.MerchantID
	}
	e := tx.QueryRow(ctx, `INSERT INTO notification_sources231(source_key,kind,merchant_id,reference_id,channel_id,facts_hash) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(source_key) DO NOTHING RETURNING id,source_transaction::text,created_at`, key, f.Kind, mid, f.ReferenceID, f.ChannelID, Hash(f)).Scan(&s.ID, &s.Transaction, &s.At)
	if errors.Is(e, pgx.ErrNoRows) {
		return s, false, nil
	}
	return s, e == nil, e
}
func Verify(ctx context.Context, tx pgx.Tx, s Source) error {
	var hash []byte
	var mid, ref, ch int64
	var kind, tr string
	var at time.Time
	e := tx.QueryRow(ctx, `SELECT kind,COALESCE(merchant_id,0),reference_id,channel_id,facts_hash,source_transaction::text,created_at FROM notification_sources231 WHERE id=$1`, s.ID).Scan(&kind, &mid, &ref, &ch, &hash, &tr, &at)
	if e != nil {
		return e
	}
	if kind != s.Facts.Kind || mid != s.Facts.MerchantID || ref != s.Facts.ReferenceID || ch != s.Facts.ChannelID || tr != s.Transaction || !at.Equal(s.At) || subtle.ConstantTimeCompare(hash, Hash(s.Facts)) != 1 {
		return ErrConflict
	}
	return nil
}
