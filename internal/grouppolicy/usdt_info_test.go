package grouppolicy

import "testing"

func TestUSDTGroupInfoRetainsExactChannelAndRate(t *testing.T) {
	info, err := ParseInfo([]byte(`{"usdt":{"rate":"99.25","channel":"9007199254740993","type":"channel"}}`))
	if err != nil {
		t.Fatal("USDT must participate in group policy", err)
	}
	if rate, ok := info["usdt"].RetainedBPS(); !ok || rate != 9925 {
		t.Fatal("USDT retained rate lost", rate, ok)
	}
	if c := info["usdt"].RoutingChoice(); c.Kind() != Channel || c.ID() != 9007199254740993 {
		t.Fatal("USDT channel lost or rounded", c)
	}
	raw, err := info.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	again, err := ParseInfo(raw)
	if err != nil || again["usdt"] != info["usdt"] {
		t.Fatal("USDT group policy changed on serialization", err)
	}
}
