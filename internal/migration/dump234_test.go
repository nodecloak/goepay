package migration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

const dumpFixture234 = "-- synthetic backup; never source data\n" +
	"CREATE TABLE `pre_user` (`uid` int, `money` decimal(10,2), `codename` varchar(40), `addtime` datetime, `deposit` decimal(10,2));\n" +
	"CREATE TABLE `pre_type` (`id` int,`name` varchar(32));\n" +
	"CREATE TABLE `pre_order` (`trade_no` varchar(30),`uid` int,`out_trade_no` varchar(50),`name` varchar(64),`type` int,`money` decimal(10,2),`status` int,`addtime` datetime,`endtime` datetime,`version` int,`tid` int,`combine` int,`refundmoney` decimal(10,2));\n" +
	"INSERT INTO `pre_user` VALUES (9001,12.34,'Synthetic merchant','2025-01-01 10:00:00',NULL);\n" +
	"INSERT INTO `pre_type` VALUES(1,'alipay'),(2,'wxpay');\n" +
	"INSERT INTO `pre_order` VALUES('0000123',9001,'business-001','test; with \\' quote',1,2.50,1,'2025-01-02 10:00:00','2025-01-02 10:01:00',1,0,0,NULL);\n"

func parseFixture234(t *testing.T, raw string) Dump234 {
	t.Helper()
	d, e := ParseDump234([]byte(raw), "epay_pro", "", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if e != nil {
		t.Fatalf("parse failed: %v issues=%v", e, d.Issues)
	}
	return d
}
func decodePlan234(t *testing.T, d Dump234) Plan {
	t.Helper()
	var p Plan
	if e := json.Unmarshal(d.Snapshot, &p); e != nil {
		t.Fatal(e)
	}
	if _, r, e := Validate(d.Snapshot); e != nil {
		t.Fatalf("invalid normalized snapshot: %v", r.Issues)
	}
	return p
}
func TestDump234MapsCoreWithoutExecutingSQL(t *testing.T) {
	d := parseFixture234(t, dumpFixture234)
	p := decodePlan234(t, d)
	if d.Prefix != "pre_" || d.Source != "epay_pro" || len(d.SHA256) != 64 || len(d.Tables) != 3 {
		t.Fatal("metadata", d)
	}
	if len(p.Merchants) != 1 || p.Merchants[0].ID != 9001 || p.Merchants[0].Available != "12.34" || p.Merchants[0].Reserved != "0.00" {
		t.Fatal("merchant mapping", p.Merchants)
	}
	if len(p.Orders) != 1 || p.Orders[0].TradeNo != "0000123" || p.Orders[0].Type != "alipay" || p.Orders[0].Version != "v2" || p.Orders[0].Status != "paid" || p.Orders[0].Name != "test; with ' quote" {
		t.Fatal("order mapping", p.Orders)
	}
	if !strings.HasPrefix(p.ExportID, "wizard234:") || len(d.Warnings) == 0 {
		t.Fatal("missing provenance/activation warning")
	}
}
func TestDump234ExplicitColumnsPrefixesCommentsAndArchive(t *testing.T) {
	raw := strings.ReplaceAll(dumpFixture234, "pre_", "shop42_") + "CREATE TABLE shop42_custom (secret text); INSERT INTO shop42_custom (secret) VALUES ('not-for-views');"
	raw = "/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */; SET NAMES utf8mb4;\n" + raw + "/*!40000 ALTER TABLE `shop42_user` ENABLE KEYS */;"
	raw = strings.Replace(raw, "INSERT INTO `shop42_user` VALUES (9001,12.34,'Synthetic merchant','2025-01-01 10:00:00',NULL)", "INSERT INTO `shop42_user` (`money`,`uid`,`deposit`,`codename`,`addtime`) VALUES ('12.34',9001,0,'Synthetic merchant','2025-01-01 10:00:00')", 1)
	d := parseFixture234(t, raw)
	p := decodePlan234(t, d)
	if d.Prefix != "shop42_" || p.Merchants[0].Available != "12.34" {
		t.Fatal("prefix/columns")
	}
	found := false
	for _, tb := range d.Tables {
		if tb.Name == "shop42_custom" {
			found = tb.Rows == 1 && tb.Mode == "archive"
		}
	}
	if !found {
		t.Fatal("archive inventory")
	}
	if bytes.Contains(mustJSON234(t, d.Tables), []byte("not-for-views")) {
		t.Fatal("source row leaked")
	}
}
func mustJSON234(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestDump234ReleaseEmptyOrdersAndRegistrationUnknown(t *testing.T) {
	raw := "CREATE TABLE x_user(uid int,money decimal(10,2));CREATE TABLE x_type(id int,name text);CREATE TABLE x_order(trade_no text,uid int,out_trade_no text,name text,type int,money text,status int,addtime text);INSERT INTO x_user VALUES(1000,0);"
	d, e := ParseDump234([]byte(raw), "epay_release", "x_", time.Now().UTC())
	if e != nil {
		t.Fatal(e, d.Issues)
	}
	p := decodePlan234(t, d)
	if p.Merchants[0].CreatedAt != "" || p.Merchants[0].Name != "Merchant 1000" || len(p.Orders) != 0 {
		t.Fatal("invented registration or empty history")
	}
}
func TestDump234RejectsSyntaxAndAmbiguity(t *testing.T) {
	cases := map[string]string{
		"truncated":         strings.TrimSuffix(strings.TrimSpace(dumpFixture234), ";"),
		"expression":        strings.Replace(dumpFixture234, "9001,12.34", "9001,(SELECT 1)", 1),
		"update":            dumpFixture234 + "UPDATE pre_user SET money=99;",
		"delete":            dumpFixture234 + "DELETE FROM pre_order;",
		"replace":           dumpFixture234 + "REPLACE INTO pre_user VALUES(9001,99,'a',NULL,NULL);",
		"duplicate_columns": dumpFixture234 + "INSERT INTO pre_user(uid,uid) VALUES(1,1);",
		"duplicate_create":  dumpFixture234 + "CREATE TABLE pre_user(uid int);",
		"versioned_dml":     dumpFixture234 + "/*!50000 INSERT INTO pre_user(uid,money) VALUES(44,9) */;",
		"sql_mode":          "SET SQL_MODE='NO_BACKSLASH_ESCAPES';" + dumpFixture234,
		"second_family":     dumpFixture234 + strings.ReplaceAll(dumpFixture234, "pre_", "other_"),
		"missing_family":    "CREATE TABLE pre_user(uid int);",
		"qualified":         "INSERT INTO otherdb.pre_user(uid,money) VALUES(1,1);",
		"wrong_encoding":    dumpFixture234 + string([]byte{255}),
		"rollback":          dumpFixture234 + "ROLLBACK;",
		"schema_alter":      dumpFixture234 + "ALTER TABLE pre_user ADD money2 int;",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			d, e := ParseDump234([]byte(raw), "epay_pro", "", time.Now().UTC())
			if e == nil || len(d.Issues) == 0 {
				t.Fatal("unsafe dump accepted")
			}
			if strings.Contains(e.Error(), "SELECT") || strings.Contains(e.Error(), "Synthetic") {
				t.Fatal("raw source error")
			}
		})
	}
}
func TestDump234BlocksUnsupportedFinancialSemantics(t *testing.T) {
	cases := map[string]string{
		"freeze":             strings.Replace(dumpFixture234, "1,'2025-01-02 10:00:00'", "3,'2025-01-02 10:00:00'", 1),
		"authorization":      strings.Replace(dumpFixture234, "1,'2025-01-02 10:00:00'", "4,'2025-01-02 10:00:00'", 1),
		"unknown_status":     strings.Replace(dumpFixture234, "1,'2025-01-02 10:00:00'", "9,'2025-01-02 10:00:00'", 1),
		"unknown_type":       strings.Replace(dumpFixture234, "quote',1,2.50", "quote',99,2.50", 1),
		"special_scene":      strings.Replace(dumpFixture234, ",1,0,0,NULL)", ",1,2,0,NULL)", 1),
		"combine":            strings.Replace(dumpFixture234, ",1,0,0,NULL)", ",1,0,1,NULL)", 1),
		"deposit":            strings.Replace(dumpFixture234, "10:00:00',NULL)", "10:00:00',2.00)", 1),
		"pending_settlement": dumpFixture234 + "CREATE TABLE pre_settle(id int,status int);INSERT INTO pre_settle VALUES(1,0);",
		"pending_transfer":   dumpFixture234 + "CREATE TABLE pre_transfer(biz_no text,uid int,money text,status int,costmoney text);INSERT INTO pre_transfer VALUES('t1',9001,'1.00',3,'1.10');",
		"pending_refund":     dumpFixture234 + "CREATE TABLE pre_refundorder(refund_no text,trade_no text,uid int,money text,status int);INSERT INTO pre_refundorder VALUES('r1','0000123',9001,'1.00',0);",
		"sharing":            dumpFixture234 + "CREATE TABLE pre_psorder(id int,status int);INSERT INTO pre_psorder VALUES(1,1);",
		"refunded_ambiguous": strings.Replace(dumpFixture234, "1,'2025-01-02 10:00:00'", "2,'2025-01-02 10:00:00'", 1),
		"duplicate_merchant": dumpFixture234 + "INSERT INTO pre_user(uid,money) VALUES(9001,9);",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			d, e := ParseDump234([]byte(raw), "epay_pro", "", time.Now().UTC())
			if e == nil || len(d.Issues) == 0 {
				t.Fatal("unsafe financial semantics accepted", name)
			}
		})
	}
}
func TestDump234ImportsOnlyExplicitCompletedRefundTransferHistory(t *testing.T) {
	raw := strings.Replace(dumpFixture234, "1,'2025-01-02 10:00:00'", "2,'2025-01-02 10:00:00'", 1) + "CREATE TABLE pre_refundorder(refund_no text,trade_no text,uid int,money text,status int);INSERT INTO pre_refundorder VALUES('r1','0000123',9001,'2.50',1);CREATE TABLE pre_transfer(biz_no text,uid int,money text,status int,costmoney text);INSERT INTO pre_transfer VALUES('t1',9001,'1.00',1,'1.10');CREATE TABLE pre_settle(id int,status int);INSERT INTO pre_settle VALUES(2,1);"
	d := parseFixture234(t, raw)
	p := decodePlan234(t, d)
	if p.Orders[0].Status != "paid" || len(p.Refunds) != 1 || p.Refunds[0].Status != "succeeded" || len(p.Transfers) != 1 || p.Transfers[0].Status != "succeeded" {
		t.Fatal("history mapping", p)
	}
}
func TestDump234BoundsAndSource(t *testing.T) {
	for _, source := range []string{"", "mysql", "epay_pro;DROP"} {
		if d, e := ParseDump234([]byte(dumpFixture234), source, "", time.Now()); e == nil || len(d.Issues) == 0 {
			t.Fatal("source accepted")
		}
	}
	if d, e := ParseDump234(bytes.Repeat([]byte{'a'}, 64<<20+1), "epay_pro", "", time.Now()); e == nil || len(d.Issues) == 0 {
		t.Fatal("size cap")
	}
	if d, e := ParseDump234([]byte(dumpFixture234), "epay_pro", "wrong_", time.Now()); e == nil || len(d.Issues) == 0 {
		t.Fatal("wrong explicit family")
	}
}

