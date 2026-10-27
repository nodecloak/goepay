package payment

import (
	"context"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"strings"
	"testing"
)

type remainingProvider75 struct {
	*observationProvider68
	writes        int
	sent          adapter.AuthorizationRequest
	unknown       bool
	paymentState  string
	paymentError  error
	paymentAmount int64
}

func (p *remainingProvider75) Query(_ context.Context, r adapter.QueryRequest) (adapter.QueryResult, error) {
	if p.paymentState == "" {
		return adapter.QueryResult{}, &adapter.ProviderError{Code: "40004:ACQ.TRADE_NOT_EXIST"}
	}
	return adapter.QueryResult{TradeNo: r.TradeNo, Amount: p.paymentAmount, Currency: "CNY", State: p.paymentState, ProviderTradeNo: "original-payment75"}, p.paymentError
}
func (p *remainingProvider75) UnfreezeAuthorization(_ context.Context, r adapter.AuthorizationRequest) (adapter.AuthorizationEvent, error) {
	p.writes++
	p.sent = r
	if p.unknown {
		return adapter.AuthorizationEvent{}, context.DeadlineExceeded
	}
	return adapter.AuthorizationEvent{State: "SUCCESS"}, nil
}
func (p *remainingProvider75) QueryAuthorization(ctx context.Context, r adapter.AuthorizationRequest) (adapter.AuthorizationEvent, error) {
	if strings.HasPrefix(r.RequestNo, "UA") {
		return adapter.AuthorizationEvent{TradeNo: r.TradeNo, RequestNo: r.RequestNo, AuthNo: r.AuthNo, Amount: r.Amount, BuyerID: "2088000000001068", OperationType: "UNFREEZE", State: "SUCCESS", RestAmountKnown: true, RestAmount: 0}, nil
	}
	return p.observationProvider68.QueryAuthorization(ctx, r)
}
func TestPreauthRemaining75FlowSendsOnlyBoundRemainderAndRecoversOriginalUAAfterRevocation(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "unknown"}[unknown], func(t *testing.T) {
			s, g, o, token, base := observationFixture68(t)
			ctx := context.Background()
			p := &remainingProvider75{observationProvider68: base, unknown: unknown}
			g.Providers[7] = p
			g.AllowLive = true
			q, e := g.ObservePreauthorization(ctx, PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("f", 64)}, token, "merchant", observationCheck68, observationCheckTx68)
			if e != nil {
				t.Fatal(e)
			}
			in := PreauthRemainingReleaseInput{ExpectedObservationID: q.ID, ExpectedRemainingCents: 450}
			guarded := remainingSourceGuard75(t, s)
			r, e := g.ReleaseRemainingAuthorization(guarded, o.MerchantID, o.TradeNo, in, token, "merchant", observationCheck68, observationCheckTx68)
			if unknown {
				var u *adapter.UnknownOutcome
				if !errors.As(e, &u) {
					t.Fatal(r, e)
				}
			} else if e != nil || r.State != "succeeded" {
				t.Fatal(r, e)
			}
			if p.writes != 1 || p.sent.Amount != 450 || p.sent.RequestNo != r.RequestNo || r.OriginalAmount != 1000 || !r.Sent {
				t.Fatal("wrong effective send", p.sent, r, p.writes)
			}
			if _, e = s.pool.Exec(ctx, "DELETE FROM console_sessions WHERE token_hash=$1", lookupSessionHash(token)); e != nil {
				t.Fatal(e)
			}
			g.AllowLive = false
			r, e = g.QueryAuthorizationRelease(ctx, o.MerchantID, o.TradeNo)
			if e != nil || r.State != "succeeded" || r.Amount != 450 || p.writes != 1 {
				t.Fatal("original read-only recovery", r, e, p.writes)
			}
			var journals int
			if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM journals").Scan(&journals); e != nil || journals != 0 {
				t.Fatal("buyer unfreeze invented merchant money", journals, e)
			}
		})
	}
}

func TestPreauthRemaining75FlowFreshEvidenceAndInFlightCaptureCannotBorrowObservationAuthority(t *testing.T) {
	for _, kind := range []string{"remainder-changed", "capture-not-found", "capture-closed", "partial-original-payment", "permission-revoked"} {
		t.Run(kind, func(t *testing.T) {
			s, g, o, token, base := observationFixture68(t)
			ctx := context.Background()
			p := &remainingProvider75{observationProvider68: base, unknown: true}
			g.Providers[7] = p
			g.AllowLive = true
			q, e := g.ObservePreauthorization(ctx, PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("b", 64)}, token, "merchant", observationCheck68, observationCheckTx68)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "capture-not-found" || kind == "capture-closed" {
				if _, owner, e := s.BeginPreauthorizationCapture(ctx, o.TradeNo); e != nil || !owner {
					t.Fatal(owner, e)
				}
				if e = s.RecordPreauthorizationCaptureState(ctx, o.TradeNo, "unknown"); e != nil {
					t.Fatal(e)
				}
			}
			switch kind {
			case "remainder-changed":
				p.remaining = 400
			case "capture-closed":
				p.paymentState = "TRADE_CLOSED"
				p.paymentAmount = o.Amount
			case "partial-original-payment":
				p.paymentState = "TRADE_SUCCESS"
				p.paymentAmount = 100
			case "permission-revoked":
				p.before = func() {
					if _, e = s.pool.Exec(ctx, "UPDATE merchants SET enabled=false WHERE id=$1", o.MerchantID); e != nil {
						t.Error(e)
					}
				}
			}
			in := PreauthRemainingReleaseInput{ExpectedObservationID: q.ID, ExpectedRemainingCents: 450}
			r, e := g.ReleaseRemainingAuthorization(remainingSourceGuard75(t, s), o.MerchantID, o.TradeNo, in, token, "merchant", observationCheck68, observationCheckTx68)
			if e == nil {
				t.Fatal("unknown/pending evidence became completion", r)
			}
			expected := 0
			if kind == "capture-closed" {
				expected = 1
			}
			if p.writes != expected || r.Sent != (expected == 1) {
				t.Fatal("wrong first-write boundary", kind, r, p.writes, e)
			}
			a, e := s.Preauthorization(ctx, o.TradeNo)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "capture-not-found" && a.CaptureState != "unknown" {
				t.Fatal("not-found resolved an in-flight capture", a)
			}
			if kind == "capture-closed" && a.CaptureState != "failed" {
				t.Fatal("signed closed capture not reconciled", a)
			}
			order, e := s.OrderByTradeNo(ctx, o.TradeNo)
			if e != nil || order.Status == "paid" {
				t.Fatal("partial/unknown original payment faked full receipt", order, e)
			}
			var journals int
			if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM journals").Scan(&journals); e != nil || journals != 0 {
				t.Fatal(journals, e)
			}
		})
	}
}
