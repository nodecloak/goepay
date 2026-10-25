package payment

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestMembershipSaleUXMonthPermanentAndUnpublished(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 8511, Name: "sale test", APIKey: "local-sale-test-key"}); e != nil {
		t.Fatal(e)
	}
	var month, permanent, unpublished int64
	for _, g := range []struct {
		name, body string
		id         *int64
	}{
		{"month", `{"name":"monthly","enabled":true,"price_cents":"100","duration_days":"30","sale_enabled":true,"duration_months":1}`, &month},
		{"permanent", `{"name":"permanent","enabled":true,"price_cents":"100","duration_days":"30","sale_enabled":true,"duration_months":0}`, &permanent},
		{"unpublished", `{"name":"unpublished","enabled":true,"price_cents":"100","duration_days":"30","sale_enabled":false,"duration_months":1}`, &unpublished},
	} {
		if e := s.pool.QueryRow(ctx, "INSERT INTO backoffice_records(resource,body) VALUES('groups',$1) RETURNING id", g.body).Scan(g.id); e != nil {
			t.Fatal(e)
		}
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 8511, OutTradeNo: "fund-sale", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "local-sale"); e != nil {
		t.Fatal(e)
	}
	before := time.Now().In(time.FixedZone("UTC+08", 8*3600)).AddDate(0, 1, 0)
	p, e := s.PurchaseMembership(ctx, 8511, month, "month-sale", "merchant:8511")
	if e != nil {
		t.Fatal(e)
	}
	expected := time.Date(before.Year(), before.Month(), before.Day(), 0, 0, 0, 0, before.Location())
	if !p.ExpiresAt.Equal(expected) {
		t.Fatalf("calendar month must end at original business date, got %v want %v", p.ExpiresAt, expected)
	}
	if _, e = s.PurchaseMembership(ctx, 8511, unpublished, "unpublished-sale", "merchant:8511"); !errors.Is(e, ErrState) {
		t.Fatalf("unpublished group purchased: %v", e)
	}
	p, e = s.PurchaseMembership(ctx, 8511, permanent, "permanent-sale", "merchant:8511")
	if e != nil {
		t.Fatal(e)
	}
	if p.ExpiresAt.Year() != 9999 {
		t.Fatalf("permanent became finite days: %v", p.ExpiresAt)
	}
	if _, e = s.PurchaseMembership(ctx, 8511, permanent, "permanent-repeat", "merchant:8511"); !errors.Is(e, ErrState) {
		t.Fatalf("already permanent repurchased: %v", e)
	}
	b, e := s.Balance(ctx, 8511)
	if e != nil || b.Available != 800 {
		t.Fatalf("invalid or duplicate sale changed balance %+v %v", b, e)
	}
}

func TestMembershipSaleUXCalendarRollover(t *testing.T) {
	for _, tc := range []struct {
		base, want       string
		months, quantity int
	}{
		{"2025-01-31T12:30:00+08:00", "2025-03-03T00:00:00+08:00", 1, 1},
		{"2024-01-31T12:30:00+08:00", "2024-03-02T00:00:00+08:00", 1, 1},
		{"2025-01-31T12:30:00+08:00", "2025-03-31T00:00:00+08:00", 1, 2},
		{"2025-12-31T12:30:00+08:00", "2026-03-03T00:00:00+08:00", 2, 1},
	} {
		base, _ := time.Parse(time.RFC3339, tc.base)
		want, _ := time.Parse(time.RFC3339, tc.want)
		got, _, e := membershipSaleExpiry(MembershipSaleTerms{Months: tc.months, Quantity: tc.quantity}, base)
		if e != nil || !got.Equal(want) {
			t.Fatalf("%s + %d*%d month = %v %v, want %v", tc.base, tc.months, tc.quantity, got, e, want)
		}
	}
	var empty Store
	for _, quantity := range []int{0, -1, 301} {
		if _, e := empty.PurchaseMembership(context.Background(), 1, 2, "x", "actor", quantity); !errors.Is(e, ErrInvalid) {
			t.Fatalf("invalid quantity accessed DB: %d %v", quantity, e)
		}
	}
}

func TestMembershipSaleUXAlreadyPermanentBaseGroupCannotBeRepurchased(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 8512, Name: "permanent base", APIKey: "local-member-sale-key"}); e != nil {
		t.Fatal(e)
	}
	var group int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"base permanent","enabled":true,"price_cents":"0","duration_months":1}') RETURNING id`).Scan(&group); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, "UPDATE merchants SET profile=jsonb_set(profile,'{group_id}',to_jsonb($2::text)) WHERE id=$1", 8512, strconv.FormatInt(group, 10)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.PurchaseMembership(ctx, 8512, group, "base-repeat", "merchant:8512"); !errors.Is(e, ErrState) {
		t.Fatalf("permanent base converted into expiring purchase: %v", e)
	}
	offers, _, e := s.MembershipSalesCatalog(ctx, 8512)
	if e != nil || len(offers) != 0 {
		t.Fatalf("already permanent base advertised: %+v %v", offers, e)
	}
}

func TestMembershipSaleUXQuantityProofReplayAndOriginalReward(t *testing.T) {
	s, promoter, buyer, group := membership72RewardFixture(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"duration_months":1,"sale_enabled":true,"sale_visible":"0"}' WHERE id=$1`, group); e != nil {
		t.Fatal(e)
	}
	p, e := s.PurchaseMembership(ctx, buyer, group, "sale-quantity", "merchant:7211", 2)
	if e != nil {
		t.Fatal(e)
	}
	if p.Price != 600 || p.SaleTerms == nil || p.SaleTerms.Quantity != 2 || p.SaleTerms.UnitPrice != 300 {
		t.Fatalf("quantity not frozen: %+v", p)
	}
	b, e := s.Balance(ctx, buyer)
	if e != nil || b.Available != 400 {
		t.Fatalf("wrong total debit: %+v %v", b, e)
	}
	b, e = s.Balance(ctx, promoter)
	if e != nil || b.Available != 150 {
		t.Fatalf("wrong original-income reward: %+v %v", b, e)
	}
	old, e := s.PurchaseMembership(ctx, buyer, group, "sale-quantity", "merchant:7211", 2)
	if e != nil || old.ID != p.ID {
		t.Fatalf("same request charged again: %+v %v", old, e)
	}
	if _, e = s.PurchaseMembership(ctx, buyer, group, "sale-quantity", "merchant:7211", 1); !errors.Is(e, ErrConflict) {
		t.Fatalf("quantity changed under original request: %v", e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if e = s.verifyMembershipPurchase(ctx, tx, p); e != nil {
		t.Fatal(e)
	}
	bad := p
	terms := *p.SaleTerms
	terms.Quantity = 1
	bad.SaleTerms = &terms
	if e = s.verifyMembershipPurchase(ctx, tx, bad); !errors.Is(e, ErrConflict) {
		t.Fatalf("sale terms not authenticated: %v", e)
	}
	bad.SaleTerms = nil
	if e = s.verifyMembershipPurchase(ctx, tx, bad); !errors.Is(e, ErrConflict) {
		t.Fatalf("new sale downgraded to old proof: %v", e)
	}
}
