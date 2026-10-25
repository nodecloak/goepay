package payment

import (
	"bytes"
	"context"
	adapter "goepay/internal/adapters/payment"
	"testing"
	"time"
)

func TestTransferReportingFactsUseOriginalTypeAndImmutableResolutionDate(t *testing.T) {
	s, flow, provider := transferFixture(t)
	ctx := context.Background()
	paid, err := flow.Transfer(ctx, 501, 7, "report-success", adapter.TransferRequest{Type: "wxpay", Account: "private-report-account", Amount: 100, Remark: "本地统计"})
	if err != nil || paid.Status != "pending" {
		t.Fatalf("local transfer fixture %v", err)
	}
	paid, err = flow.QueryTransfer(ctx, 501, "report-success")
	if err != nil || paid.Status != "succeeded" {
		t.Fatalf("local query fixture %v", err)
	}
	provider.unknown = true
	_, _ = flow.Transfer(ctx, 501, 7, "report-pending", adapter.TransferRequest{Type: "wxpay", Account: "private-report-account", Amount: 100, Remark: "本地统计"})
	pending, err := s.Transfer(ctx, 501, "report-pending")
	if err != nil || pending.Status != "unknown" {
		t.Fatalf("unknown fixture %v", err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE transfers SET updated_at=now()+interval '3 days' WHERE id=$1`, paid.ID); err != nil {
		t.Fatal(err)
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Now().In(zone)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	facts, err := s.TransferReportingFacts(ctx, day, day.AddDate(0, 0, 1), 50001)
	if err != nil || len(facts) != 1 || facts[0].Amount != 100 || facts[0].Type != "wxpay" || facts[0].MerchantID != 501 || facts[0].ChannelID != 7 {
		t.Fatalf("wrong reporting facts %#v %v", facts, err)
	}
	bad, err := NewWithKey(s.pool, bytes.Repeat([]byte{0xA7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bad.TransferReportingFacts(ctx, day, day.AddDate(0, 0, 1), 50001); err == nil {
		t.Fatal("invalid encrypted intent silently became an unknown payment type")
	}
	if _, err = s.TransferReportingFacts(ctx, day, day.AddDate(0, 0, 1), 50002); err == nil {
		t.Fatal("unbounded source query accepted")
	}
}
