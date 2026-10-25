package payment

import (
	"strings"
	"testing"
	"time"
)

func remainingObservation75() (PreauthorizationObservation, PreauthRemainingReleaseInput, time.Time) {
	at := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)
	remaining := int64(600)
	observed := at.Add(-time.Second)
	q := PreauthorizationObservation{ID: strings.Repeat("a", 64), MerchantID: 1, OrderID: 12, AttemptID: 13, ChannelID: 7, TradeNo: "remaining75-order", AppID: "local75-app", AuthNo: "local75-auth", BuyerID: "local75-buyer", Amount: 1000, Status: "verified", RestKnown: true, RestAmount: &remaining, ProviderStatus: "SUCCESS", OrderStatus: "AUTHORIZED", OperationType: "FREEZE", ObservedAt: &observed, SourceVerified: true, merchantRevision: 1, expiresAt: at.Add(time.Minute), accountHash: make([]byte, 32), sourceProof: make([]byte, 32), resultProof: make([]byte, 32)}
	q.original.sessionHash = make([]byte, 32)
	q.original.MerchantID = q.MerchantID
	q.original.actorRole = "admin"
	q.original.actorID = 1
	q.original.actorUsername = "local75-admin"
	q.original.actorVersion = 1
	return q, PreauthRemainingReleaseInput{ExpectedObservationID: q.ID, ExpectedRemainingCents: 600}, at
}

func TestPreauthRemaining75TermsPreserveOriginalUAAndExplicitVerifiedRemainder(t *testing.T) {
	q, in, at := remainingObservation75()
	terms, e := preauthRemainingTerms(q, in, at)
	if e != nil || terms.Amount != 600 || terms.OriginalAmount != 1000 || terms.RequestNo != "UA12" || terms.ObservationID != q.ID || terms.Revision != 1 || !terms.SendBefore.Equal(q.expiresAt) {
		t.Fatal(terms, e)
	}
	in.ExpectedSourceRevision = 99
	if terms, e = preauthRemainingTerms(q, in, at); e != nil || terms.Revision != 100 {
		t.Fatal(terms, e)
	}
}

func TestPreauthRemaining75TermsRejectUnknownChangedExpiredOrForeignObservation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*PreauthorizationObservation, *PreauthRemainingReleaseInput)
	}{
		{"wrong observation", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) {
			in.ExpectedObservationID = strings.Repeat("b", 64)
		}},
		{"wrong amount", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) {
			in.ExpectedRemainingCents = 500
		}},
		{"unknown remainder", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) { q.RestKnown = false }},
		{"missing remainder", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) { q.RestAmount = nil }},
		{"zero remainder", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) {
			*q.RestAmount = 0
			in.ExpectedRemainingCents = 0
		}},
		{"above frozen", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) {
			*q.RestAmount = 1001
			in.ExpectedRemainingCents = 1001
		}},
		{"unverified", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) { q.SourceVerified = false }},
		{"unknown query", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) { q.Status = "unknown" }},
		{"wrong operation", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) { q.OperationType = "UNFREEZE" }},
		{"finished order", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) { q.OrderStatus = "FINISH" }},
		{"wrong provider state", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) { q.ProviderStatus = "FAIL" }},
		{"expired", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) {
			q.expiresAt = q.ObservedAt.Add(time.Second)
		}},
		{"missing result proof", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) { q.resultProof = nil }},
		{"missing account scope", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) { q.accountHash = nil }},
		{"missing session", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) { q.original.sessionHash = nil }},
		{"wrong actor role", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) {
			q.original.actorRole = "system"
		}},
		{"missing actor version", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) { q.original.actorVersion = 0 }},
		{"exhausted revisions", func(q *PreauthorizationObservation, in *PreauthRemainingReleaseInput) {
			in.ExpectedSourceRevision = 100
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, in, at := remainingObservation75()
			tc.change(&q, &in)
			if _, e := preauthRemainingTerms(q, in, at); e == nil {
				t.Fatal("unsafe remaining-release terms accepted", tc.name)
			}
		})
	}
}
