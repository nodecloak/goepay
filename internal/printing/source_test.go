package printing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"net/http"
	"testing"
)

func verifyPrintTestOrder(t *testing.T, p *payment.Store, o payment.Order) {
	t.Helper()
	ctx := context.Background()
	if _, _, e := p.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); e != nil {
		t.Fatal(e)
	}
	b := []byte(o.TradeNo + ":123")
	m := hmac.New(sha256.New, []byte("local-only-payment-signer"))
	m.Write(b)
	f := payment.ProviderFlow{Store: p, Providers: map[int64]provider.Provider{7: printPaymentVerifier{}}}
	if e := f.HandleNotification(ctx, 7, o.TradeNo, http.Header{"Local-Signature": []string{hex.EncodeToString(m.Sum(nil))}}, b); e != nil {
		t.Fatal(e)
	}
}
func TestPrintingCollectionOriginIsAtomicAndCannotBeGuessedFromMerchantText(t *testing.T) {
	s, p := printFixture(t)
	configurePrinting(t, s, 1)
	ctx := context.Background()
	var code int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,owner_id,body) VALUES('collection-codes',901,'{"enabled":true}') RETURNING id`).Scan(&code); e != nil {
		t.Fatal(e)
	}
	in := payment.CreateOrderInput{MerchantID: 901, OutTradeNo: "collect:forged-prefix", Name: "collection", Amount: 123, Type: "wxpay"}
	ordinary, e := p.CreateOrder(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	verifyPrintTestOrder(t, p, ordinary)
	if job, e := s.PrepareAutomatic(ctx, ordinary.ID); e != nil || job != nil {
		t.Fatal("merchant text inferred collection origin", job, e)
	}
	if _, e = p.CreateCollectionOrder(ctx, code, in); !errors.Is(e, payment.ErrConflict) {
		t.Fatal("ordinary replay relabeled collection", e)
	}
	in.OutTradeNo = "typed-collection-no-prefix"
	collection, e := p.CreateCollectionOrder(ctx, code, in)
	if e != nil {
		t.Fatal(e)
	}
	var cid int64
	var kind string
	if e = s.pool.QueryRow(ctx, "SELECT kind,collection_code_id FROM order_print_origins WHERE order_id=$1", collection.ID).Scan(&kind, &cid); e != nil || kind != "collection" || cid != code {
		t.Fatal(kind, cid, e)
	}
	if _, e = p.CreateOrder(ctx, in); !errors.Is(e, payment.ErrConflict) {
		t.Fatal("collection replay relabeled ordinary", e)
	}
	verifyPrintTestOrder(t, p, collection)
	if job, e := s.PrepareAutomatic(ctx, collection.ID); e != nil || job == nil || job.OrderID != collection.ID {
		t.Fatal("typed paid collection not printable", job, e)
	}
	in.MerchantID = 902
	in.OutTradeNo = "cross-owner"
	if _, e = p.CreateCollectionOrder(ctx, code, in); !errors.Is(e, payment.ErrConflict) {
		t.Fatal("foreign collection birth", e)
	}
}
func TestPrintingGenericMarkPaidCannotInventVerifiedSource(t *testing.T) {
	s, p := printFixture(t)
	configurePrinting(t, s, 2)
	ctx := context.Background()
	o, e := p.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 901, OutTradeNo: "generic-paid", Name: "local", Amount: 123, Type: "wxpay"})
	if e != nil {
		t.Fatal(e)
	}
	if e = p.MarkPaid(ctx, o.TradeNo, "LOCAL_UNVERIFIED"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Receipt(ctx, 901, o.TradeNo, printOwner); !errors.Is(e, ErrState) {
		t.Fatal("unverified paid source printable", e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM payment_print_sources WHERE order_id=$1", o.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
}
func TestPrintingForgedPaidSourceAndResultCannotAcquireTrust(t *testing.T) {
	s, p := printFixture(t)
	configurePrinting(t, s, 2)
	ctx := context.Background()
	o, _, _, _ := printPaidOrder(t, p, "forged-result")
	j, e := s.PrepareAutomatic(ctx, o.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Dispatch(ctx, j.ID, LocalFake{State: "unknown"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `ALTER TABLE printing_results DISABLE TRIGGER printing_result_immutable; UPDATE printing_results SET state='accepted',reason='gateway_accepted',proof_hash=decode(repeat('00',32),'hex'); ALTER TABLE printing_results ENABLE TRIGGER printing_result_immutable`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Job(ctx, 901, j.ID, printOwner); !errors.Is(e, ErrConflict) {
		t.Fatal("fake result accepted", e)
	}
	if _, e = s.pool.Exec(ctx, `ALTER TABLE payment_print_sources DISABLE TRIGGER payment_print_source_immutable; UPDATE payment_print_sources SET proof_hash=decode(repeat('00',32),'hex'); ALTER TABLE payment_print_sources ENABLE TRIGGER payment_print_source_immutable`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Receipt(ctx, 901, o.TradeNo, printOwner); !errors.Is(e, ErrConflict) {
		t.Fatal("fake paid source accepted", e)
	}
}
