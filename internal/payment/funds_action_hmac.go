package payment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// FundsActionHMAC prevents retained action fingerprints from becoming an
// offline dictionary oracle for recipient names or account numbers.
func (s *Store) FundsActionHMAC(domain string, raw []byte) (string, error) {
	if s == nil || len(s.indexKey) != 32 || len(domain) == 0 || len(domain) > 128 || strings.ContainsRune(domain, 0) || len(raw) > 4<<20 {
		return "", ErrInvalid
	}
	m := hmac.New(sha256.New, s.indexKey)
	m.Write([]byte("goepay/admin-funds-action/v1\x00"))
	m.Write([]byte(domain))
	m.Write([]byte{0})
	m.Write(raw)
	return hex.EncodeToString(m.Sum(nil)), nil
}
