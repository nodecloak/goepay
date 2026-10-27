package grouppolicy

import (
	"strings"
	"testing"
)

func TestSubchannel168BOriginalMinusTwoIsAnExplicitChildChoiceNotParentID(t *testing.T) {
	for _, kind := range []string{"wxpay", "alipay"} {
		raw := []byte(`{"` + kind + `":{"channel":"-2","type":"channel","rate":"0"}}`)
		info, e := ParseInfo(raw)
		if e != nil {
			t.Fatalf("original child route -2 is not supported for %s: %v", kind, e)
		}
		if info[kind].RoutingChoice().ID() != 0 {
			t.Fatal("-2 was interpreted as a parent ID")
		}
		canonical, e := info.CanonicalJSON()
		if e != nil || !strings.Contains(string(canonical), `"channel":"-2"`) {
			t.Fatalf("-2 identity was lost: %s %v", canonical, e)
		}
	}
}
