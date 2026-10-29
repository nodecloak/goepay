package payment

import (
	"context"
	"testing"

	provider "goepay/internal/adapters/payment"
)

// The console list/detail/summary read complaints133 (the provider-synced
// store), scoped to a merchant owner, with the Pro filters and offset paging.
// These seed rows directly and assert scope, each filter, paging+total, the
// detail images/history extraction and the today/yesterday summary + ranking.

func seedComplaint(t *testing.T, s *Store, channel int64, owner *int64, rec provider.ComplaintRecord) {
	t.Helper()
	if e := s.upsertComplaint133(context.Background(), channel, rec); e != nil {
		t.Fatal("seed:", e)
	}
	if owner != nil {
		if _, e := s.pool.Exec(context.Background(), `UPDATE complaints133 SET owner_id=$2 WHERE pay_type=$1 AND third_id=$3`, rec.PayType, *owner, rec.ThirdID); e != nil {
			t.Fatal("seed owner:", e)
		}
	}
}

func TestComplaintListScopeFiltersPagingAndSummary(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	// Two merchants so scope is testable.
	if e := s.CreateMerchant(ctx, Merchant{ID: 80001, Name: "complain-owner-1", APIKey: "complain-key-0001"}); e != nil {
		t.Fatal(e)
	}
	if e := s.CreateMerchant(ctx, Merchant{ID: 80002, Name: "complain-owner-2", APIKey: "complain-key-0002"}); e != nil {
		t.Fatal(e)
	}
	o1, o2 := int64(80001), int64(80002)
	a1, a2 := int64(1500), int64(2500)
	seedComplaint(t, s, 9100, &o1, provider.ComplaintRecord{ThirdID: "WX-L1", PayType: "wxpay", Status: "PENDING", Content: "未发货", Phone: "13800000001", AmountCents: &a1, Times: 1, TradeNos: []string{"ORD-A"}, ThirdMchID: "MCH-W"})
	seedComplaint(t, s, 9100, &o1, provider.ComplaintRecord{ThirdID: "AL-L2", PayType: "alipay", Status: "MERCHANT_PROCESSING", Title: "商品问题", Content: "质量差", AmountCents: &a2, Times: 3, TradeNos: []string{"ORD-B"}})
	seedComplaint(t, s, 9200, &o2, provider.ComplaintRecord{ThirdID: "WX-L3", PayType: "wxpay", Status: "PENDING", Content: "重复扣款", Times: 1, TradeNos: []string{"ORD-C"}})
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), `DELETE FROM complaints133 WHERE third_id IN ('WX-L1','AL-L2','WX-L3')`)
	})

	// Admin scope sees all three; merchant 80001 sees only its two.
	all, total, e := s.ComplaintList(ctx, ComplaintListFilter{OwnerScope: 0})
	if e != nil || total < 3 || len(all) < 3 {
		t.Fatalf("admin list: total=%d len=%d err=%v", total, len(all), e)
	}
	mine, mtotal, e := s.ComplaintList(ctx, ComplaintListFilter{OwnerScope: o1})
	if e != nil || mtotal != 2 || len(mine) != 2 {
		t.Fatalf("merchant scope: total=%d len=%d err=%v", mtotal, len(mine), e)
	}
	for _, it := range mine {
		if it.OwnerID != "80001" {
			t.Fatalf("merchant saw another owner's complaint: %+v", it)
		}
	}

	// pay_type filter.
	wx, wtotal, e := s.ComplaintList(ctx, ComplaintListFilter{OwnerScope: o1, PayType: "wxpay"})
	if e != nil || wtotal != 1 || wx[0].ThirdID != "WX-L1" {
		t.Fatalf("pay_type filter: total=%d err=%v rows=%+v", wtotal, e, wx)
	}
	// status filter (PENDING -> open).
	open, ototal, e := s.ComplaintList(ctx, ComplaintListFilter{OwnerScope: o1, Status: "open"})
	if e != nil || ototal != 1 || open[0].Status != "open" {
		t.Fatalf("status filter: total=%d err=%v", ototal, e)
	}
	// 搜索字段: 关联订单 (trade_nos array), 电话, 渠道子商户号, 第三方单号.
	byOrder, ototal, e := s.ComplaintList(ctx, ComplaintListFilter{OwnerScope: 0, Field: "trade_no", Keyword: "ORD-B"})
	if e != nil || ototal != 1 || byOrder[0].ThirdID != "AL-L2" {
		t.Fatalf("trade_no filter: total=%d err=%v", ototal, e)
	}
	byPhone, ptotal, e := s.ComplaintList(ctx, ComplaintListFilter{OwnerScope: 0, Field: "phone", Keyword: "13800000001"})
	if e != nil || ptotal != 1 || byPhone[0].ThirdID != "WX-L1" {
		t.Fatalf("phone filter: total=%d err=%v", ptotal, e)
	}
	byMch, _, e := s.ComplaintList(ctx, ComplaintListFilter{OwnerScope: 0, Field: "third_mch_id", Keyword: "MCH-W"})
	if e != nil || len(byMch) != 1 || byMch[0].ThirdID != "WX-L1" {
		t.Fatalf("third_mch_id filter: err=%v rows=%+v", e, byMch)
	}
	byThird, _, e := s.ComplaintList(ctx, ComplaintListFilter{OwnerScope: 0, Field: "third_id", Keyword: "AL-L2"})
	if e != nil || len(byThird) != 1 {
		t.Fatalf("third_id filter: err=%v rows=%+v", e, byThird)
	}

	// Offset paging: limit 1 over the 2 owner rows yields page1!=page2, total 2.
	p1, pt, e := s.ComplaintList(ctx, ComplaintListFilter{OwnerScope: o1, Page: 1, Limit: 1})
	p2, _, _ := s.ComplaintList(ctx, ComplaintListFilter{OwnerScope: o1, Page: 2, Limit: 1})
	if e != nil || pt != 2 || len(p1) != 1 || len(p2) != 1 || p1[0].ID == p2[0].ID {
		t.Fatalf("paging: total=%d p1=%v p2=%v err=%v", pt, p1, p2, e)
	}

	// Detail carries amount, times, third_mch_id, scoped.
	var id int64
	s.pool.QueryRow(ctx, `SELECT id FROM complaints133 WHERE third_id='WX-L1'`).Scan(&id)
	d, e := s.ComplaintDetail(ctx, id, o1)
	if e != nil || d.Amount != "1500" || d.ThirdMchID != "MCH-W" || d.TradeNo != "ORD-A" {
		t.Fatalf("detail: %+v err=%v", d, e)
	}
	if _, e := s.ComplaintDetail(ctx, id, o2); e == nil {
		t.Fatal("detail cross-tenant read allowed")
	}

	// Summary counts today for the owner (all seeds are 'now').
	sum, e := s.ComplaintSummary(ctx, o1)
	if e != nil || sum.TodayCount != "2" || sum.TodayWxpay != "1" || sum.TodayAlipay != "1" {
		t.Fatalf("summary: %+v err=%v", sum, e)
	}
	// Ranking lists the owner with today's counts.
	rank, e := s.ComplaintChartRanking(ctx, o1, 50)
	if e != nil || len(rank) != 1 || rank[0].Dimension != "80001" || rank[0].TodayCount != "2" {
		t.Fatalf("ranking: %+v err=%v", rank, e)
	}
}