func TestDump234RejectsDerivedAndMalformedSchemaData(t *testing.T) {
	for _, suffix := range []string{
		"CREATE TABLE pre_custom(x int) AS SELECT 99;",
		"CREATE TABLE pre_custom(x int,);",
		"CREATE TABLE pre_custom(x int) INSERT INTO pre_user VALUES(1,1);",
		"LOCK TABLES pre_user WRITE DELETE FROM pre_user;",
	} {
		d, e := ParseDump234([]byte(dumpFixture234+suffix), "epay_pro", "", time.Now().UTC())
		if e == nil || len(d.Issues) == 0 || d.Snapshot != nil {
			t.Fatal("derived or malformed data accepted", suffix)
		}
	}
}
func TestDump234ArchiveLiteralFormsAreData(t *testing.T) {
	raw := dumpFixture234 + "CREATE TABLE pre_custom(a text,b text,c text,d text,e text,f text);INSERT INTO pre_custom VALUES(-1.25,+0.50,1e-05,0x4142,X'4142',_binary 'x\\0y');"
	d := parseFixture234(t, raw)
	if len(d.Tables) != 4 || d.Tables[3].Name != "pre_user" {
		t.Fatal("inventory")
	}
	for _, tb := range d.Tables {
		if tb.Name == "pre_custom" && tb.Rows != 1 {
			t.Fatal("archive rows")
		}
	}
}
func TestDump234BlocksNegativeTransferCost(t *testing.T) {
	raw := dumpFixture234 + "CREATE TABLE pre_transfer(biz_no text,uid int,money text,status int,costmoney text);INSERT INTO pre_transfer VALUES('t1',9001,'1.00',1,'-1.10');"
	d, e := ParseDump234([]byte(raw), "epay_pro", "", time.Now().UTC())
	if e == nil || len(d.Issues) == 0 || d.Snapshot != nil {
		t.Fatal("negative cost accepted")
	}
}

