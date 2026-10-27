package migration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"goepay/internal/ledger"
	"io"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrValidation = errors.New("legacy snapshot validation failed")

type Merchant struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Available string `json:"available_money"`
	Reserved  string `json:"reserved_money"`
	CreatedAt string `json:"created_at,omitempty"`
}
type Order struct {
	TradeNo    string `json:"trade_no"`
	MerchantID int64  `json:"merchant_id"`
	OutTradeNo string `json:"out_trade_no"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Money      string `json:"money"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	PaidAt     string `json:"paid_at,omitempty"`
	Version    string `json:"version,omitempty"`
}
type Refund struct {
	MerchantID int64  `json:"merchant_id"`
	TradeNo    string `json:"trade_no"`
	RefundNo   string `json:"refund_no"`
	Money      string `json:"money"`
	Status     string `json:"status"`
}
type Transfer struct {
	MerchantID int64  `json:"merchant_id"`
	TransferNo string `json:"transfer_no"`
	Money      string `json:"money"`
	Status     string `json:"status"`
}
type Plan struct {
	SchemaVersion  int                        `json:"schema_version"`
	ExportID       string                     `json:"export_id"`
	Source         string                     `json:"source"`
	ExportedAt     string                     `json:"exported_at"`
	Merchants      []Merchant                 `json:"merchants"`
	Orders         []Order                    `json:"orders"`
	Refunds        []Refund                   `json:"refunds"`
	Transfers      []Transfer                 `json:"transfers"`
	CombinedOrders []CombinedOrderSnapshot204 `json:"combined_orders,omitempty"`
}
type Issue struct {
	Path string `json:"path"`
	Code string `json:"code"`
}
type Report struct {
	RefundHistoryVersion int     `json:"refund_history_version"`
	Valid                bool    `json:"valid"`
	Digest               string  `json:"sha256"`
	Merchants            int     `json:"merchant_count"`
	Orders               int     `json:"order_count"`
	Refunds              int     `json:"refund_count"`
	Transfers            int     `json:"transfer_count"`
	AvailableTotal       string  `json:"available_total"`
	ReservedTotal        string  `json:"reserved_total"`
	Issues               []Issue `json:"issues"`
	Warnings             []Issue `json:"warnings"`
	Applied              bool    `json:"applied"`
	AlreadyImported      bool    `json:"already_imported"`
}

