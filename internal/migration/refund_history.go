package migration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RefundHistory preserves an export's assertion. Its status never grants
// permission to resolve, query a provider, release funds, or create a refund.
type RefundHistory struct {
	ID                    int64     `json:"id,string"`
	ExportID              string    `json:"export_id"`
	MerchantID            int64     `json:"merchant_id,string"`
	TradeNo               string    `json:"trade_no"`
	RefundNo              string    `json:"refund_no"`
	Amount                int64     `json:"amount,string"`
	Status                string    `json:"legacy_status"`
	Source                string    `json:"source"`
	CreatedAt             time.Time `json:"created_at"`
	VerifiedPaymentSource bool      `json:"verified_payment_source"`
	orderID               int64
	digest                string
	proof                 []byte
	parentsValid          bool
}

type RefundHistoryReader struct {
	pool   *pgxpool.Pool
	master []byte
}

func NewRefundHistoryReader(pool *pgxpool.Pool, master []byte) (*RefundHistoryReader, error) {
	if pool == nil || len(master) != 32 {
		return nil, ErrValidation
	}
	return &RefundHistoryReader{pool: pool, master: append([]byte(nil), master...)}, nil
}

func (r *RefundHistoryReader) List(ctx context.Context, mid, after int64, limit int) ([]RefundHistory, error) {
	if r == nil {
		return nil, ErrValidation
	}
	return ListRefundHistory(ctx, r.pool, r.master, mid, after, limit)
}

func historyProof(master []byte, h RefundHistory) []byte {
	raw, _ := json.Marshal([]any{h.ID, h.ExportID, h.MerchantID, h.orderID, h.TradeNo, h.RefundNo, h.Amount, h.Status, h.Source, h.digest, h.CreatedAt.UTC().Format(time.RFC3339Nano)})
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte("goepay/legacy-refund-history/v1\x00"))
	mac.Write(raw)
	return mac.Sum(nil)
}

func archiveRefund(ctx context.Context, tx pgx.Tx, master []byte, export, source, digest string, orderID int64, r Refund, amount int64) error {
	h := RefundHistory{ExportID: export, Source: source, MerchantID: r.MerchantID, orderID: orderID, TradeNo: r.TradeNo, RefundNo: r.RefundNo, Amount: amount, Status: r.Status, digest: digest}
	if e := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('legacy_refund_history','id')),transaction_timestamp()`).Scan(&h.ID, &h.CreatedAt); e != nil {
		return e
	}
	h.proof = historyProof(master, h)
	_, e := tx.Exec(ctx, `INSERT INTO legacy_refund_history(id,export_id,merchant_id,order_id,trade_no,refund_no,amount,legacy_status,source_hash,proof_hash,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, h.ID, h.ExportID, h.MerchantID, h.orderID, h.TradeNo, h.RefundNo, h.Amount, h.Status, h.digest, h.proof, h.CreatedAt)
	return e
}

const historyColumns = `h.id,h.export_id,h.merchant_id,h.order_id,h.trade_no,h.refund_no,h.amount,h.legacy_status,h.source_hash,h.proof_hash,h.created_at,COALESCE(r.source,''),
 (r.export_id IS NOT NULL AND r.sha256=h.source_hash AND EXISTS(SELECT 1 FROM orders o WHERE (o.id,o.merchant_id,o.trade_no)=(h.order_id,h.merchant_id,h.trade_no)))`

func scanHistory(row pgx.Row) (RefundHistory, error) {
	var h RefundHistory
	e := row.Scan(&h.ID, &h.ExportID, &h.MerchantID, &h.orderID, &h.TradeNo, &h.RefundNo, &h.Amount, &h.Status, &h.digest, &h.proof, &h.CreatedAt, &h.Source, &h.parentsValid)
	return h, e
}

func historyValid(master []byte, h RefundHistory) bool {
	return h.parentsValid && len(h.proof) == 32 && hmac.Equal(h.proof, historyProof(master, h))
}

type refundManifest struct {
	Export, Source, Digest string
	Count, Unknown         int64
	At                     time.Time
	Proof                  []byte
	Valid                  bool
}

func manifestProof(master []byte, m refundManifest) []byte {
	raw, _ := json.Marshal([]any{m.Export, m.Source, m.Digest, m.Count, m.Unknown, m.At.UTC().Format(time.RFC3339Nano)})
	h := hmac.New(sha256.New, master)
	h.Write([]byte("goepay/legacy-refund-manifest/v1\x00"))
	h.Write(raw)
	return h.Sum(nil)
}

