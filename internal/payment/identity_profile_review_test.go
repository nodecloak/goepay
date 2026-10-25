package payment

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestReviewIdentityProfileSourceAndCipherAreBoundToExactRequest(t *testing.T) {
	s, err := NewWithKey(nil, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	// A live session with less than five minutes remaining gives all pending
	// mini requests the same exact session expiry. Their profile/session match.
	q1 := ProfileIdentityLookup{IdentityProfileBinding: profileBinding64(965, "mini"), ID: strings.Repeat("1", 64), ExpiresAt: time.Date(2026, 10, 23, 18, 0, 0, 0, time.UTC)}
	q1.profileHash = s.oauthHash("identity-profile-runtime-v1", q1.Fingerprint)
	q1.session = IdentityLookup{sessionHash: bytes.Repeat([]byte{3}, 32), actorUsername: "same-live-session", actorRole: "merchant", actorID: 965, actorVersion: 1}
	q2 := q1
	q2.ID = strings.Repeat("2", 64)
	q1.sourceProof = s.profileSource(q1)
	q2.sourceProof = s.profileSource(q2)
	nonce := bytes.Repeat([]byte{4}, s.aead.NonceSize())
	ciphertext := s.aead.Seal(nil, nonce, []byte("request-one-private-subject"), q1.sourceProof)
	if bytes.Equal(q1.sourceProof, q2.sourceProof) {
		t.Error("two distinct mini requests share their source HMAC")
	}
	if plaintext, err := s.aead.Open(nil, nonce, ciphertext, q2.sourceProof); err == nil {
		t.Fatalf("request-two accepted request-one subject ciphertext: %q", plaintext)
	}
}
