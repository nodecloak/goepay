package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

// BeginProviderAttemptWithIdentity binds a signed payer and the trusted channel
// AppID to the single attempt. Plain OpenIDs are not written into the index.
func (s *Store) BeginProviderAttemptWithIdentity(ctx context.Context, tradeNo string, channelID int64, method, appID, openID string) (ProviderAttempt, bool, error) {
	return s.beginProviderAttemptWithIdentity(ctx, tradeNo, channelID, method, appID, openID, "")
}

// BeginProviderAttemptWithAccount fixes the receiving merchant at the same
// transaction boundary as the signed buyer and the single send opportunity.
func (s *Store) BeginProviderAttemptWithAccount(ctx context.Context, tradeNo string, channelID int64, method, appID, openID, accountScope string) (ProviderAttempt, bool, error) {
	if accountScope == "" {
		return ProviderAttempt{}, false, ErrInvalid
	}
	return s.beginProviderAttemptWithIdentity(ctx, tradeNo, channelID, method, appID, openID, accountScope)
}
func (s *Store) beginProviderAttemptWithIdentity(ctx context.Context, tradeNo string, channelID int64, method, appID, openID, accountScope string, terms ...*sharingPaymentIntent) (ProviderAttempt, bool, error) {
	buyer := ""
	if method == "jsapi" || method == "mini" || method == "preauth" {
		buyer = openID
	}
	ctx = withCoreRiskBuyer(ctx, buyer)
	hash, e := s.providerIdentityHash(channelID, method, appID, openID)
	if e != nil {
		return ProviderAttempt{}, false, e
	}
	if len(terms) == 0 {
		terms = []*sharingPaymentIntent{{ChannelSharingPaymentView: ChannelSharingPaymentView{AppID: appID}}}
	}
	return s.beginProviderAttempt(ctx, tradeNo, channelID, method, hash, accountScope, terms...)
}
func (s *Store) providerIdentityHash(channelID int64, method, appID, openID string) ([]byte, error) {
	if len(s.indexKey) != 32 || len(appID) > 256 || len(openID) > 256 {
		return nil, ErrInvalid
	}
	mac := hmac.New(sha256.New, s.indexKey)
	mac.Write([]byte("goepay/payment-create-identity/v1\x00"))
	if e := json.NewEncoder(mac).Encode([]any{channelID, method, appID, openID}); e != nil {
		return nil, e
	}
	return mac.Sum(nil), nil
}

func bindProviderIdentity(ctx context.Context, tx pgx.Tx, attemptID int64, hash []byte, created bool) error {
	if hash == nil {
		return nil
	} // Legacy internal entry point; public flows bind identity.
	if created {
		_, e := tx.Exec(ctx, "INSERT INTO provider_attempt_identities(attempt_id,request_hash) VALUES($1,$2)", attemptID, hash)
		return e
	}
	var stored []byte
	e := tx.QueryRow(ctx, "SELECT request_hash FROM provider_attempt_identities WHERE attempt_id=$1", attemptID).Scan(&stored)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrConflict
	} // Historical payer cannot safely be inferred.
	if e != nil {
		return e
	}
	if !hmac.Equal(stored, hash) {
		return ErrConflict
	}
	return nil
}