func archiveRefundManifest(ctx context.Context, tx pgx.Tx, master []byte, plan Plan, digest string) error {
	m := refundManifest{Export: plan.ExportID, Source: plan.Source, Digest: digest, Count: int64(len(plan.Refunds))}
	for _, r := range plan.Refunds {
		if r.Status == "unknown" {
			m.Unknown++
		}
	}
	if e := tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&m.At); e != nil {
		return e
	}
	m.Proof = manifestProof(master, m)
	_, e := tx.Exec(ctx, `INSERT INTO legacy_refund_manifests(export_id,source,source_hash,refund_count,unknown_count,proof_hash,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, m.Export, m.Source, m.Digest, m.Count, m.Unknown, m.Proof, m.At)
	return e
}

const manifestColumns = `r.export_id,COALESCE(m.source,''),COALESCE(m.source_hash,''),COALESCE(m.refund_count,0),COALESCE(m.unknown_count,0),COALESCE(m.created_at,r.imported_at),m.proof_hash,
 (m.export_id IS NOT NULL AND r.refund_history_version=2 AND (r.source,r.sha256)=(m.source,m.source_hash)
 AND m.refund_count=(SELECT count(*) FROM legacy_refund_history h WHERE h.export_id=r.export_id)
 AND m.unknown_count=(SELECT count(*) FROM legacy_refund_history h WHERE h.export_id=r.export_id AND h.legacy_status='unknown'))`

const manifestParents = ` FROM legacy_import_runs r LEFT JOIN legacy_refund_manifests m ON m.export_id=r.export_id WHERE r.refund_history_version=2 OR m.export_id IS NOT NULL OR EXISTS(SELECT 1 FROM legacy_refund_history h WHERE h.export_id=r.export_id)`

func scanManifest(row pgx.Row) (refundManifest, error) {
	var m refundManifest
	e := row.Scan(&m.Export, &m.Source, &m.Digest, &m.Count, &m.Unknown, &m.At, &m.Proof, &m.Valid)
	return m, e
}
func validManifest(master []byte, m refundManifest) bool {
	return m.Valid && len(m.Proof) == 32 && hmac.Equal(m.Proof, manifestProof(master, m))
}

// ListRefundHistory is scoped by the original session's merchant in HTTP.
// Callers cannot use a legacy outcome as trusted live refund evidence.
func ListRefundHistory(ctx context.Context, pool *pgxpool.Pool, master []byte, mid, after int64, limit int) ([]RefundHistory, error) {
	if pool == nil || len(master) != 32 || mid <= 0 || after < 0 || limit < 1 || limit > 100 {
		return nil, ErrValidation
	}
	tx, e := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	// The importer admits one initial dataset. Its manifest set is a global
	// integrity gate, independent of surviving children or mutable tenant
	// profiles: a missing owner's final row must not look like empty history.
	invalid, e := checkRefundManifests(ctx, tx, master)
	if e != nil {
		return nil, e
	}
	if invalid != 0 {
		return nil, ErrConflict
	}
	rows, e := tx.Query(ctx, `SELECT `+historyColumns+` FROM legacy_refund_history h LEFT JOIN legacy_import_runs r ON r.export_id=h.export_id WHERE h.merchant_id=$1 AND h.id>$2 ORDER BY h.id LIMIT $3`, mid, after, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := make([]RefundHistory, 0, limit)
	for rows.Next() {
		h, e := scanHistory(rows)
		if e != nil {
			return nil, e
		}
		if !historyValid(master, h) {
			return nil, ErrConflict
		}
		out = append(out, h)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	return out, tx.Commit(ctx)
}

type RefundHistoryCheck struct {
	InvalidLegacyRefundHistory int64 `json:"invalid_legacy_refund_history"`
	UnknownLegacyRefunds       int64 `json:"unknown_legacy_refunds"`
}

func CheckRefundHistory(ctx context.Context, pool *pgxpool.Pool, master []byte) (RefundHistoryCheck, error) {
	var out RefundHistoryCheck
	if pool == nil || len(master) != 32 {
		return out, ErrValidation
	}
	tx, e := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	out, e = checkRefundHistory(ctx, tx, master)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

// checkRefundHistory uses the caller's transaction so replay verification
// neither needs a second pool connection nor releases its import lock.
func checkRefundHistory(ctx context.Context, tx pgx.Tx, master []byte) (RefundHistoryCheck, error) {
	var out RefundHistoryCheck
	// A bounded page size avoids loading a potentially large import into memory.
	var after int64
	for {
		rows, e := tx.Query(ctx, `SELECT `+historyColumns+` FROM legacy_refund_history h LEFT JOIN legacy_import_runs r ON r.export_id=h.export_id WHERE h.id>$1 ORDER BY h.id LIMIT 100`, after)
		if e != nil {
			return out, e
		}
		n := 0
		for rows.Next() {
			h, e := scanHistory(rows)
			if e != nil {
				rows.Close()
				return out, e
			}
			n++
			after = h.ID
			if !historyValid(master, h) {
				out.InvalidLegacyRefundHistory++
			}
			if h.Status == "unknown" {
				out.UnknownLegacyRefunds++
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
		if n < 100 {
			break
		}
	}
	invalid, e := checkRefundManifests(ctx, tx, master)
	out.InvalidLegacyRefundHistory += invalid
	return out, e
}

// Stream parents as well as orphan manifests, including zero-child exports.
// The separately HMAC-protected counts detect omitted archive rows.
func checkRefundManifests(ctx context.Context, tx pgx.Tx, master []byte) (int64, error) {
	var invalid int64
	rows, e := tx.Query(ctx, `SELECT `+manifestColumns+manifestParents)
	if e != nil {
		return invalid, e
	}
	for rows.Next() {
		m, e := scanManifest(rows)
		if e != nil {
			rows.Close()
			return invalid, e
		}
		if !validManifest(master, m) {
			invalid++
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return invalid, e
	}
	var orphanManifests int64
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM legacy_refund_manifests m WHERE NOT EXISTS(SELECT 1 FROM legacy_import_runs r WHERE r.export_id=m.export_id)`).Scan(&orphanManifests); e != nil {
		return invalid, e
	}
	return invalid + orphanManifests, nil
}
