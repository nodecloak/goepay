package migration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"goepay/internal/ledger"
)

// Dump234 is the private result of parsing an untrusted MySQL backup. Snapshot
// contains normalized source data and must never be returned in public views.
type Dump234 struct {
	Source   string
	Prefix   string
	SHA256   string
	Snapshot []byte `json:"-"`
	Tables   []DumpTable234
	Issues   []Issue
	Warnings []Issue
}

type DumpTable234 struct {
	Name string `json:"name"`
	Rows int    `json:"rows"`
	Mode string `json:"mode"`
}

func ParseDump234(raw []byte, source, prefix string, at time.Time) (Dump234, error) {
	h := sha256.Sum256(raw)
	d := Dump234{SHA256: hex.EncodeToString(h[:]), Tables: []DumpTable234{}, Issues: []Issue{}, Warnings: []Issue{}}
	issue := func(path, code string) {
		if len(d.Issues) < 200 {
			d.Issues = append(d.Issues, Issue{path, code})
		}
	}
	warn := func(path, code string) {
		if len(d.Warnings) < 300 {
			d.Warnings = append(d.Warnings, Issue{path, code})
		}
	}
	if source != "epay_pro" && source != "epay_release" {
		issue("source", "unsupported_source")
		return d, ErrValidation
	}
	d.Source = source
	if at.IsZero() {
		issue("exported_at", "invalid_timestamp")
		return d, ErrValidation
	}
	if !validDumpBytes234(raw) {
		issue("backup", "invalid_encoding_or_size")
		return d, ErrValidation
	}
	if prefix != "" && (!dumpIdentifier234.MatchString(prefix) || len(prefix) > 59) {
		issue("prefix", "invalid_prefix")
		return d, ErrValidation
	}
	tables, e := readDump234(bytes.TrimPrefix(raw, []byte{239, 187, 191}))
	if e != nil {
		var syntax dumpSyntax234
		if errors.As(e, &syntax) {
			issue(fmt.Sprintf("sql@%d", syntax.offset), syntax.code)
		} else {
			issue("backup", "invalid_sql")
		}
		return d, ErrValidation
	}
	families := []string{}
	for name := range tables {
		if strings.HasSuffix(name, "user") {
			p := strings.TrimSuffix(name, "user")
			if tables[p+"order"] != nil && tables[p+"type"] != nil {
				families = append(families, p)
			}
		}
	}
	selected := false
	if prefix == "" {
		if len(families) != 1 {
			issue("prefix", "ambiguous_or_missing_family")
		} else {
			prefix = families[0]
			selected = true
		}
	} else {
		for _, family := range families {
			if family == prefix {
				selected = true
				break
			}
		}
		if !selected {
			issue("prefix", "family_not_found")
		}
	}
	if selected {
		d.Prefix = prefix
	}
	keys := make([]string, 0, len(tables))
	for name := range tables {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		mode := "archive"
		if selected {
			switch name {
			case prefix + "user", prefix + "order", prefix + "refundorder", prefix + "transfer":
				mode = "native"
			}
		}
		d.Tables = append(d.Tables, DumpTable234{name, tables[name].count, mode})
		if mode == "archive" && tables[name].count > 0 {
			warn("table:"+name, "archive_only_requires_manual_configuration")
		}
	}
	if len(d.Issues) > 0 {
		return d, ErrValidation
	}
	warn("merchants", "credentials_archived_merchants_disabled")
	warn("configuration", "channels_settings_groups_plugins_not_activated")
	warn("timestamps", "mysql_datetime_assumed_utc_plus_8")
	p := Plan{SchemaVersion: 1, ExportID: "wizard234:" + d.SHA256, Source: source, ExportedAt: at.UTC().Format(time.RFC3339Nano), Merchants: []Merchant{}, Orders: []Order{}, Refunds: []Refund{}, Transfers: []Transfer{}}
	value := func(row dumpRow234, key string) string {
		v := row[key]
		if v.null {
			return ""
		}
		// Hex/binary SQL literals may decode bytes that were not present as
		// literal UTF-8 in the input. Reject them only when consumed by the
		// native mapper, before json.Marshal can replace them with U+FFFD.
		// Unconverted binary fields/tables remain in the byte-for-byte archive.
		if !utf8.ValidString(v.text) {
			issue("backup", "invalid_utf8_native_value")
			return ""
		}
		return v.text
	}
	integer := func(row dumpRow234, key, path string) int64 {
		v, e := strconv.ParseInt(value(row, key), 10, 64)
		if e != nil {
			issue(path+"."+key, "invalid_integer")
		}
		return v
	}
	money := func(row dumpRow234, key, path string, optional bool) int64 {
		s := value(row, key)
		if optional && s == "" {
			return 0
		}
		n, e := ledger.Parse(s)
		if e != nil {
			issue(path+"."+key, "invalid_money")
		}
		return n
	}
	requireColumns := func(table *dumpTableData234, fields ...string) {
		seen := map[string]bool{}
		for _, c := range table.columns {
			seen[c] = true
		}
		for _, f := range fields {
			if !seen[f] {
				issue("table:"+table.name, "missing_required_column")
			}
		}
	}
	u := tables[prefix+"user"]
	ot := tables[prefix+"order"]
	tt := tables[prefix+"type"]
	requireColumns(u, "uid", "money")
	requireColumns(tt, "id", "name")
	requireColumns(ot, "trade_no", "uid", "out_trade_no", "name", "type", "money", "status", "addtime")
	for i, row := range u.rows {
		path := fmt.Sprintf("merchants[%d]", i)
		id := integer(row, "uid", path)
		name := value(row, "codename")
		if name == "" {
			name = fmt.Sprintf("Merchant %d", id)
		}
		created := value(row, "addtime")
		if created != "" {
			stamp, err := parseTime(created)
			if err != nil || stamp.IsZero() || stamp.Year() < 1 || strings.HasSuffix(created, "-00:00") {
				issue(path+".addtime", "invalid_registration_timestamp")
			} else {
				created = stamp.Format(time.RFC3339Nano)
			}
		}
		p.Merchants = append(p.Merchants, Merchant{ID: id, Name: name, Available: ledger.Format(money(row, "money", path, false)), Reserved: "0.00", CreatedAt: created})
		if money(row, "deposit", path, true) != 0 {
			issue(path+".deposit", "unmapped_deposit_balance")
		}
	}
	types := map[int64]string{}
	for i, row := range tt.rows {
		path := fmt.Sprintf("types[%d]", i)
		id := integer(row, "id", path)
		name := value(row, "name")
		if id <= 0 || !goodString(name, 32) {
			issue(path, "invalid_payment_type")
		}
		if _, ok := types[id]; ok {
			issue(path, "duplicate_payment_type")
		}
		types[id] = name
	}
	statuses := map[string]int64{}
	orderAmounts := map[string]int64{}
	sourceRefunds := map[string]int64{}
	for i, row := range ot.rows {
		path := fmt.Sprintf("orders[%d]", i)
		id := value(row, "trade_no")
		st := integer(row, "status", path)
		status := "unknown"
		switch st {
		case 0:
			status = "created"
		case 1, 2:
			status = "paid"
		case 3:
			issue(path, "frozen_order_requires_mapping")
		case 4:
			issue(path, "preauthorization_requires_mapping")
		default:
			issue(path, "unsupported_order_status")
		}
		typeID := integer(row, "type", path)
		payType, ok := types[typeID]
		if !ok {
			issue(path+".type", "unknown_payment_type")
		}
		version := "v1"
		switch value(row, "version") {
		case "", "0":
		case "1":
			version = "v2"
		default:
			issue(path+".version", "unsupported_protocol_version")
		}
		if tid := value(row, "tid"); tid != "" && tid != "0" {
			issue(path+".tid", "special_business_scene_requires_mapping")
		}
		if combine := value(row, "combine"); combine != "" && combine != "0" {
			issue(path+".combine", "combined_order_requires_mapping")
		}
		n := money(row, "money", path, false)
		if realmoney := value(row, "realmoney"); realmoney != "" {
			if rn := money(row, "realmoney", path, false); rn != n {
				issue(path+".realmoney", "charged_amount_requires_mapping")
			}
		}
		if value(row, "getmoney") != "" {
			warn(path, "settlement_amount_archived_only")
		}
		paidAt := ""
		if status == "paid" {
			paidAt = value(row, "endtime")
		}
		p.Orders = append(p.Orders, Order{TradeNo: id, MerchantID: integer(row, "uid", path), OutTradeNo: value(row, "out_trade_no"), Name: value(row, "name"), Type: payType, Money: ledger.Format(n), Status: status, CreatedAt: value(row, "addtime"), PaidAt: paidAt, Version: version})
		statuses[id] = st
		orderAmounts[id] = n
		sourceRefunds[id] = money(row, "refundmoney", path, true)
	}
	refundTotals := map[string]int64{}
	if rt := tables[prefix+"refundorder"]; rt != nil {
		requireColumns(rt, "refund_no", "trade_no", "uid", "money", "status")
		for i, row := range rt.rows {
			path := fmt.Sprintf("refunds[%d]", i)
			if integer(row, "status", path) != 1 {
				issue(path, "unresolved_refund_requires_mapping")
				continue
			}
			n := money(row, "money", path, false)
			trade := value(row, "trade_no")
			if n > 0 && refundTotals[trade] > int64(^uint64(0)>>1)-n {
				issue(path, "money_overflow")
			} else {
				refundTotals[trade] += n
			}
			p.Refunds = append(p.Refunds, Refund{MerchantID: integer(row, "uid", path), TradeNo: trade, RefundNo: value(row, "refund_no"), Money: ledger.Format(n), Status: "succeeded"})
		}
	}
	for id, st := range statuses {
		total := refundTotals[id]
		expected := sourceRefunds[id]
		if st == 2 && !(total > 0 && (total == expected || total == orderAmounts[id] && expected == 0)) {
			issue("orders.refundmoney", "refunded_order_missing_amount_evidence")
		}
		if st == 1 && total > 0 && expected == 0 {
			issue("orders.refundmoney", "refund_history_amount_mismatch")
		}
		if expected < 0 || expected > 0 && expected != total {
			issue("orders.refundmoney", "refund_history_amount_mismatch")
		}
	}
	if tr := tables[prefix+"transfer"]; tr != nil {
		requireColumns(tr, "biz_no", "uid", "money", "status")
		for i, row := range tr.rows {
			path := fmt.Sprintf("transfers[%d]", i)
			status := ""
			switch integer(row, "status", path) {
			case 1:
				status = "succeeded"
			case 2:
				status = "failed"
			default:
				issue(path, "unresolved_transfer_requires_mapping")
				continue
			}
			n := money(row, "money", path, false)
			if value(row, "costmoney") != "" && money(row, "costmoney", path, false) != n {
				warn(path, "transfer_cost_archived_only")
			}
			p.Transfers = append(p.Transfers, Transfer{MerchantID: integer(row, "uid", path), TransferNo: value(row, "biz_no"), Money: ledger.Format(n), Status: status})
		}
	}
	if st := tables[prefix+"settle"]; st != nil {
		requireColumns(st, "status")
		for i, row := range st.rows {
			path := fmt.Sprintf("settlements[%d]", i)
			if integer(row, "status", path) != 1 {
				issue(path, "unresolved_settlement_requires_mapping")
			}
		}
	}
	for _, name := range []string{"psorder", "psinternal", "internal_transfer", "suborder"} {
		if table := tables[prefix+name]; table != nil && table.count > 0 {
			issue("table:"+table.name, "financial_history_requires_dedicated_mapping")
		}
	}
	rawPlan, e := json.Marshal(p)
	if e != nil {
		issue("backup", "normalization_failed")
		return d, ErrValidation
	}
	_, report, validation := Validate(rawPlan)
	for _, v := range report.Issues {
		issue(v.Path, v.Code)
	}
	for _, v := range report.Warnings {
		warn(v.Path, v.Code)
	}
	if validation != nil || len(d.Issues) > 0 {
		return d, ErrValidation
	}
	d.Snapshot = rawPlan
	return d, nil
}
