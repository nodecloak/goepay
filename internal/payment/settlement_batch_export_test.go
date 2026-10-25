package payment

import (
	"bytes"
	"context"
	"errors"
	"golang.org/x/text/encoding/simplifiedchinese"
	"strings"
	"testing"
)

func TestSettlementBatches84ExportUsesOriginalRecipientAndAuditsWithoutPII(t *testing.T) {
	s, _, _, request := payoutFixture(t)
	ctx := context.Background()
	b, err := s.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: "export-original84", RequestIDs: []int64{request.ID}}, batchAdmin84)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetSettlementRecipient(ctx, 501, SettlementRecipientInput{Type: "wxpay", AppID: "changed-app84", Account: "changed-openid84", Name: "Changed Name84"}, nil, SettlementRecipientActor{Role: "merchant", ID: 501, Name: "merchant"}); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []TransferBatchActor{settlementAdmin, {Role: "merchant", ID: 501, Name: "merchant"}, {Role: "system", Class: "super", ID: 1, Name: "system"}} {
		if _, err = s.ExportSettlementBatch(ctx, b.ID, "wxpay", actor); !errors.Is(err, ErrInvalid) {
			t.Fatal("sensitive export authority", err)
		}
	}
	file, err := s.ExportSettlementBatch(ctx, b.ID, "wxpay", batchAdmin84)
	if err != nil {
		t.Fatal(err)
	}
	text := string(file.Data)
	if !strings.Contains(text, "original-payout-openid") || !strings.Contains(text, "batch-test-app") || !strings.Contains(text, "9.70") || strings.Contains(text, "changed-openid84") || strings.Contains(text, "changed-app84") {
		t.Fatal("export adopted current receiver or gross")
	}
	var audit string
	if err = s.pool.QueryRow(ctx, `SELECT changes::text FROM backoffice_audit WHERE resource='settlement-batch-exports' AND resource_id=$1`, b.ID).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(audit, "openid") || strings.Contains(audit, "Original Payout") || !strings.Contains(audit, "sha256") {
		t.Fatal("export audit leaked receiver or lost file digest", audit)
	}
	if _, err = s.SettlementBatch(ctx, b.ID); err != nil {
		t.Fatal("export broke original event proof", err)
	}
	if _, err = s.pool.Exec(ctx, `CREATE FUNCTION batch_export84_audit_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='settlement-batch-exports' THEN RAISE EXCEPTION 'synthetic export audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER batch_export84_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION batch_export84_audit_failure()`); err != nil {
		t.Fatal(err)
	}
	failed, err := s.ExportSettlementBatch(ctx, b.ID, "wxpay", batchAdmin84)
	if err == nil || len(failed.Data) != 0 || failed.Filename != "" {
		t.Fatal("export returned receiver without committed audit")
	}
	legacy, items := settlementBatchFixture84(t)
	missing, err := legacy.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: "missing-receiver84", RequestIDs: []int64{items[0].ID}}, batchAdmin84)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.ExportSettlementBatch(ctx, missing.ID, "common", batchAdmin84); !errors.Is(err, ErrState) {
		t.Fatal("missing original recipient exported", err)
	}
}

func TestSettlementBatches84OriginalFourExportLayoutsAndEncodingUseFrozenNet(t *testing.T) {
	batch := SettlementBatch{Batch: "20261025000000000001", Count: 2}
	rows := []settlementBatchExportRow{{RequestID: 1, Position: 1, Type: "alipay", Account: "buyer84@example.test", Name: "测试甲", NetAmount: 4875}, {RequestID: 2, Position: 2, Type: "wxpay", Account: "original-openid84", Name: "测试乙", AppID: "wxoriginal84", NetAmount: 1950}}
	for _, format := range []string{"common", "alipay", "mybank", "wxpay"} {
		file, err := renderSettlementBatchExport(batch, rows, format, "平台结算")
		if err != nil {
			t.Fatal(format, err)
		}
		raw := file.Data
		if format == "alipay" || format == "mybank" {
			if bytes.HasPrefix(raw, []byte{239, 187, 191}) {
				t.Fatal("GBK acquired UTF8 BOM")
			}
			raw, err = simplifiedchinese.GBK.NewDecoder().Bytes(raw)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			if !bytes.HasPrefix(raw, []byte{239, 187, 191}) {
				t.Fatal("original UTF8 BOM lost")
			}
			raw = raw[3:]
		}
		text := string(raw)
		if !strings.Contains(text, "\r\n") || !strings.Contains(text, "平台结算") {
			t.Fatal("original CRLF/remark missing", format)
		}
		if format == "wxpay" {
			if !strings.Contains(text, "转账appid（必填）,wxoriginal84") || !strings.Contains(text, "转账总金额（必填，单位：元）,19.5") || strings.Contains(text, "buyer84@example.test") {
				t.Fatal("wechat snapshot/app/filter/net mismatch", text)
			}
		} else {
			if !strings.Contains(text, "48.75") || strings.Contains(text, "50.00") {
				t.Fatal("gross substituted original net", format, text)
			}
		}
		if file.Filename != "pay_"+format+"_"+batch.Batch+".csv" {
			t.Fatal("original filename", file.Filename)
		}
	}
}

func TestSettlementBatches84ExportRejectsUnknownReceiverAndCrossAppOrSpreadsheetFormula(t *testing.T) {
	batch := SettlementBatch{Batch: "20261025000000000001", Count: 1}
	good := settlementBatchExportRow{RequestID: 1, Position: 1, Type: "alipay", Account: "safe84@example.test", Name: "测试", NetAmount: 100}
	for _, row := range []settlementBatchExportRow{{RequestID: 1, Position: 1, Type: "unknown", NetAmount: 100}, {RequestID: 1, Position: 1, Type: "alipay", Account: "=HYPERLINK(1)", Name: "测试", NetAmount: 100}, {RequestID: 1, Position: 1, Type: "alipay", Account: "safe@example.test", Name: "\t=1+1", NetAmount: 100}, {RequestID: 1, Position: 1, Type: "alipay", Account: "\uFEFF=1+1", Name: "测试", NetAmount: 100}} {
		if _, err := renderSettlementBatchExport(batch, []settlementBatchExportRow{row}, "common", "平台结算"); !errors.Is(err, ErrInvalid) {
			t.Fatal("unsafe export changed or accepted receiver", err)
		}
	}
	batch.Count = 2
	good.Type = "wxpay"
	good.Account = "openid84"
	good.AppID = "wx84a"
	other := good
	other.RequestID = 2
	other.Position = 2
	other.AppID = "wx84b"
	if _, err := renderSettlementBatchExport(batch, []settlementBatchExportRow{good, other}, "wxpay", "平台结算"); !errors.Is(err, ErrConflict) {
		t.Fatal("cross-app OpenIDs exported under one app", err)
	}
}
