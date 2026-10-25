package payment

import (
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
)

func (s *Store) sharingOrderFailureFingerprint(r sharingOrderRecord) ([]byte, error) {
	raw, e := json.Marshal([]any{r.ID, hex.EncodeToString(r.RequestHash), "failed-before-acceptance"})
	if e != nil {
		return nil, e
	}
	return s.sharingDigest("sharing-order-rejection", raw)
}

// An unobserved failure may permit a different finish operation. The immutable
// request proof alone never authenticates that mutable rejection decision.
func (s *Store) verifySharingOrderFailure(r sharingOrderRecord) error {
	if r.State != "failed" || r.LastObservationID != 0 {
		if len(r.FailureProof) != 0 {
			return ErrConflict
		}
		return nil
	}
	h, e := s.sharingOrderFailureFingerprint(r)
	if e != nil || !hmac.Equal(h, r.FailureProof) {
		return ErrConflict
	}
	return nil
}
