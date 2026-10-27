package migration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// The parent nominal amount is deliberately 5.00 while the original charged
// amount and the three suborders total 5.25. Historical assertions must not be
// converted into a newly authenticated customer-charge104 quote.
const combinedSnapshot204 = `{"schema_version":1,"export_id":"sanitized-combined204","source":"epay_pro","exported_at":"2026-10-23T12:00:00+08:00","merchants":[{"id":9001,"name":"Imported combined204","available_money":"12.34","reserved_money":"0.00"}],"orders":[{"trade_no":"204-parent","merchant_id":9001,"out_trade_no":"204-external","name":"Historical combined parent","type":"wxpay","money":"5.00","status":"paid","created_at":"2026-10-20T12:00:00+08:00","paid_at":"2026-10-20T12:01:00+08:00"}],"refunds":[],"transfers":[],"combined_orders":[{"trade_no":"204-parent","merchant_id":9001,"source_plugin":"wxpayn","currency":"CNY","charged_money":"5.25","settle":2,"sub_orders":[{"sub_trade_no":"204-parent1","api_trade_no":"WX204-1","money":"1.75","refundmoney":null,"status":2,"settle":1},{"sub_trade_no":"204-parent2","api_trade_no":"WX204-2","money":"1.75","refundmoney":"0.25","status":2,"settle":0},{"sub_trade_no":"204-parent3","api_trade_no":"WX204-3","money":"1.75","refundmoney":"0.00","status":1,"settle":0}]}]}`

