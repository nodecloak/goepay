package httpclient

import (
	"crypto/tls"
	"errors"
	"testing"
)

func TestMutualTLSRetainsDestinationGuard(t *testing.T) {
	c := NewMutualTLS(tls.Certificate{})
	_, e := c.Get("https://127.0.0.1/private")
	if !errors.Is(e, ErrDestination) {
		t.Fatalf("destination policy lost: %v", e)
	}
}