func TestDump234RejectsEncodedQuoteModes(t *testing.T) {
	for _, literal := range []string{"'NO_\\BACKSLASH_ESCAPES'", "'ANSI_\\QUOTES'", "X'4e4f5f4241434b534c4153485f45534341504553'", "0x414e53495f51554f544553"} {
		d, e := ParseDump234([]byte("SET SQL_MODE="+literal+";"+dumpFixture234), "epay_pro", "", time.Now().UTC())
		if e == nil || len(d.Issues) == 0 {
			t.Fatal("encoded interpretation-changing mode accepted")
		}
	}
}

func TestDump234SerializationNeverExposesNormalizedRows(t *testing.T) {
	d := parseFixture234(t, dumpFixture234)
	raw, e := json.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(raw, []byte("Snapshot")) || bytes.Contains(raw, []byte("snapshot")) || bytes.Contains(raw, []byte("Synthetic merchant")) || bytes.Contains(raw, []byte("business-001")) {
		t.Fatal("private normalized source is serializable in a view")
	}
}
func TestDump234RegistrationNeedsKnownOriginalTime(t *testing.T) {
	for _, stamp := range []string{"2025-01-01T10:00:00-00:00", "0000-00-00 00:00:00"} {
		raw := strings.Replace(dumpFixture234, "2025-01-01 10:00:00", stamp, 1)
		if d, e := ParseDump234([]byte(raw), "epay_pro", "", time.Now().UTC()); e == nil || len(d.Issues) == 0 {
			t.Fatal("unknown/invalid original time became known")
		}
	}
	d := parseFixture234(t, dumpFixture234)
	p := decodePlan234(t, d)
	if p.Merchants[0].CreatedAt != "2025-01-01T10:00:00+08:00" {
		t.Fatal("lost source timezone", p.Merchants[0].CreatedAt)
	}
}

