package payment

import (
	"fmt"
	"time"
)

type PreauthRemainingReleaseInput struct {
	ExpectedObservationID  string `json:"expected_observation_id"`
	ExpectedRemainingCents int64  `json:"expected_remaining_cents,string"`
	ExpectedSourceRevision int64  `json:"expected_source_revision,string"`
}

type preauthRemainingReleaseTerms struct {
	OriginalAmount int64     `json:"original_amount,string"`
	Amount         int64     `json:"amount,string"`
	Revision       int64     `json:"revision,string"`
	RequestNo      string    `json:"request_no"`
	ObservationID  string    `json:"observation_id"`
	SendBefore     time.Time `json:"send_before"`
}

// Pure shape/time policy, not an observation authenticator or a send grant.
// Its caller must load the original row through scanObservation, verify its
// event chain/origin/current session, and seal the independent release source.
func preauthRemainingTerms(q PreauthorizationObservation, in PreauthRemainingReleaseInput, at time.Time) (preauthRemainingReleaseTerms, error) {
	var out preauthRemainingReleaseTerms
	if !observationID68(in.ExpectedObservationID) || in.ExpectedRemainingCents <= 0 || in.ExpectedSourceRevision < 0 || in.ExpectedSourceRevision >= 100 {
		return out, ErrInvalid
	}
	if q.ID != in.ExpectedObservationID || !q.SourceVerified || !q.RestKnown || q.Status != "verified" || q.ProviderStatus != "SUCCESS" || q.OrderStatus != "AUTHORIZED" || q.OperationType != "FREEZE" || q.RestAmount == nil || *q.RestAmount != in.ExpectedRemainingCents || q.Amount <= 0 || *q.RestAmount > q.Amount || q.MerchantID <= 0 || q.OrderID <= 0 || q.AttemptID <= 0 || q.ChannelID <= 0 || q.TradeNo == "" || q.AppID == "" || q.AuthNo == "" || q.BuyerID == "" || len(q.accountHash) != 32 || len(q.sourceProof) != 32 || len(q.resultProof) != 32 || q.merchantRevision <= 0 || len(q.original.sessionHash) != 32 || q.original.actorID <= 0 || q.original.actorVersion <= 0 || q.original.actorUsername == "" || (q.original.actorRole != "admin" && q.original.actorRole != "merchant") || q.original.MerchantID != q.MerchantID || (q.original.actorRole == "merchant" && q.original.actorID != q.MerchantID) {
		return out, ErrConflict
	}
	if at.IsZero() || q.ObservedAt == nil || q.ObservedAt.IsZero() || q.ObservedAt.After(at) || !q.expiresAt.After(*q.ObservedAt) || q.expiresAt.Sub(*q.ObservedAt) > 5*time.Minute {
		return out, ErrConflict
	}
	if !at.Before(q.expiresAt) {
		return out, ErrState
	}
	return preauthRemainingReleaseTerms{OriginalAmount: q.Amount, Amount: *q.RestAmount, Revision: in.ExpectedSourceRevision + 1, RequestNo: fmt.Sprintf("UA%d", q.OrderID), ObservationID: q.ID, SendBefore: q.expiresAt.UTC()}, nil
}
