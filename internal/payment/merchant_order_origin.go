package payment

import "crypto/hmac"

// VerifiedOrdinaryOrderOrigin verifies batch-read immutable origin data without
// another query. Unknown/legacy/recharge origins never become an invented tid.
func (s *Store) VerifiedOrdinaryOrderOrigin(id, merchant, collection int64, kind string, proof []byte) bool {
	if id <= 0 || merchant <= 0 || (kind != "api" && kind != "collection") || (kind == "api" && collection != 0) || (kind == "collection" && collection <= 0) {
		return false
	}
	want, e := s.referralDigest("order-print-origin", []any{id, merchant, kind, collection})
	return e == nil && hmac.Equal(want, proof)
}
