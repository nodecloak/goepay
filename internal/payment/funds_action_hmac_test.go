package payment

import (
	"strings"
	"testing"
)

func TestFundsActionHMACDomainKeyAndInputBoundaries(t *testing.T) {
	a, _ := NewWithKey(nil, make([]byte, 32))
	b, _ := NewWithKey(nil, []byte(strings.Repeat("b", 32)))
	x, e := a.FundsActionHMAC("body/v1", []byte("same"))
	if e != nil || len(x) != 64 {
		t.Fatal(x, e)
	}
	same, _ := a.FundsActionHMAC("body/v1", []byte("same"))
	otherDomain, _ := a.FundsActionHMAC("snapshot/v1", []byte("same"))
	otherKey, _ := b.FundsActionHMAC("body/v1", []byte("same"))
	if x != same || x == otherDomain || x == otherKey {
		t.Fatal("unseparated action HMAC")
	}
	if _, e = (&Store{}).FundsActionHMAC("body/v1", nil); e == nil {
		t.Fatal("unkeyed digest accepted")
	}
}
