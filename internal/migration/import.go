package migration

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/ledger"
	"strconv"
)

var ErrConflict = errors.New("legacy import conflicts with target or existing export")

// Apply imports one already sanitized snapshot atomically. It never contacts
// upstream payment systems and never generates jobs for historical paid orders.
func Apply(ctx context.Context, pool *pgxpool.Pool, master []byte, raw []byte) (Report, error) {
	plan, report, e := Validate(raw)
	if e != nil {
		return report, e
	}
	if pool == nil || len(master) != 32 {
		return report, ErrValidation
	}
	block, e := aes.NewCipher(master)
	if e != nil {
		return report, e
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		return report, e
	}
	tx, e := pool.Begin(ctx)
	if e != nil {
		return report, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(74973232)"); e != nil {
		return report, e
	}
	report, e = applyValidatedTx234(ctx, tx, master, plan, report, aead)
	if e != nil || report.AlreadyImported {
		return report, e
	}
	if e = tx.Commit(ctx); e != nil {
		report.Applied = false
		return report, e
	}
	return report, nil
}

// applyValidatedTx234 shares the original import statements with the wizard.
// The caller owns the transaction and holds advisory lock 74973232; wizard
// completion evidence can therefore commit atomically with the opening ledger.
func applyValidatedTx234(ctx context.Context, tx pgx.Tx, master []byte, plan Plan, report Report, aead cipher.AEAD) (Report, error) {
	var e error
	var previous string
	e = tx.QueryRow(ctx, "SELECT sha256 FROM legacy_import_runs WHERE export_id=$1", plan.ExportID).Scan(&previous)
	if e == nil {
		if previous != report.Digest {
			return report, ErrConflict
		}
		check, err := checkRefundHistory(ctx, tx, master)
		if err != nil || check.InvalidLegacyRefundHistory != 0 {
			return report, ErrConflict
		}
		if err = verifyCombinedReplay204(ctx, tx, master, plan); err != nil {
			return report, err
		}
		report.AlreadyImported = true
		return report, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return report, e
	}
	var journalExists bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM journals LIMIT 1)").Scan(&journalExists); e != nil {
		return report, e
	}
	if journalExists {
		return report, ErrConflict
	}
	for _, m := range plan.Merchants {
		var exists bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM merchants WHERE id=$1)", m.ID).Scan(&exists); e != nil {
			return report, e
		}
		if exists {
			return report, ErrConflict
		}
	}
	for _, o := range plan.Orders {
		var exists bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM orders WHERE trade_no=$1 OR (merchant_id=$2 AND out_trade_no=$3))", o.TradeNo, o.MerchantID, o.OutTradeNo).Scan(&exists); e != nil {
			return report, e
		}
		if exists {
			return report, ErrConflict
		}
	}
	for _, m := range plan.Merchants {
		key := make([]byte, 24)
		nonce := make([]byte, aead.NonceSize())
		if _, e = rand.Read(key); e != nil {
			return report, e
		}
		if _, e = rand.Read(nonce); e != nil {
			return report, e
		}
		encrypted := aead.Seal(nonce, nonce, []byte(base64.RawURLEncoding.EncodeToString(key)), []byte(fmt.Sprint(m.ID)))
		profile, _ := json.Marshal(map[string]string{"legacy_source": plan.Source, "legacy_export": plan.ExportID, "compat_profile": compatProfile(plan.Source)})
		var originalCreated any
		known := m.CreatedAt != ""
		if known {
			originalCreated, e = parseRegistrationTime(m.CreatedAt)
			if e != nil {
				return report, e
			}
		}
		if _, e = tx.Exec(ctx, "INSERT INTO merchants(id,name,api_key,enabled,signature_algorithm,profile,created_at,registration_time_known) VALUES($1,$2,$3,false,'MD5',$4::jsonb,COALESCE($5::timestamptz,transaction_timestamp()),$6)", m.ID, m.Name, encrypted, string(profile), originalCreated, known); e != nil {
			return report, e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO accounts(owner_id,kind) VALUES($1,'available'),($1,'reserved')", m.ID); e != nil {
			return report, e
		}
		available, _ := ledger.Parse(m.Available)
		reserved, _ := ledger.Parse(m.Reserved)
		if e = openingJournal(ctx, tx, plan.ExportID, m.ID, "available", available); e != nil {
			return report, e
		}
		if e = openingJournal(ctx, tx, plan.ExportID, m.ID, "reserved", reserved); e != nil {
			return report, e
		}
	}
	orders := map[string]int64{}
	maxTrade := int64(0)
	for _, o := range plan.Orders {
		created, _ := parseTime(o.CreatedAt)
		var paid any
		if o.PaidAt != "" {
			paid, _ = parseTime(o.PaidAt)
		}
		money, _ := ledger.Parse(o.Money)
		version := o.Version
		if version == "" {
			version = "v1"
		}
		var id int64
		e = tx.QueryRow(ctx, "INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,status,created_at,paid_at,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id", o.TradeNo, o.MerchantID, o.OutTradeNo, o.Name, o.Type, money, o.Status, created, paid, version).Scan(&id)
		if e != nil {
			return report, e
		}
		orders[o.TradeNo] = id
		if n, e := strconv.ParseInt(o.TradeNo, 10, 64); e == nil && n > maxTrade {
			maxTrade = n
		}
	}
	for _, r := range plan.Refunds {
		money, _ := ledger.Parse(r.Money)
		if e = archiveRefund(ctx, tx, master, plan.ExportID, plan.Source, report.Digest, orders[r.TradeNo], r, money); e != nil {
			return report, e
		}
	}
	for _, t := range plan.Transfers {
		money, _ := ledger.Parse(t.Money)
		if _, e = tx.Exec(ctx, "INSERT INTO transfers(merchant_id,transfer_no,amount,status) VALUES($1,$2,$3,$4)", t.MerchantID, t.TransferNo, money, t.Status); e != nil {
			return report, e
		}
	}
	if maxTrade > 0 {
		if _, e = tx.Exec(ctx, "SELECT setval('trade_no_seq',GREATEST((SELECT last_value FROM trade_no_seq),$1),true)", maxTrade); e != nil {
			return report, e
		}
	}
	if e = archiveCombinedOrders204(ctx, tx, master, plan, report.Digest, orders); e != nil {
		return report, e
	}
	report.Applied = true
	if e = archiveRefundManifest(ctx, tx, master, plan, report.Digest); e != nil {
		report.Applied = false
		return report, e
	}
	stored, _ := json.Marshal(report)
	receiptSQL := "INSERT INTO legacy_import_runs(export_id,source,sha256,report) VALUES($1,$2,$3,$4::jsonb)"
	if plan.CombinedOrders != nil {
		receiptSQL = "INSERT INTO legacy_import_runs(export_id,source,sha256,report,combined_history_version204) VALUES($1,$2,$3,$4::jsonb,1)"
	}
	if _, e = tx.Exec(ctx, receiptSQL, plan.ExportID, plan.Source, report.Digest, string(stored)); e != nil {
		report.Applied = false
		return report, e
	}
	return report, nil
}
func compatProfile(source string) string {
	if source == "epay_release" {
		return "release-legacy"
	}
	return "pro-v1"
}
func openingJournal(ctx context.Context, tx pgx.Tx, export string, merchantID int64, kind string, amount int64) error {
	if amount == 0 {
		return nil
	}
	var clearing, account, journal int64
	if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=0 AND kind='clearing' FOR UPDATE").Scan(&clearing); e != nil {
		return e
	}
	if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind=$2 FOR UPDATE", merchantID, kind).Scan(&account); e != nil {
		return e
	}
	event := fmt.Sprintf("legacy:%s:%d:%s", export, merchantID, kind)
	if e := tx.QueryRow(ctx, "INSERT INTO journals(event_key,description) VALUES($1,$2) RETURNING id", event, "legacy opening "+kind).Scan(&journal); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "UPDATE accounts SET balance=balance-$1 WHERE id=$2", amount, clearing); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "UPDATE accounts SET balance=balance+$1 WHERE id=$2", amount, account); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, "INSERT INTO ledger_entries(journal_id,account_id,amount) VALUES($1,$2,-$4::bigint),($1,$3,$4::bigint)", journal, clearing, account, amount)
	return e
}
