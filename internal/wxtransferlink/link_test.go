package wxtransferlink

import (
	"strings"
	"testing"
	"time"
)

func TestConfirmationRouteHasBoundSignature231(t *testing.T) {
	key := make([]byte, 32)
	a := Sign(key, "1", "2")
	if a == Sign(key, "2", "2") || a == Sign(key, "1", "3") {
		t.Fatal("unbound signature")
	}
	p := Path(key, 1, time.Unix(2, 0))
	if !strings.HasPrefix(p, "/pay/wxtrans?") || !strings.Contains(p, "s="+a) {
		t.Fatal("wrong route")
	}
}