func TestDump234PaidOrderRefundHistoryRequiresMatchingCounter(t *testing.T) {
	raw := dumpFixture234 + "CREATE TABLE pre_refundorder(refund_no text,trade_no text,uid int,money text,status int);INSERT INTO pre_refundorder VALUES('r1','0000123',9001,'1.00',1);"
	d, e := ParseDump234([]byte(raw), "epay_pro", "", time.Now().UTC())
	if e == nil || len(d.Issues) == 0 || d.Snapshot != nil {
		t.Fatal("interrupted source refund accounting accepted")
	}
	raw = strings.Replace(raw, ",1,0,0,NULL)", ",1,0,0,1.00)", 1)
	d = parseFixture234(t, raw)
	p := decodePlan234(t, d)
	if p.Orders[0].Status != "paid" || len(p.Refunds) != 1 || p.Refunds[0].Money != "1.00" {
		t.Fatal("explicit matching partial refund lost")
	}
}

func TestDump234ExplicitPrefixSelectsOneCompleteFamily(t *testing.T) {
	raw := dumpFixture234 + strings.ReplaceAll(dumpFixture234, "pre_", "second_")
	d, e := ParseDump234([]byte(raw), "epay_pro", "second_", time.Now().UTC())
	if e != nil {
		t.Fatal("explicit family rejected", d.Issues)
	}
	p := decodePlan234(t, d)
	if len(p.Merchants) != 1 || len(p.Orders) != 1 || d.Prefix != "second_" {
		t.Fatal("merged families")
	}
	for _, tb := range d.Tables {
		if strings.HasPrefix(tb.Name, "pre_") && tb.Mode != "archive" {
			t.Fatal("unselected family activated")
		}
	}
}
func TestDump234UniqueUnprefixedFamilyIsValid(t *testing.T) {
	d := parseFixture234(t, strings.ReplaceAll(dumpFixture234, "pre_", ""))
	p := decodePlan234(t, d)
	if d.Prefix != "" || len(p.Merchants) != 1 {
		t.Fatal("unprefixed family")
	}
	for _, tb := range d.Tables {
		if (tb.Name == "user" || tb.Name == "order") && tb.Mode != "native" {
			t.Fatal("unprefixed core not mapped")
		}
	}
}
func TestDump234BoundsTotalLiteralCells(t *testing.T) {
	var b strings.Builder
	b.WriteString(dumpFixture234)
	b.WriteString("CREATE TABLE pre_archive_budget(")
	for c := 0; c < 512; c++ {
		if c > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "c%d int", c)
	}
	b.WriteString(");INSERT INTO pre_archive_budget VALUES")
	// About 4 MiB of literals, with no native-row maps: the cell budget must
	// reject before a small file could cause unbounded per-cell allocations.
	row := "(" + strings.Repeat("0,", 511) + "0)"
	for n := 0; n < 3907; n++ {
		if n > 0 {
			b.WriteByte(',')
		}
		b.WriteString(row)
	}
	b.WriteByte(';')
	d, e := ParseDump234([]byte(b.String()), "epay_pro", "", time.Now().UTC())
	if e == nil || d.Snapshot != nil {
		t.Fatal("unbounded literal cells")
	}
	found := false
	for _, v := range d.Issues {
		if v.Code == "too_many_cells" {
			found = true
		}
	}
	if !found {
		t.Fatal("cell budget not enforced", d.Issues)
	}
}

