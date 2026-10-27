package privatesearch

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
)

func TestPrivateSearchTokensPreserveLiteralUnicodeAndFieldIsolation(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	fields := Fields{Account: "Alpha.Private@Example.invalid", Name: "本地张三", Remark: "中文备注 %_ literal"}
	terms, e := Build(key, fields)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []struct {
		field Field
		q     string
	}{{Account, "ALPHA.PRIVATE"}, {Name, "张三"}, {Name, "张"}, {Remark, "%_"}} {
		q, e := Query(key, v.field, v.q)
		if e != nil || !MayMatch(terms, q) || !Match(fields, v.field, v.q) {
			t.Fatal("original substring lost", v, e)
		}
	}
	q, e := Query(key, Name, "alpha")
	if e != nil || MayMatch(terms, q) || Match(fields, Name, "alpha") {
		t.Fatal("cross-field private tokens")
	}
	raw, _ := json.Marshal(terms)
	for _, plain := range []string{"Alpha", "Example", "张三", "literal"} {
		if strings.Contains(string(raw), plain) {
			t.Fatal("index contains private plaintext")
		}
	}
	foreign, _ := Query(bytes.Repeat([]byte{8}, 32), Account, "alpha")
	if MayMatch(terms, foreign) {
		t.Fatal("different index key shares candidates")
	}
}
func TestPrivateSearchCandidateRequiresOriginalPlaintextVerification(t *testing.T) {
	key := make([]byte, 32)
	fields := Fields{Account: "ababa"}
	terms, e := Build(key, fields)
	if e != nil {
		t.Fatal(e)
	}
	q, e := Query(key, Account, "ababab")
	if e != nil || !MayMatch(terms, q) || Match(fields, Account, "ababab") {
		t.Fatal("candidate was confused with original full match", e)
	}
}
func TestPrivateSearchProofBindsSourceOwnerKindAndEveryToken(t *testing.T) {
	key := make([]byte, 32)
	terms, e := Build(key, Fields{Account: "local-account", Name: "本地姓名"})
	if e != nil {
		t.Fatal(e)
	}
	source := sha256.Sum256([]byte("synthetic original encrypted payload"))
	p, e := Proof(key, "settlement_config", 7, 11, source[:], terms)
	if e != nil || !Verify(key, "settlement_config", 7, 11, source[:], terms, p) {
		t.Fatal(e)
	}
	if Verify(key, "transfer_intent", 7, 11, source[:], terms, p) || Verify(key, "settlement_config", 8, 11, source[:], terms, p) || Verify(key, "settlement_config", 7, 12, source[:], terms, p) {
		t.Fatal("detached search projection trusted")
	}
	source[0] ^= 1
	if Verify(key, "settlement_config", 7, 11, source[:], terms, p) {
		t.Fatal("changed source hash")
	}
	source[0] ^= 1
	terms[Name][0] = strings.Repeat("0", 64)
	if Verify(key, "settlement_config", 7, 11, source[:], terms, p) {
		t.Fatal("changed term list")
	}
	for _, raw := range []string{"", "bad\x00", "bad\n", strings.Repeat("中", 257)} {
		if _, e := Query(key, Account, raw); e == nil {
			t.Fatal("invalid private query", len(raw))
		}
	}
	if _, e := Build([]byte("short"), Fields{Account: "x"}); e == nil {
		t.Fatal("weak index key")
	}
}