func parseTime(s string) (time.Time, error) {
	if t, e := time.Parse(time.RFC3339, s); e == nil {
		return t, nil
	}
	return time.ParseInLocation("2006-01-02 15:04:05", s, time.FixedZone("legacy-UTC8", 8*3600))
}
func goodString(s string, max int) bool {
	return s != "" && len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func Validate(raw []byte) (Plan, Report, error) {
	var plan Plan
	digest := sha256.Sum256(raw)
	report := Report{RefundHistoryVersion: 2, Digest: hex.EncodeToString(digest[:]), Issues: []Issue{}, Warnings: []Issue{}}
	issue := func(path, code string) { report.Issues = append(report.Issues, Issue{path, code}) }
	if len(raw) > 64<<20 {
		issue("$", "file_too_large")
		return plan, report, ErrValidation
	}
	if e := noDuplicateJSON(raw); e != nil {
		issue("$", "invalid_or_duplicate_json")
		return plan, report, ErrValidation
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&plan); e != nil {
		issue("$", "schema_mismatch")
		return plan, report, ErrValidation
	}
	if plan.SchemaVersion != 1 {
		issue("schema_version", "unsupported_version")
	}
	if !goodString(plan.ExportID, 128) {
		issue("export_id", "invalid_identifier")
	}
	if plan.Source != "epay_pro" && plan.Source != "epay_release" {
		issue("source", "unsupported_source")
	}
	exportedAt, exportedErr := time.Parse(time.RFC3339, plan.ExportedAt)
	if exportedErr != nil {
		issue("exported_at", "invalid_timestamp")
	}
	report.Merchants = len(plan.Merchants)
	report.Orders = len(plan.Orders)
	report.Refunds = len(plan.Refunds)
	report.Transfers = len(plan.Transfers)
	if len(plan.Merchants) == 0 {
		issue("merchants", "empty_snapshot")
	}
	merchants := map[int64]Merchant{}
	available, reserved := int64(0), int64(0)
	reserveByMerchant := map[int64]int64{}
	holds := map[int64]int64{}
	money := func(s, path string, allowZero bool) int64 {
		n, e := ledger.Parse(s)
		if e != nil || n < 0 || !allowZero && n == 0 {
			issue(path, "invalid_money")
			return 0
		}
		return n
	}
	add := func(current *int64, n int64, path string) {
		if n > math.MaxInt64-*current {
			issue(path, "money_overflow")
			return
		}
		*current += n
	}
	for i, m := range plan.Merchants {
		path := fmt.Sprintf("merchants[%d]", i)
		if m.CreatedAt != "" {
			created, err := parseRegistrationTime(m.CreatedAt)
			if err != nil || created.After(exportedAt) {
				issue(path+".created_at", "invalid_registration_timestamp")
			}
		}
		if m.ID <= 0 || !goodString(m.Name, 128) {
			issue(path, "invalid_merchant")
		}
		if _, exists := merchants[m.ID]; exists {
			issue(path, "duplicate_merchant")
		}
		merchants[m.ID] = m
		a := money(m.Available, path+".available_money", true)
		r := money(m.Reserved, path+".reserved_money", true)
		add(&available, a, path)
		add(&reserved, r, path)
		reserveByMerchant[m.ID] = r
	}
	orders := map[string]Order{}
	business := map[string]bool{}
	refundTotals := map[string]int64{}
	for i, o := range plan.Orders {
		path := fmt.Sprintf("orders[%d]", i)
		if _, ok := merchants[o.MerchantID]; !ok {
			issue(path, "unknown_merchant")
		}
		if !goodString(o.TradeNo, 128) || !goodString(o.OutTradeNo, 128) || !goodString(o.Type, 32) || !goodString(o.Name, 256) {
			issue(path, "invalid_order")
		}
		if _, ok := orders[o.TradeNo]; ok {
			issue(path, "duplicate_trade_number")
		}
		orders[o.TradeNo] = o
		key := fmt.Sprintf("%d:%s", o.MerchantID, o.OutTradeNo)
		if business[key] {
			issue(path, "duplicate_business_number")
		}
		business[key] = true
		money(o.Money, path+".money", false)
		if o.Status != "created" && o.Status != "paid" && o.Status != "closed" && o.Status != "failed" && o.Status != "unknown" {
			issue(path, "unsupported_order_status")
		}
		if _, e := parseTime(o.CreatedAt); e != nil {
			issue(path, "invalid_created_time")
		}
		if o.Status == "paid" {
			paid, e := parseTime(o.PaidAt)
			created, ce := parseTime(o.CreatedAt)
			if e != nil || ce == nil && paid.Before(created) {
				issue(path, "invalid_paid_time")
			}
		} else if o.PaidAt != "" {
			issue(path, "unpaid_with_paid_timestamp")
		}
		if o.Version != "" && o.Version != "v1" && o.Version != "v2" {
			issue(path, "unsupported_protocol_version")
		}
	}
	validateCombinedOrders204(plan, orders, issue)
	refundKeys := map[string]bool{}
	for i, r := range plan.Refunds {
		path := fmt.Sprintf("refunds[%d]", i)
		report.Warnings = append(report.Warnings, Issue{path, "legacy_refund_is_read_only_history"})
		o, ok := orders[r.TradeNo]
		if !ok || o.MerchantID != r.MerchantID {
			issue(path, "unknown_order")
		}
		if o.Status != "paid" {
			issue(path, "refund_for_unpaid_order")
		}
		if !goodString(r.RefundNo, 128) {
			issue(path, "invalid_refund_number")
		}
		key := fmt.Sprintf("%d:%s", r.MerchantID, r.RefundNo)
		if refundKeys[key] {
			issue(path, "duplicate_refund_number")
		}
		refundKeys[key] = true
		n := money(r.Money, path+".money", false)
		if r.Status != "succeeded" && r.Status != "failed" && r.Status != "unknown" {
			issue(path, "unsupported_refund_status")
		}
		if r.Status != "failed" {
			v := refundTotals[r.TradeNo]
			add(&v, n, path)
			refundTotals[r.TradeNo] = v
			amount, _ := ledger.Parse(o.Money)
			if v > amount {
				issue(path, "refund_above_order_amount")
			}
		}
		if r.Status == "unknown" {
			v := holds[r.MerchantID]
			add(&v, n, path)
			holds[r.MerchantID] = v
			report.Warnings = append(report.Warnings, Issue{path, "unknown_refund_requires_manual_provider_mapping"})
		}
	}
	transferKeys := map[string]bool{}
	for i, t := range plan.Transfers {
		path := fmt.Sprintf("transfers[%d]", i)
		if _, ok := merchants[t.MerchantID]; !ok {
			issue(path, "unknown_merchant")
		}
		if !goodString(t.TransferNo, 128) {
			issue(path, "invalid_transfer_number")
		}
		key := fmt.Sprintf("%d:%s", t.MerchantID, t.TransferNo)
		if transferKeys[key] {
			issue(path, "duplicate_transfer_number")
		}
		transferKeys[key] = true
		n := money(t.Money, path+".money", false)
		if t.Status != "succeeded" && t.Status != "failed" && t.Status != "unknown" {
			issue(path, "unsupported_transfer_status")
		}
		if t.Status == "unknown" {
			v := holds[t.MerchantID]
			add(&v, n, path)
			holds[t.MerchantID] = v
			report.Warnings = append(report.Warnings, Issue{path, "unknown_transfer_requires_manual_provider_mapping"})
		}
	}
	for id, reserve := range reserveByMerchant {
		if reserve != holds[id] {
			issue(fmt.Sprintf("merchant:%d", id), "reserved_amount_does_not_match_unknown_obligations")
		}
	}
	if available > math.MaxInt64-reserved {
		issue("$", "opening_balance_overflow")
	}
	report.AvailableTotal = ledger.Format(available)
	report.ReservedTotal = ledger.Format(reserved)
	report.Valid = len(report.Issues) == 0
	if !report.Valid {
		return plan, report, ErrValidation
	}
	return plan, report, nil
}
func noDuplicateJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 64 {
			return ErrValidation
		}
		token, e := d.Token()
		if e != nil {
			return e
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return e
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return ErrValidation
				}
				seen[name] = true
				if e := walk(depth + 1); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e := walk(depth + 1); e != nil {
					return e
				}
			}
		default:
			return ErrValidation
		}
		_, e = d.Token()
		return e
	}
	if e := walk(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrValidation
	}
	return nil
}
