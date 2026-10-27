package payment

import (
	"bytes"
	"testing"
)

func TestMerchant167OriginProofRequiresOriginalPurposeOwnerAndUnchangedEvidence(t *testing.T) {
	s, e := NewWithKey(nil, bytes.Repeat([]byte{167}, 32))
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []struct {
		kind       string
		collection int64
	}{{"api", 0}, {"collection", 9}} {
		proof, e := s.referralDigest("order-print-origin", []any{int64(167), int64(1), v.kind, v.collection})
		if e != nil {
			t.Fatal(e)
		}
		if !s.VerifiedOrdinaryOrderOrigin(167, 1, v.collection, v.kind, proof) {
			t.Fatal("original proof rejected", v)
		}
		for _, args := range []struct {
			id, owner, collection int64
			kind                  string
			proof                 []byte
		}{{168, 1, v.collection, v.kind, proof}, {167, 2, v.collection, v.kind, proof}, {167, 1, v.collection, "recharge", proof}, {167, 1, v.collection, "legacy", proof}, {167, 1, v.collection, v.kind, bytes.Repeat([]byte{0}, 32)}, {167, 1, v.collection, v.kind, nil}} {
			if s.VerifiedOrdinaryOrderOrigin(args.id, args.owner, args.collection, args.kind, args.proof) {
				t.Fatal("unverified origin granted", args.kind)
			}
		}
	}
}