func TestDump234NativeHexLiteralsCannotLoseInvalidUTF8(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(dumpFixture234, "'Synthetic merchant'", "0xff", 1),
		strings.Replace(dumpFixture234, "'0000123'", "X'ff'", 1),
		strings.Replace(dumpFixture234, "'business-001'", "_binary '\\xff'", 1),
	} {
		d, e := ParseDump234([]byte(raw), "epay_pro", "", time.Now().UTC())
		// The last SQL string is valid ASCII after MySQL's ordinary escaped-x
		// rule; it must not be confused with a hex introducer.
		if strings.Contains(raw, "_binary") {
			if e != nil {
				t.Fatal("valid escaped ASCII literal rejected", d.Issues)
			}
			continue
		}
		if e == nil || len(d.Issues) == 0 || d.Snapshot != nil {
			t.Fatal("native binary value silently replaced by JSON encoder")
		}
	}
}
func TestDump234ArchiveOnlyBinaryRemainsPreserved(t *testing.T) {
	raw := dumpFixture234 + "CREATE TABLE pre_custom(blob_value blob);INSERT INTO pre_custom VALUES(0xff);"
	parseFixture234(t, raw)
	other := strings.ReplaceAll(strings.Replace(dumpFixture234, "'Synthetic merchant'", "X'ff'", 1), "pre_", "other_")
	d, e := ParseDump234([]byte(dumpFixture234+other), "epay_pro", "pre_", time.Now().UTC())
	if e != nil {
		t.Fatal("unselected archived family binary value rejected", d.Issues)
	}
	decodePlan234(t, d)
}
