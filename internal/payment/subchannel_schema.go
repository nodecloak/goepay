package payment

import (
	"context"
	"encoding/hex"
	adapter "goepay/internal/adapters/payment"
)

// The template digest includes fixed credential provenance inside the process.
// Persist and expose only distinct keyed commitments, never a credential hash
// usable as a public offline guessing oracle.
func (s *Store) childSchemaCommitment(raw string) (string, error) {
	decoded, e := hex.DecodeString(raw)
	if e != nil || len(decoded) != 32 {
		return "", ErrInvalid
	}
	h, e := s.referralDigest("payment-subchannel168/private-schema", decoded)
	if e != nil {
		return "", e
	}
	return hex.EncodeToString(h), nil
}
func (s *Store) childPublicSchema(commitment string) (string, error) {
	h, e := s.referralDigest("payment-subchannel168/public-schema", commitment)
	if e != nil {
		return "", e
	}
	return hex.EncodeToString(h), nil
}
func (s *Store) PublicSubchannelSchema(schema adapter.SubchannelSchema) (adapter.SubchannelSchema, error) {
	private, e := s.childSchemaCommitment(schema.Fingerprint)
	if e != nil {
		return schema, e
	}
	schema.Fingerprint, e = s.childPublicSchema(private)
	return schema, e
}

func (s *Store) CheckSubchannelActor(ctx context.Context, a SubchannelActor) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = a.check(ctx, tx); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