func combinedData204(t *testing.T) map[string]any {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal([]byte(combinedSnapshot204), &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func combinedGroup204(data map[string]any) map[string]any {
	return data["combined_orders"].([]any)[0].(map[string]any)
}

func combinedChild204(data map[string]any, index int) map[string]any {
	return combinedGroup204(data)["sub_orders"].([]any)[index].(map[string]any)
}

func encodeCombined204(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCombinedOrders204ValidateRetainsOriginalGraphAndExactRawDigest(t *testing.T) {
	plan, report, err := Validate([]byte(combinedSnapshot204))
	if err != nil || !report.Valid {
		t.Fatalf("explicit historical combined_orders graph must be accepted by the schema1 importer: issues=%+v err=%v", report.Issues, err)
	}
	digest := sha256.Sum256([]byte(combinedSnapshot204))
	if report.Digest != hex.EncodeToString(digest[:]) || report.Orders != 1 || len(plan.Orders) != 1 || plan.Orders[0].Money != "5.00" {
		t.Fatal("combined children changed the raw digest, parent count or nominal amount", report)
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var retained map[string]any
	if err = json.Unmarshal(encoded, &retained); err != nil {
		t.Fatal(err)
	}
	groups, ok := retained["combined_orders"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatal("validated combined graph disappeared before import")
	}
	group := groups[0].(map[string]any)
	if group["trade_no"] != "204-parent" || group["charged_money"] != "5.25" || group["source_plugin"] != "wxpayn" || group["settle"] != float64(2) {
		t.Fatal("validated combined parent lost its explicit historical assertions", group)
	}
	children, ok := group["sub_orders"].([]any)
	if !ok || len(children) != 3 {
		t.Fatal("validated combined parent lost original suborders")
	}
	for i, refund := range []any{nil, "0.25", "0.00"} {
		child := children[i].(map[string]any)
		value, present := child["refundmoney"]
		if !present || value != refund || child["sub_trade_no"] != fmt.Sprintf("204-parent%d", i+1) || child["money"] != "1.75" {
			t.Fatal("refund NULL/zero distinction or original child amount/identity was lost", i, child)
		}
	}
}

func TestCombinedOrders204ApplyCreatesOnlyParentOrdersAndOpeningBalances(t *testing.T) {
	p := migrationPool(t)
	master := bytes.Repeat([]byte{3}, 32)
	report, err := Apply(t.Context(), p, master, []byte(combinedSnapshot204))
	if err != nil || !report.Applied {
		t.Fatalf("historical combined graph could not be atomically imported: issues=%+v err=%v", report.Issues, err)
	}
	var orders, journals, effects int
	var available, reserved, amount, merchantAmount, fee int64
	var actualCharge *int64
	var chargeRequired bool
	if err = p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM journals),(SELECT count(*) FROM provider_attempts)+(SELECT count(*) FROM provider_operations)+(SELECT count(*) FROM refunds)+(SELECT count(*) FROM jobs),(SELECT balance FROM accounts WHERE owner_id=9001 AND kind='available'),(SELECT balance FROM accounts WHERE owner_id=9001 AND kind='reserved'),amount,merchant_amount,platform_fee,actual_charge_amount104,actual_charge_required104 FROM orders WHERE trade_no='204-parent'`).Scan(&orders, &journals, &effects, &available, &reserved, &amount, &merchantAmount, &fee, &actualCharge, &chargeRequired); err != nil {
		t.Fatal(err)
	}
	if orders != 1 || journals != 1 || effects != 0 || available != 1234 || reserved != 0 || amount != 500 || merchantAmount != 500 || fee != 0 || actualCharge != nil || chargeRequired {
		t.Fatal("historical children became ordinary orders, money movements, provider evidence or queued work", orders, journals, effects, available, reserved, amount, merchantAmount, fee, actualCharge, chargeRequired)
	}
	check, err := CheckRefundHistory(t.Context(), p, master)
	if err != nil || check.InvalidLegacyRefundHistory != 0 || check.UnknownLegacyRefunds != 0 {
		t.Fatal("combined import changed the existing refund history proof contract", check, err)
	}
	replayed, err := Apply(t.Context(), p, master, []byte(combinedSnapshot204))
	if err != nil || !replayed.AlreadyImported || replayed.Applied {
		t.Fatal("identical combined source was not an idempotent read", replayed, err)
	}
	changed := strings.Replace(combinedSnapshot204, `"charged_money":"5.25"`, `"charged_money":"5.26"`, 1)
	changed = strings.Replace(changed, `"money":"1.75"`, `"money":"1.76"`, 1)
	if _, _, err = Validate([]byte(changed)); err != nil {
		t.Fatal("changed graph fixture must remain valid before export identity is checked", err)
	}
	if _, err = Apply(t.Context(), p, master, []byte(changed)); !errors.Is(err, ErrConflict) {
		t.Fatal("same sealed export accepted a replacement child graph", err)
	}
}

func TestCombinedOrders204ValidationRejectsCrossOwnerMalformedAndUnbalancedGraphs(t *testing.T) {
	if _, _, err := Validate([]byte(combinedSnapshot204)); err != nil {
		t.Fatal("valid combined graph support is required before boundary cases", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing parent", func(d map[string]any) { combinedGroup204(d)["trade_no"] = "missing204" }},
		{"foreign owner", func(d map[string]any) { combinedGroup204(d)["merchant_id"] = 9002 }},
		{"duplicate parent", func(d map[string]any) {
			d["combined_orders"] = append(d["combined_orders"].([]any), combinedGroup204(d))
		}},
		{"duplicate child", func(d map[string]any) { combinedChild204(d, 1)["sub_trade_no"] = "204-parent1" }},
		{"self parent", func(d map[string]any) { combinedChild204(d, 0)["sub_trade_no"] = "204-parent" }},
		{"sum mismatch", func(d map[string]any) { combinedChild204(d, 0)["money"] = "1.74" }},
		{"zero child", func(d map[string]any) { combinedChild204(d, 0)["money"] = "0.00" }},
		{"fractional fen", func(d map[string]any) { combinedChild204(d, 0)["money"] = "1.751" }},
		{"over refund", func(d map[string]any) { combinedChild204(d, 1)["refundmoney"] = "1.76" }},
		{"negative refund", func(d map[string]any) { combinedChild204(d, 1)["refundmoney"] = "-0.01" }},
		{"unknown child status", func(d map[string]any) { combinedChild204(d, 0)["status"] = 5 }},
		{"unknown child settlement", func(d map[string]any) { combinedChild204(d, 0)["settle"] = 2 }},
		{"wrong currency", func(d map[string]any) { combinedGroup204(d)["currency"] = "USD" }},
		{"empty children", func(d map[string]any) { combinedGroup204(d)["sub_orders"] = []any{} }},
		{"oversized suborder id", func(d map[string]any) { combinedChild204(d, 0)["sub_trade_no"] = strings.Repeat("s", 26) }},
		{"oversized upstream id", func(d map[string]any) { combinedChild204(d, 0)["api_trade_no"] = strings.Repeat("a", 151) }},
		{"unaccepted raw authority", func(d map[string]any) { combinedGroup204(d)["verified_payment_source"] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := combinedData204(t)
			tc.mutate(d)
			_, report, err := Validate(encodeCombined204(t, d))
			if err == nil || report.Valid || len(report.Issues) == 0 {
				t.Fatal("unsafe historical graph accepted", report, err)
			}
		})
	}
}

func TestCombinedOrders204HistoryUsesBoundedCountsWithoutInventedOfficialMinimum(t *testing.T) {
	for _, tc := range []struct {
		plugin string
		count  int
		valid  bool
	}{{"wxpayn", 1, true}, {"", 1, true}, {"unknown", 50, true}, {"unknown", 51, false}, {"alipayd", 6, true}, {"alipayd", 7, false}} {
		t.Run(fmt.Sprintf("%s-%d", tc.plugin, tc.count), func(t *testing.T) {
			d := combinedData204(t)
			group := combinedGroup204(d)
			if tc.plugin == "" {
				delete(group, "source_plugin")
			} else {
				group["source_plugin"] = tc.plugin
			}
			if tc.plugin == "alipayd" {
				d["orders"].([]any)[0].(map[string]any)["type"] = "alipay"
			}
			d["orders"].([]any)[0].(map[string]any)["money"] = fmt.Sprintf("%d.00", tc.count)
			children := make([]any, tc.count)
			for i := range children {
				children[i] = map[string]any{"sub_trade_no": fmt.Sprintf("204-count-%d", i+1), "api_trade_no": "", "money": "1.00", "refundmoney": nil, "status": 0, "settle": 0}
			}
			group["sub_orders"], group["charged_money"] = children, fmt.Sprintf("%d.00", tc.count)
			_, report, err := Validate(encodeCombined204(t, d))
			if tc.valid && (err != nil || !report.Valid) {
				t.Fatal("valid bounded history was rejected by an invented minimum/plugin assumption", report.Issues, err)
			}
			if !tc.valid && (err == nil || report.Valid) {
				t.Fatal("unbounded or locally disproven history was accepted", tc.count, err)
			}
		})
	}
}

func TestCombinedOrders204DuplicateJSONAndLateImportFailureDoNotPartiallyCommit(t *testing.T) {
	duplicate := strings.Replace(combinedSnapshot204, `"charged_money":"5.25"`, `"charged_money":"5.25","charged_money":"0.01"`, 1)
	if _, report, err := Validate([]byte(duplicate)); err == nil || len(report.Issues) == 0 {
		t.Fatal("ambiguous combined money accepted", report, err)
	}
	p := migrationPool(t)
	if _, _, err := Validate([]byte(combinedSnapshot204)); err != nil {
		t.Fatal("combined graph support required before exercising transactional rollback", err)
	}
	if _, err := p.Exec(t.Context(), `CREATE FUNCTION reject_combined_import204() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic final receipt failure204'; END $$; CREATE TRIGGER reject_combined_import204 BEFORE INSERT ON legacy_import_runs FOR EACH ROW EXECUTE FUNCTION reject_combined_import204()`); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(t.Context(), p, bytes.Repeat([]byte{3}, 32), []byte(combinedSnapshot204)); err == nil {
		t.Fatal("final import receipt failure was ignored")
	}
	var effects int
	if err := p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM merchants)+(SELECT count(*) FROM orders)+(SELECT count(*) FROM journals)+(SELECT count(*) FROM legacy_import_runs)+(SELECT count(*) FROM legacy_refund_history)+(SELECT count(*) FROM jobs)`).Scan(&effects); err != nil || effects != 0 {
		t.Fatal("combined import left partial historical or monetary state", effects, err)
	}
}

func TestCombinedOrders204OldSchemaOneSnapshotRetainsDigestAndRefundProofBytes(t *testing.T) {
	plan, report, err := Validate([]byte(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(snapshot))
	if report.Digest != hex.EncodeToString(digest[:]) {
		t.Fatal("legacy raw hash changed")
	}
	roundTrip, err := json.Marshal(plan)
	if err != nil || bytes.Contains(roundTrip, []byte(`"combined_orders"`)) {
		t.Fatal("omitted historical relationship was fabricated", err)
	}
	p := migrationPool(t)
	key := bytes.Repeat([]byte{3}, 32)
	if _, err = Apply(t.Context(), p, key, []byte(snapshot)); err != nil {
		t.Fatal(err)
	}
	readProofs := func() string {
		var proof string
		if err := p.QueryRow(t.Context(), `SELECT (SELECT encode(proof_hash,'hex') FROM legacy_refund_history WHERE refund_no='legacy-r1')||':'||(SELECT encode(proof_hash,'hex') FROM legacy_refund_manifests WHERE export_id='sanitized-fixture-1')||':'||(SELECT report::text FROM legacy_import_runs WHERE export_id='sanitized-fixture-1')`).Scan(&proof); err != nil {
			t.Fatal(err)
		}
		return proof
	}
	before := readProofs()
	if replay, err := Apply(t.Context(), p, key, []byte(snapshot)); err != nil || !replay.AlreadyImported {
		t.Fatal("legacy replay failed", replay, err)
	}
	if readProofs() != before {
		t.Fatal("legacy replay rewrote existing refund MACs or sealed import receipt")
	}
}
